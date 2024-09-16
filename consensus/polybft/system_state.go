package polybft

import (
	"fmt"
	"math/big"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo"
	"github.com/Ethernal-Tech/ethgo/contract"
)

// ValidatorInfo is data transfer object which holds validator information,
// provided by smart contract
type ValidatorInfo struct {
	Stake               *big.Int      `json:"stake"`
	WithdrawableRewards *big.Int      `json:"withdrawableRewards"`
	Address             types.Address `json:"address"`
	IsActive            bool          `json:"isActive"`
	IsWhitelisted       bool          `json:"isWhitelisted"`
}

// SystemState is an interface to interact with the consensus system contracts in the chain
type SystemState interface {
	// GetEpoch retrieves current epoch number from the smart contract
	GetEpoch() (uint64, error)
	// GetNextCommittedIndexExternal retrieves next committed external bridge message index
	GetNextCommittedIndexExternal(sourceChainID uint64) (uint64, error)
	// GetNextCommittedIndexInternal retrieves next committed internal bridge message index
	GetNextCommittedIndexInternal(sourceChainID uint64) (uint64, error)
}

var _ SystemState = &SystemStateImpl{}

// SystemStateImpl is implementation of SystemState interface
type SystemStateImpl struct {
	validatorContract       *contract.Contract
	sidechainBridgeContract *contract.Contract
}

// NewSystemState initializes new instance of systemState which abstracts smart contracts functions
func NewSystemState(
	valSetAddr types.Address,
	bridgeStorageAddr types.Address,
	provider contract.Provider) *SystemStateImpl {
	s := &SystemStateImpl{}
	s.validatorContract = contract.NewContract(
		ethgo.Address(valSetAddr),
		contractsapi.EpochManager.Abi, contract.WithProvider(provider),
	)
	s.sidechainBridgeContract = contract.NewContract(
		ethgo.Address(bridgeStorageAddr),
		contractsapi.BridgeStorage.Abi,
		contract.WithProvider(provider),
	)

	return s
}

// GetEpoch retrieves current epoch number from the smart contract
func (s *SystemStateImpl) GetEpoch() (uint64, error) {
	rawResult, err := s.validatorContract.Call("currentEpochId", ethgo.Latest)
	if err != nil {
		return 0, err
	}

	epochNumber, isOk := rawResult["0"].(*big.Int)
	if !isOk {
		return 0, fmt.Errorf("failed to decode epoch")
	}

	return epochNumber.Uint64(), nil
}

// GetNextCommittedIndexExternal retrieves next committed external bridge message index
func (s *SystemStateImpl) GetNextCommittedIndexExternal(sourceChainID uint64) (uint64, error) {
	rawResult, err := s.sidechainBridgeContract.Call(
		"lastCommitted",
		ethgo.Latest,
		new(big.Int).SetUint64(sourceChainID))
	if err != nil {
		return 0, err
	}

	nextCommittedIndex, isOk := rawResult["0"].(*big.Int)
	if !isOk {
		return 0, fmt.Errorf("failed to decode next committed index")
	}

	return nextCommittedIndex.Uint64() + 1, nil
}

// GetNextCommittedIndexInternal retrieves next committed internal bridge message index
func (s *SystemStateImpl) GetNextCommittedIndexInternal(destinationChainID uint64) (uint64, error) {
	rawResult, err := s.sidechainBridgeContract.Call(
		"lastCommittedInternal",
		ethgo.Latest,
		new(big.Int).SetUint64(destinationChainID))
	if err != nil {
		return 0, err
	}

	nextCommittedIndex, isOk := rawResult["0"].(*big.Int)
	if !isOk {
		return 0, fmt.Errorf("failed to decode next committed Index")
	}

	return nextCommittedIndex.Uint64() + 1, nil
}
