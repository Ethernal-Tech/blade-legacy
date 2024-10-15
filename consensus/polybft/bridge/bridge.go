package bridge

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/0xPolygon/polygon-edge/bls"
	polychain "github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/helpers"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/oracle"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/signer"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/state"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/hashicorp/go-hclog"
	"github.com/libp2p/go-libp2p/core/peer"
	bolt "go.etcd.io/bbolt"
	"google.golang.org/protobuf/proto"
)

var (
	errBridgeBatchTxExists             = errors.New("only one bridge batch tx is allowed per block")
	errBridgeBatchTxInNonSprintBlock   = errors.New("bridge batch tx is not allowed in non-sprint block")
	errCommitValidatorSetExists        = errors.New("only one commit validator set tx is allowed per block")
	errCommitValidatorSetTxNotExpected = errors.New("commit validator set tx is not expected " +
		"in non epoch ending block")
	errCommitValidatorSetTxInvalid      = errors.New("commit validator set tx is invalid")
	errCommitValidatorSetTxDoesNotExist = errors.New("commit validator set tx is not found in the block " +
		"even though new validator set delta is not empty")
)

// Topic is an interface for p2p message gossiping
type Topic interface {
	Publish(obj proto.Message) error
	Subscribe(handler func(obj interface{}, from peer.ID)) error
}

var _ Bridge = (*bridge)(nil)

// bridge is a struct that manages different bridges
type bridge struct {
	bridgeManagers  map[uint64]BridgeManager
	state           *BridgeManagerStore
	internalChainID uint64
	relayer         BridgeEventRelayer
	logger          hclog.Logger
}

// Bridge is an interface that defines functions that a bridge must implement
type Bridge interface {
	oracle.ReadOnlyOracle
	oracle.TxnOracle
	Close()
	BridgeBatch(pendingBlockNumber uint64) ([]*BridgeBatchSigned, error)
}

var _ Bridge = (*DummyBridge)(nil)

type DummyBridge struct{}

func (d *DummyBridge) Close()                                       {}
func (d *DummyBridge) PostBlock(req *oracle.PostBlockRequest) error { return nil }
func (d *DummyBridge) PostEpoch(req *oracle.PostEpochRequest) error { return nil }
func (d *DummyBridge) BridgeBatch(pendingBlockNumber uint64) ([]*BridgeBatchSigned, error) {
	return nil, nil
}
func (d *DummyBridge) GetTransactions(blockInfo oracle.NewBlockInfo) ([]*types.Transaction, error) {
	return nil, nil
}
func (d *DummyBridge) VerifyTransactions(blockInfo oracle.NewBlockInfo, txs []*types.Transaction) error {
	return nil
}

// NewBridge creates a new instance of bridge
func NewBridge(runtime Runtime,
	state *state.State,
	runtimeConfig *config.Runtime,
	bridgeTopic Topic,
	eventProvider *state.EventProvider,
	blockchain polychain.Blockchain,
	logger hclog.Logger,
	dbTx *bolt.Tx) (Bridge, error) {
	if len(runtimeConfig.GenesisConfig.Bridge) == 0 {
		return &DummyBridge{}, nil
	}

	internalChainID := blockchain.GetChainID()
	chainIDs := make([]uint64, 0, len(runtimeConfig.GenesisConfig.Bridge)+1)
	chainIDs = append(chainIDs, internalChainID)

	for chainID := range runtimeConfig.GenesisConfig.Bridge {
		chainIDs = append(chainIDs, chainID)
	}

	store, err := newBridgeManagerStore(state.DB(), dbTx, chainIDs)
	if err != nil {
		return nil, fmt.Errorf("error creating bridge manager store, err: %w", err)
	}

	bridge := &bridge{
		bridgeManagers:  make(map[uint64]BridgeManager),
		state:           store,
		internalChainID: internalChainID,
		logger:          logger,
	}

	for externalChainID, cfg := range runtimeConfig.GenesisConfig.Bridge {
		bridgeManager := newBridgeManager(logger, store, &bridgeEventManagerConfig{
			bridgeCfg:         cfg,
			topic:             bridgeTopic,
			key:               runtimeConfig.Key,
			maxNumberOfEvents: maxNumberOfBatchEvents,
		}, runtime, externalChainID, internalChainID)
		bridge.bridgeManagers[externalChainID] = bridgeManager

		if err := bridgeManager.Start(runtimeConfig); err != nil {
			return nil, fmt.Errorf("error starting bridge manager for chainID: %d, err: %w", externalChainID, err)
		}

		eventProvider.Subscribe(bridgeManager)
	}

	relayer, err := newBridgeEventRelayer(blockchain, runtimeConfig, logger, store)
	if err != nil {
		return nil, err
	}

	bridge.relayer = relayer

	if err := relayer.Start(runtimeConfig, eventProvider); err != nil {
		return nil, fmt.Errorf("error starting bridge event relayer, err: %w", err)
	}

	return bridge, nil
}

