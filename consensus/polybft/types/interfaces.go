package types

import (
	"time"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/types"
	bolt "go.etcd.io/bbolt"
)

// Polybft is the interface that provides the necessary functions
// to interact with the polybft consensus
type Polybft interface {
	// GetValidators retrieves validator set for the given block
	GetValidators(blockNumber uint64, parents []*types.Header) (validator.AccountSet, error)

	// GetValidators retrieves validator set for the given block
	// Function expects that db tx is already open
	GetValidatorsWithTx(blockNumber uint64, parents []*types.Header,
		dbTx *bolt.Tx) (validator.AccountSet, error)

	// SetBlockTime updates the block time
	SetBlockTime(blockTime time.Duration)
}
