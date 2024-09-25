package systemstate

import (
	"math/big"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/Ethernal-Tech/ethgo"
	"github.com/Ethernal-Tech/ethgo/contract"
	"github.com/stretchr/testify/mock"
)

var _ SystemState = (*SystemStateMock)(nil)

type SystemStateMock struct {
	mock.Mock
}

func (m *SystemStateMock) GetNextCommittedIndex(chainID uint64, chainType ChainType) (uint64, error) {
	args := m.Called()

	if len(args) == 1 {
		index, _ := args.Get(0).(uint64)

		return index, nil
	} else if len(args) == 2 {
		index, _ := args.Get(0).(uint64)

		return index, args.Error(1)
	}

	return 0, nil
}

func (m *SystemStateMock) GetBridgeBatchByNumber(numberOfBatch *big.Int) (
	*contractsapi.SignedBridgeMessageBatch, error) {
	args := m.Called()
	if len(args) == 1 {
		batch, _ := args.Get(0).(contractsapi.SignedBridgeMessageBatch)

		return &batch, nil
	} else if len(args) == 2 {
		batch, _ := args.Get(0).(contractsapi.SignedBridgeMessageBatch)

		return &batch, args.Error(1)
	}

	return &contractsapi.SignedBridgeMessageBatch{}, nil
}

func (m *SystemStateMock) GetValidatorSetByNumber(numberOfValidatorSet *big.Int) (
	*contractsapi.SignedValidatorSet, error) {
	args := m.Called()
	if len(args) == 1 {
		validatorSet, _ := args.Get(0).(contractsapi.SignedValidatorSet)

		return &validatorSet, nil
	} else if len(args) == 2 {
		batch, _ := args.Get(0).(contractsapi.SignedValidatorSet)

		return &batch, args.Error(1)
	}

	return &contractsapi.SignedValidatorSet{}, nil
}

func (m *SystemStateMock) GetEpoch() (uint64, error) {
	args := m.Called()
	if len(args) == 1 {
		epochNumber, _ := args.Get(0).(uint64)

		return epochNumber, nil
	} else if len(args) == 2 {
		epochNumber, _ := args.Get(0).(uint64)

		err, ok := args.Get(1).(error)
		if ok {
			return epochNumber, err
		}

		return epochNumber, nil
	}

	return 0, nil
}

var _ contract.Provider = (*StateProviderMock)(nil)

type StateProviderMock struct {
	mock.Mock
}

func (s *StateProviderMock) Call(ethgo.Address, []byte, *contract.CallOpts) ([]byte, error) {
	return nil, nil
}

func (s *StateProviderMock) Txn(ethgo.Address, ethgo.Key, []byte) (contract.Txn, error) {
	return nil, nil
}