// Close calls Close on each bridge manager, which stops ongoing go routines in manager
func (b *bridge) Close() {
	for _, bridgeManager := range b.bridgeManagers {
		bridgeManager.Close()
	}

	b.relayer.Close()
}

// PostBlock is a function executed on every block finalization (either by consensus or syncer)
// and calls PostBlock in each bridge manager
func (b *bridge) PostBlock(req *oracle.PostBlockRequest) error {
	for chainID, bridgeManager := range b.bridgeManagers {
		if err := bridgeManager.PostBlock(); err != nil {
			return fmt.Errorf("erorr bridge post block, chainID: %d, err: %w", chainID, err)
		}
	}

	return nil
}

// PostEpoch is a function executed on epoch ending / start of new epoch
// and calls PostEpoch in each bridge manager
func (b *bridge) PostEpoch(req *oracle.PostEpochRequest) error {
	if err := b.state.cleanEpochsFromDB(req.DBTx); err != nil {
		// we just log this, as it is not critical
		b.logger.Error("error cleaning epochs from db", "err", err)
	}

	if err := b.state.insertEpoch(req.NewEpochID, req.DBTx, b.internalChainID); err != nil {
		return fmt.Errorf("error inserting epoch to internal, err: %w", err)
	}

	for chainID, bridgeManager := range b.bridgeManagers {
		if err := bridgeManager.PostEpoch(req); err != nil {
			return fmt.Errorf("erorr bridge post epoch, chainID: %d, err: %w", chainID, err)
		}
	}

	return nil
}

// BridgeBatch returns the pending signed bridge batches as a list of signed bridge batches
func (b *bridge) BridgeBatch(pendingBlockNumber uint64) ([]*BridgeBatchSigned, error) {
	bridgeBatches := make([]*BridgeBatchSigned, 0)

	for chainID, bridgeManager := range b.bridgeManagers {
		bridgeBatch, err := bridgeManager.BridgeBatch(pendingBlockNumber)
		if err != nil {
			return nil, fmt.Errorf("error while getting signed batches for chainID: %d, err: %w", chainID, err)
		}

		if bridgeBatch != nil {
			bridgeBatches = append(bridgeBatches, bridgeBatch)
		}
	}

	return bridgeBatches, nil
}

// GetTransactions returns the system transactions associated with the given block.
func (b *bridge) GetTransactions(blockInfo oracle.NewBlockInfo) ([]*types.Transaction, error) {
	var txs []*types.Transaction

	if blockInfo.IsFirstBlockOfEpoch && blockInfo.ParentBlock.Number > 1 {
		tx, err := createCommitValidatorSetTxn(blockInfo)
		if err != nil {
			return nil, fmt.Errorf("error while creating commit validator set tx, err: %w", err)
		}

		if tx != nil {
			txs = append(txs, tx)
		}
	}

	if blockInfo.IsEndOfSprint {
		for chainID, bridgeManager := range b.bridgeManagers {
			bridgeBatch, err := bridgeManager.BridgeBatch(blockInfo.CurrentBlock())
			if err != nil {
				return nil, fmt.Errorf("error while getting signed batches for chainID: %d, err: %w", chainID, err)
			}

			if bridgeBatch == nil {
				continue
			}

			tx, err := createBridgeBatchTx(bridgeBatch)
			if err != nil {
				return nil, fmt.Errorf("error while creating bridge batch tx for chainID: %d, err: %w", chainID, err)
			}

			txs = append(txs, tx)
		}
	}

	return txs, nil
}

