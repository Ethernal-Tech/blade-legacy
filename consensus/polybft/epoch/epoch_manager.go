package epoch

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/blockchain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/governance"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/helpers"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/forkmanager"
	"github.com/0xPolygon/polygon-edge/types"
)

var (
	errCommitEpochTxDoesNotExist   = errors.New("commit epoch transaction is not found in the epoch ending block")
	errCommitEpochTxNotExpected    = errors.New("didn't expect commit epoch transaction in a non epoch ending block")
	errCommitEpochTxSingleExpected = errors.New("only one commit epoch transaction is allowed " +
		"in an epoch ending block")
	errDistributeRewardsTxDoesNotExist = errors.New("distribute rewards transaction is " +
		"not found in the given block, though it is expected to be present")
	errDistributeRewardsTxNotExpected = errors.New("distribute rewards transaction " +
		"is not expected at this block")
	errDistributeRewardsTxSingleExpected = errors.New("only one distribute rewards transaction is " +
		"allowed in the given block")
)

var _ polytypes.Oracle = (*EpochManager)(nil)

// EpochManager is a struct that implements Oracle interface and provides
// system (state) transactions regarding closing epochs and distributing rewards.
type EpochManager struct {
	backend    polytypes.Polybft
	blockchain blockchain.Blockchain
	forks      *chain.Forks
}

// NewEpochManager creates a new instance of EpochManager.
func NewEpochManager(forks *chain.Forks, backend polytypes.Polybft, blockchain blockchain.Blockchain) *EpochManager {
	return &EpochManager{
		forks:      forks,
		backend:    backend,
		blockchain: blockchain,
	}
}

// GetTransactions returns the system transactions associated with the given block.
func (e *EpochManager) GetTransactions(blockInfo polytypes.BlockInfo) ([]*types.Transaction, error) {
	var txs []*types.Transaction
	if blockInfo.IsEndOfEpoch {
		commitEpochTxn, err := createCommitEpochTx(blockInfo)
		if err != nil {
			return nil, fmt.Errorf("cannot create commit epoch transaction: %w", err)
		}

		txs = append(txs, commitEpochTxn)
	}

	if governance.IsRewardDistributionBlock(e.forks, blockInfo.IsFirstBlockOfEpoch,
		blockInfo.IsEndOfEpoch, blockInfo.CurrentBlock()) {
		distributeRewardsTxn, err := e.createDistributeRewardsTx(blockInfo)
		if err != nil {
			return nil, fmt.Errorf("cannot create distribute rewards transaction: %w", err)
		}

		txs = append(txs, distributeRewardsTxn)
	}

	return txs, nil
}

// VerifyTransactions verifies the system transactions associated with the given block.
func (e *EpochManager) VerifyTransactions(blockInfo polytypes.BlockInfo, txs []*types.Transaction) error {
	var (
		commitEpochFn             contractsapi.CommitEpochEpochManagerFn
		distributeRewardsFn       contractsapi.DistributeRewardForEpochManagerFn
		distributeRewardsTxExists bool
		commitEpochTxExists       bool
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
		if bytes.Equal(sig, commitEpochFn.Sig()) {
			if commitEpochTxExists {
				// if we already validated commit epoch tx,
				// that means someone added more than one commit epoch tx to block,
				// which is invalid
				return errCommitEpochTxSingleExpected
			}

			commitEpochTxExists = true

			if err := e.verifyCommitEpochTx(blockInfo, tx); err != nil {
				return fmt.Errorf("error while verifying commit epoch transaction. error: %w", err)
			}
		} else if bytes.Equal(sig, distributeRewardsFn.Sig()) {
			if distributeRewardsTxExists {
				// if we already validated distribute rewards tx,
				// that means someone added more than one distribute rewards tx to block,
				// which is invalid
				return errDistributeRewardsTxSingleExpected
			}

			distributeRewardsTxExists = true

			if err := e.verifyDistributeRewardsTx(blockInfo, tx); err != nil {
				return fmt.Errorf("error while verifying distribute rewards transaction. error: %w", err)
			}
		}
	}

	if blockInfo.IsEndOfEpoch {
		if !commitEpochTxExists {
			// this is a check if commit epoch transaction is not in the list of transactions at all
			// but it should be
			return errCommitEpochTxDoesNotExist
		}
	}

	if governance.IsRewardDistributionBlock(e.forks, blockInfo.IsFirstBlockOfEpoch,
		blockInfo.IsEndOfEpoch, blockInfo.ParentBlock.Number+1) {
		if !distributeRewardsTxExists {
			// this is a check if distribute rewards transaction is not in the list of transactions at all
			// but it should be
			return errDistributeRewardsTxDoesNotExist
		}
	}

	return nil
}

