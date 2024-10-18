package oracle

import (
	"context"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/types"
	"golang.org/x/sync/errgroup"
)

// NewBlockInfo holds information about the new block
type NewBlockInfo struct {
	// IsEndOfEpoch indicates if the block is the last block of the epoch
	IsEndOfEpoch bool
	// IsFirstBlockOfEpoch indicates if the block is the first block of the epoch
	IsFirstBlockOfEpoch bool
	// IsEndOfSprint indicates if the block is the last block of the sprint
	IsEndOfSprint bool
	// FirstBlockInEpoch is the number of the first block in the epoch
	FirstBlockInEpoch uint64
	// CurrentEpoch is the current epoch number
	CurrentEpoch uint64
	// EpochSize is the number of blocks in the epoch
	EpochSize uint64
	// ParentBlock is the parent block of the current block
	ParentBlock *types.Header
	// CurrentEpochValidatorSet is the validator set for the current epoch
	CurrentEpochValidatorSet validator.ValidatorSet
	// NewValidatorSetDelta carries the changes in the validator set
	NewValidatorSetDelta *validator.ValidatorSetDelta
}

func (b NewBlockInfo) CurrentBlock() uint64 {
	return b.ParentBlock.Number + 1
}

// Oracle represents a common interface for all oracle types
type Oracle interface {
	// Close closes the oracle
	Close()
}

// ReadOnlyOracle is an oracle that only reads data from blocks
type ReadOnlyOracle interface {
	Oracle
	// PostBlock posts a block to the oracle
	PostBlock(postBlockReq *PostBlockRequest) error
	// PostEpoch posts an epoch to the oracle
	PostEpoch(postEpochReq *PostEpochRequest) error
}

// TxnOracle is an oracle that can provide and verify system transactions
type TxnOracle interface {
	Oracle
	// GetTransactions returns the system transactions
	GetTransactions(blockInfo NewBlockInfo) ([]*types.Transaction, error)
	// VerifyTransaction verifies system transactions
	VerifyTransactions(blockInfo NewBlockInfo, txs []*types.Transaction) error
}

// Oracles is a collection of oracles
type Oracles []Oracle

// Close closes all oracles
func (o Oracles) Close() {
	for _, oracle := range o {
		oracle.Close()
	}
}

// PostBlock posts a block to all oracles
func (o Oracles) PostBlock(postBlockReq *PostBlockRequest) error {
	for _, oracle := range o {
		ro, ok := oracle.(ReadOnlyOracle)
		if !ok {
			continue
		}

		if err := ro.PostBlock(postBlockReq); err != nil {
			return err
		}
	}

	return nil
}

// PostEpoch posts an epoch to all oracles
func (o Oracles) PostEpoch(postEpochReq *PostEpochRequest) error {
	for _, oracle := range o {
		ro, ok := oracle.(ReadOnlyOracle)
		if !ok {
			continue
		}

		if err := ro.PostEpoch(postEpochReq); err != nil {
			return err
		}
	}

	return nil
}

// GetTransactions returns all system transactions from all oracles
func (o Oracles) GetTransactions(blockInfo NewBlockInfo) ([]*types.Transaction, error) {
	var allTxs []*types.Transaction

	for _, oracle := range o {
		to, ok := oracle.(TxnOracle)
		if !ok {
			continue
		}

		oracleTxs, err := to.GetTransactions(blockInfo)
		if err != nil {
			return nil, err
		}

		allTxs = append(allTxs, oracleTxs...)
	}

	return allTxs, nil
}

// VerifyTransactions verifies all system transactions from all oracles
func (o Oracles) VerifyTransactions(blockInfo NewBlockInfo, txs []*types.Transaction) error {
	g, _ := errgroup.WithContext(context.Background())

	for _, oracle := range o {
		to, ok := oracle.(TxnOracle)
		if !ok {
			continue
		}

		g.Go(func() error {
			return to.VerifyTransactions(blockInfo, txs)
		})
	}

	return g.Wait()
}