// VerifyTransactions verifies the system transactions associated with the given block.
func (b *bridge) VerifyTransactions(blockInfo oracle.NewBlockInfo, txs []*types.Transaction) error {
	var (
		bridgeBatchTxExists      bool
		commitValidatorSetExists bool
		commitBatchFn            = new(contractsapi.CommitBatchBridgeStorageFn)
		commitValidatorSetFn     = new(contractsapi.CommitValidatorSetBridgeStorageFn)
	)

	for _, tx := range txs {
		if tx.Type() != types.StateTxType {
			continue // not a state transaction, we don't care about it
		}

		txData := tx.Input()

		if len(txData) < helpers.AbiMethodIDLength {
			return helpers.ErrStateTransactionInputInvalid
		}

		sig := txData[:helpers.AbiMethodIDLength]

		if bytes.Equal(sig, commitBatchFn.Sig()) {
			if !blockInfo.IsEndOfSprint {
				return errBridgeBatchTxInNonSprintBlock
			}

			if bridgeBatchTxExists {
				return errBridgeBatchTxExists
			}

			bridgeBatchTxExists = true

			bridgeBatchFn := &BridgeBatchSigned{}
			if err := bridgeBatchFn.DecodeAbi(txData); err != nil {
				return fmt.Errorf("error decoding bridge batch tx: %w", err)
			}

			if err := VerifyBridgeBatchTx(blockInfo.CurrentBlock(), tx.Hash(),
				bridgeBatchFn, blockInfo.CurrentEpochValidatorSet); err != nil {
				return err
			}
		} else if bytes.Equal(sig, commitValidatorSetFn.Sig()) {
			if commitValidatorSetExists {
				// if we already validated commit validator set tx,
				// that means someone added more than one commit validator set tx to block,
				// which is invalid
				return errCommitValidatorSetExists
			}

			commitValidatorSetExists = true

			if err := commitValidatorSetFn.DecodeAbi(txData); err != nil {
				return fmt.Errorf("error decoding commit validator set tx: %w", err)
			}

			if err := verifyCommitValidatorSetTx(blockInfo, commitValidatorSetFn); err != nil {
				return fmt.Errorf("error while verifying commit validator set transaction. error: %w", err)
			}
		}
	}

	if blockInfo.IsFirstBlockOfEpoch {
		hasValidatorChanges, err := doesParentBlockHasValidatorChanges(blockInfo.ParentBlock)
		if err != nil {
			return err
		}

		if hasValidatorChanges && !commitValidatorSetExists {
			// this is a check if commit validator set transaction is not in the list of transactions at all
			// but it should be
			return errCommitValidatorSetTxDoesNotExist
		}
	}

	return nil
}

// createBridgeBatchTx builds bridge batch commit transaction
func createBridgeBatchTx(signedBridgeBatch *BridgeBatchSigned) (*types.Transaction, error) {
	inputData, err := signedBridgeBatch.EncodeAbi()
	if err != nil {
		return nil, fmt.Errorf("failed to encode input data for bridge batch registration: %w", err)
	}

	return helpers.CreateStateTransactionWithData(contracts.BridgeStorageContract, inputData), nil
}

// VerifyBridgeBatchTx validates bridge batch transaction
func VerifyBridgeBatchTx(blockNumber uint64, txHash types.Hash,
	signedBridgeBatch *BridgeBatchSigned,
	validators validator.ValidatorSet) error {
	signers, err := validators.Accounts().GetFilteredValidators(signedBridgeBatch.AggSignature.Bitmap)
	if err != nil {
		return fmt.Errorf("failed to retrieve signers for state tx (%s): %w", txHash, err)
	}

	if !validators.HasQuorum(blockNumber, signers.GetAddressesAsSet()) {
		return fmt.Errorf("quorum size not reached for state tx (%s)", txHash)
	}

	batchHash, err := signedBridgeBatch.Hash()
	if err != nil {
		return err
	}

	signature, err := bls.UnmarshalSignature(signedBridgeBatch.AggSignature.AggregatedSignature)
	if err != nil {
		return fmt.Errorf("error for state tx (%s) while unmarshaling signature: %w", txHash, err)
	}

	verified := signature.VerifyAggregated(signers.GetBlsKeys(), batchHash.Bytes(), signer.DomainBridge)
	if !verified {
		return fmt.Errorf("invalid signature for state tx (%s)", txHash)
	}

	return nil
}