// verifyCommitEpochTx creates commit epoch transaction and compares its hash with the one extracted from the block.
func (e *EpochManager) verifyCommitEpochTx(blockInfo polytypes.BlockInfo,
	commitEpochTxFromProposer *types.Transaction) error {
	if blockInfo.IsEndOfEpoch {
		localCommitEpochTx, err := createCommitEpochTx(blockInfo)
		if err != nil {
			return err
		}

		if commitEpochTxFromProposer.Hash() != localCommitEpochTx.Hash() {
			return fmt.Errorf(
				"invalid commit epoch transaction. Expected '%s', but got '%s' commit epoch transaction hash",
				localCommitEpochTx.Hash(),
				commitEpochTxFromProposer.Hash(),
			)
		}

		return nil
	}

	return errCommitEpochTxNotExpected
}

// createDistributeRewardsTx calculates distribute rewards input data and creates a state transaction
func (e *EpochManager) createDistributeRewardsTx(bi polytypes.BlockInfo,
) (*types.Transaction, error) {
	var (
		// epoch size is the number of blocks that really happened
		// because of slashing, epochs might not have the configured number of blocks
		epochSize          = uint64(0)
		uptimeCounter      = map[types.Address]int64{}
		blockHeader        = bi.ParentBlock // start calculating from this block
		epochID            = bi.CurrentEpoch
		pendingBlockNumber = bi.CurrentBlock()
	)

	if forkmanager.GetInstance().IsForkEnabled(chain.Governance, pendingBlockNumber) {
		// if governance is enabled, we are distributing rewards for previous epoch
		// at the beginning of a new epoch, so modify epochID
		epochID--
	}

	getSealersForBlock := func(blockExtra *polytypes.Extra, validators validator.AccountSet) error {
		signers, err := validators.GetFilteredValidators(blockExtra.Parent.Bitmap)
		if err != nil {
			return err
		}

		for _, a := range signers.GetAddresses() {
			uptimeCounter[a]++
		}

		epochSize++

		return nil
	}

	blockExtra, err := polytypes.GetIbftExtra(blockHeader.ExtraData)
	if err != nil {
		return nil, err
	}

	previousBlockHeader, previousBlockExtra, err := helpers.GetBlockData(blockHeader.Number-1, e.blockchain)
	if err != nil {
		return nil, err
	}

	// calculate uptime starting from last block - 1 in epoch until first block in given epoch
	for previousBlockExtra.BlockMetaData.EpochNumber == blockExtra.BlockMetaData.EpochNumber {
		validators, err := e.backend.GetValidators(blockHeader.Number-1, nil)
		if err != nil {
			return nil, err
		}

		if err := getSealersForBlock(blockExtra, validators); err != nil {
			return nil, err
		}

		blockHeader, blockExtra, err = helpers.GetBlockData(blockHeader.Number-1, e.blockchain)
		if err != nil {
			return nil, err
		}

		previousBlockHeader, previousBlockExtra, err = helpers.GetBlockData(previousBlockHeader.Number-1, e.blockchain)
		if err != nil {
			return nil, err
		}
	}

	lookbackSize := governance.GetLookbackSizeForRewardDistribution(e.forks, pendingBlockNumber)

	// calculate uptime for blocks from previous epoch that were not processed in previous uptime
	// since we can not calculate uptime for the last block in epoch (because of parent signatures)
	if blockHeader.Number > lookbackSize {
		for i := uint64(0); i < lookbackSize; i++ {
			validators, err := e.backend.GetValidators(blockHeader.Number-2, nil)
			if err != nil {
				return nil, err
			}

			if err := getSealersForBlock(blockExtra, validators); err != nil {
				return nil, err
			}

			blockHeader, blockExtra, err = helpers.GetBlockData(blockHeader.Number-1, e.blockchain)
			if err != nil {
				return nil, err
			}
		}
	}

	// include the data in the uptime counter in a deterministic way
	addrSet := []types.Address{}

	for addr := range uptimeCounter {
		addrSet = append(addrSet, addr)
	}

	uptime := make([]*contractsapi.Uptime, len(addrSet))

	sort.Slice(addrSet, func(i, j int) bool {
		return bytes.Compare(addrSet[i][:], addrSet[j][:]) > 0
	})

	for i, addr := range addrSet {
		uptime[i] = &contractsapi.Uptime{
			Validator:    addr,
			SignedBlocks: new(big.Int).SetInt64(uptimeCounter[addr]),
		}
	}

	distributeRewardsInput := &contractsapi.DistributeRewardForEpochManagerFn{
		EpochID:   new(big.Int).SetUint64(epochID),
		Uptime:    uptime,
		EpochSize: new(big.Int).SetUint64(epochSize),
	}

	input, err := distributeRewardsInput.EncodeAbi()
	if err != nil {
		return nil, fmt.Errorf("failed to encode distribute rewards input: %w", err)
	}

	return helpers.CreateStateTransactionWithData(contracts.EpochManagerContract, input), nil
}

