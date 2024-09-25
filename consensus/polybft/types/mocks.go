package types

import (
	"time"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/validator"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/mock"
	bolt "go.etcd.io/bbolt"
)

var _ Polybft = (*PolybftBackendMock)(nil)

type PolybftBackendMock struct {
	mock.Mock
}

// GetValidators retrieves validator set for the given block
func (p *PolybftBackendMock) GetValidators(blockNumber uint64, parents []*types.Header) (validator.AccountSet, error) {
	args := p.Called(blockNumber, parents)
	if len(args) == 1 {
		accountSet, _ := args.Get(0).(validator.AccountSet)

		return accountSet, nil
	} else if len(args) == 2 {
		accountSet, _ := args.Get(0).(validator.AccountSet)

		return accountSet, args.Error(1)
	}

	panic("polybftBackendMock.GetValidators doesn't support such combination of arguments") //nolint:gocritic
}

func (p *PolybftBackendMock) GetValidatorsWithTx(blockNumber uint64, parents []*types.Header,
	dbTx *bolt.Tx) (validator.AccountSet, error) {
	args := p.Called(blockNumber, parents, dbTx)
	if len(args) == 1 {
		accountSet, _ := args.Get(0).(validator.AccountSet)

		return accountSet, nil
	} else if len(args) == 2 {
		accountSet, _ := args.Get(0).(validator.AccountSet)

		return accountSet, args.Error(1)
	}

	panic("polybftBackendMock.GetValidatorsWithTx doesn't support such combination of arguments") //nolint:gocritic
}

func (p *PolybftBackendMock) SetBlockTime(blockTime time.Duration) {
	p.Called(blockTime)
}