// createCommitValidatorSetTxn creates a system transaction that updates the validator set
// on BridgeStorage contract
func createCommitValidatorSetTxn(bi oracle.NewBlockInfo) (*types.Transaction, error) {
	parentExtra, err := polytypes.GetIbftExtra(bi.ParentBlock.ExtraData)
	if err != nil {
		return nil, fmt.Errorf("failed to get parent extra data: %w", err)
	}

	if parentExtra.Validators == nil || parentExtra.Validators.IsEmpty() {
		// if there was no change in the validator set we don't need to update it
		return nil, nil
	}

	signature, err := bls.UnmarshalSignature(parentExtra.Committed.AggregatedSignature)
	if err != nil {
		return nil, err
	}

	signatureBig, err := signature.ToBigInt()
	if err != nil {
		return nil, err
	}

	input := &contractsapi.CommitValidatorSetBridgeStorageFn{
		NewValidatorSet: bi.CurrentEpochValidatorSet.Accounts().ToABIBinding(),
		Signature:       signatureBig,
		Bitmap:          parentExtra.Committed.Bitmap,
	}

	inputData, err := input.EncodeAbi()
	if err != nil {
		return nil, fmt.Errorf("failed to encode input data for BridgeStorage validator set update: %w", err)
	}

	return helpers.CreateStateTransactionWithData(contracts.BridgeStorageContract, inputData), nil
}

// verifyCommitValidatorSetTx verifies commit validator set state transaction
func verifyCommitValidatorSetTx(bi oracle.NewBlockInfo,
	commitValidatorSetTx *contractsapi.CommitValidatorSetBridgeStorageFn) error {
	if !bi.IsFirstBlockOfEpoch {
		return errCommitValidatorSetTxNotExpected
	}

	hasValidatorChanges, err := doesParentBlockHasValidatorChanges(bi.ParentBlock)
	if err != nil {
		return err
	}

	if !hasValidatorChanges {
		// if there was no change in the validator set, but we have commit validator set tx
		// then this case is invalid and we return an error
		return errCommitValidatorSetTxInvalid
	}

	expectedValidators := bi.CurrentEpochValidatorSet.Accounts()
	txValidators := commitValidatorSetTx.GetValidatorsAsMap()

	for _, expectedValidator := range expectedValidators {
		txValidator, exists := txValidators[expectedValidator.Address]
		if !exists {
			return fmt.Errorf("validator %s is missing in the commit validator set transaction",
				expectedValidator.Address.String())
		}

		if expectedValidator.VotingPower.Cmp(txValidator.VotingPower) != 0 {
			return fmt.Errorf("voting power mismatch for validator %s. Expected %s, but got %s",
				expectedValidator.Address.String(), expectedValidator.VotingPower.String(), txValidator.VotingPower.String())
		}

		expectedValidatorBlsKey := expectedValidator.BlsKey.ToBigInt()
		txValidatorBlsKey := txValidator.BlsKey

		for i := range expectedValidatorBlsKey {
			if expectedValidatorBlsKey[i].Cmp(txValidatorBlsKey[i]) != 0 {
				return fmt.Errorf("BLS key mismatch for validator %s at index %d. Expected %s, but got %s",
					expectedValidator.Address.String(), i, expectedValidatorBlsKey[i].String(), txValidatorBlsKey[i].String())
			}
		}
	}

	return nil
}

func doesParentBlockHasValidatorChanges(parentBlock *types.Header) (bool, error) {
	extra, err := polytypes.GetIbftExtra(parentBlock.ExtraData)
	if err != nil {
		return false, fmt.Errorf("failed to get parent extra data: %w", err)
	}

	return (extra.Validators != nil && !extra.Validators.IsEmpty()), nil
}