// verifyDistributeRewardsTx creates distribute rewards transaction
// and compares its hash with the one extracted from the block.
func (e *EpochManager) verifyDistributeRewardsTx(bi polytypes.BlockInfo,
	distributeRewardsTxnFromProposer *types.Transaction) error {
	// we don't have distribute rewards tx if we just started the chain
	if governance.IsRewardDistributionBlock(e.forks, bi.IsFirstBlockOfEpoch, bi.IsEndOfEpoch, bi.CurrentBlock()) {
		localDistributeRewardsTx, err := e.createDistributeRewardsTx(bi)
		if err != nil {
			return err
		}

		if distributeRewardsTxnFromProposer.Hash() != localDistributeRewardsTx.Hash() {
			return fmt.Errorf(
				"invalid distribute rewards transaction. Expected '%s', but got '%s' distribute rewards hash",
				localDistributeRewardsTx.Hash(),
				distributeRewardsTxnFromProposer.Hash(),
			)
		}

		return nil
	}

	return errDistributeRewardsTxNotExpected
}

// createCommitEpochTx create a StateTransaction, which invokes ValidatorSet smart contract
// and sends all the necessary metadata to it.
func createCommitEpochTx(blockInfo polytypes.BlockInfo) (*types.Transaction, error) {
	commitEpochFn := &contractsapi.CommitEpochEpochManagerFn{
		ID: new(big.Int).SetUint64(blockInfo.CurrentEpoch),
		Epoch: &contractsapi.Epoch{
			StartBlock: new(big.Int).SetUint64(blockInfo.FirstBlockInEpoch),
			EndBlock:   new(big.Int).SetUint64(blockInfo.ParentBlock.Number + 1),
			EpochRoot:  types.Hash{},
		},
		EpochSize: new(big.Int).SetUint64(blockInfo.EpochSize),
	}

	input, err := commitEpochFn.EncodeAbi()
	if err != nil {
		return nil, fmt.Errorf("failed to encode commit epoch input: %w", err)
	}

	return helpers.CreateStateTransactionWithData(contracts.EpochManagerContract, input), nil
}
