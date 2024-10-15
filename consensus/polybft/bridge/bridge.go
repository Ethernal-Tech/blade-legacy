package bridge

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"github.com/0xPolygon/polygon-edge/bls"
	polychain "github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/helpers"
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
	errBridgeBatchTxExists           = errors.New("only one bridge batch tx is allowed per block")
	errBridgeBatchTxInNonSprintBlock = errors.New("bridge batch tx is not allowed in non-sprint block")
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

	lock       sync.RWMutex
	validators validator.ValidatorSet
}

// Bridge is an interface that defines functions that a bridge must implement
type Bridge interface {
	polytypes.Oracle
	Close()
	PostBlock(req *polytypes.PostBlockRequest) error
	PostEpoch(req *polytypes.PostEpochRequest) error
	BridgeBatch(pendingBlockNumber uint64) ([]*BridgeBatchSigned, error)
}

var _ Bridge = (*DummyBridge)(nil)

type DummyBridge struct{}

func (d *DummyBridge) Close()                                          {}
func (d *DummyBridge) PostBlock(req *polytypes.PostBlockRequest) error { return nil }
func (d *DummyBridge) PostEpoch(req *polytypes.PostEpochRequest) error { return nil }
func (d *DummyBridge) BridgeBatch(pendingBlockNumber uint64) ([]*BridgeBatchSigned, error) {
	return nil, nil
}
func (d *DummyBridge) InsertEpoch(epoch uint64, tx *bolt.Tx) error { return nil }
func (d *DummyBridge) GetTransactions(blockInfo polytypes.BlockInfo) ([]*types.Transaction, error) {
	return nil, nil
}
func (d *DummyBridge) VerifyTransactions(blockInfo polytypes.BlockInfo, txs []*types.Transaction) error {
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
	}

	relayer, err := newBridgeEventRelayer(blockchain, runtimeConfig, logger)
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
func (b *bridge) PostBlock(req *polytypes.PostBlockRequest) error {
	for chainID, bridgeManager := range b.bridgeManagers {
		if err := bridgeManager.PostBlock(); err != nil {
			return fmt.Errorf("erorr bridge post block, chainID: %d, err: %w", chainID, err)
		}
	}

	return nil
}

// PostEpoch is a function executed on epoch ending / start of new epoch
// and calls PostEpoch in each bridge manager
func (b *bridge) PostEpoch(req *polytypes.PostEpochRequest) error {
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

	b.lock.Lock()
	defer b.lock.Unlock()

	b.validators = req.ValidatorSet

	return nil
}

// BridgeBatch returns the pending signed bridge batches as a list of signed bridge batches
func (b *bridge) BridgeBatch(pendingBlockNumber uint64) ([]*BridgeBatchSigned, error) {
	bridgeBatches := make([]*BridgeBatchSigned, 0, len(b.bridgeManagers))

	for chainID, bridgeManager := range b.bridgeManagers {
		bridgeBatch, err := bridgeManager.BridgeBatch(pendingBlockNumber)
		if err != nil {
			return nil, fmt.Errorf("error while getting signed batches for chainID: %d, err: %w", chainID, err)
		}

		bridgeBatches = append(bridgeBatches, bridgeBatch)
	}

	return bridgeBatches, nil
}

// GetTransactions returns the system transactions associated with the given block.
func (b *bridge) GetTransactions(blockInfo polytypes.BlockInfo) ([]*types.Transaction, error) {
	var txs []*types.Transaction

	if blockInfo.IsEndOfSprint {
		for chainID, bridgeManager := range b.bridgeManagers {
			bridgeBatch, err := bridgeManager.BridgeBatch(blockInfo.CurrentBlock())
			if err != nil {
				return nil, fmt.Errorf("error while getting signed batches for chainID: %d, err: %w", chainID, err)
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
func (b *bridge) VerifyTransactions(blockInfo polytypes.BlockInfo, txs []*types.Transaction) error {
	var (
		bridgeBatchTxExists bool
		commitBatchFn       = new(contractsapi.CommitBatchBridgeStorageFn)
		validators          validator.ValidatorSet
	)

	b.lock.RLock()
	validators = b.validators
	b.lock.RUnlock()

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

			if err := verifyBridgeBatchTx(blockInfo.CurrentBlock(), tx.Hash(),
				bridgeBatchFn, validators); err != nil {
				return err
			}
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

// verifyBridgeBatchTx validates bridge batch transaction
func verifyBridgeBatchTx(blockNumber uint64, txHash types.Hash,
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
