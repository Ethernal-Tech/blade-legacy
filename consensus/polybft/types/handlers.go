package types

import (
	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/config"
	systemstate "github.com/0xPolygon/polygon-edge/consensus/polybft/system_state"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/types"
	bolt "go.etcd.io/bbolt"
)

type PostBlockRequest struct {
	// FullBlock is a reference of the executed block
	FullBlock *types.FullBlock
	// Epoch is the epoch number of the executed block
	Epoch uint64
	// IsEpochEndingBlock indicates if this was the last block of given epoch
	IsEpochEndingBlock bool
	// DBTx is the opened transaction on state store (in our case boltDB)
	// used to save necessary data on PostBlock
	DBTx *bolt.Tx
	// CurrentClientConfig is the latest client configuration
	CurrentClientConfig *config.PolyBFT
	// Forks holds forks configuration
	Forks *chain.Forks
}

type PostEpochRequest struct {
	// NewEpochID is the id of the new epoch
	NewEpochID uint64

	// FirstBlockOfEpoch is the number of the epoch beginning block
	FirstBlockOfEpoch uint64

	// SystemState is the state of the governance smart contracts
	// after this block
	SystemState systemstate.SystemState

	// ValidatorSet is the validator set for the new epoch
	ValidatorSet validator.ValidatorSet

	// DBTx is the opened transaction on state store (in our case boltDB)
	// used to save necessary data on PostEpoch
	DBTx *bolt.Tx
	// Forks holds forks configuration
	Forks *chain.Forks
}

// BlockInfo holds information about the block
type BlockInfo struct {
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
}

func (b BlockInfo) CurrentBlock() uint64 {
	return b.ParentBlock.Number + 1
}
