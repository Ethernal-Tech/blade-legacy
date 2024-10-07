package systemstate

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/contracts"
	"github.com/0xPolygon/polygon-edge/state"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo"
	"github.com/Ethernal-Tech/ethgo/contract"
)

var (
	errSendTxnUnsupported = errors.New("system state does not support send transactions")
)

type ChainType int

const (
	Internal ChainType = iota // Internal = 0
	External                  // External = 1
)

// SystemState is an interface to interact with the consensus system contracts in the chain
type SystemState interface {
	// GetEpoch retrieves current epoch number from the smart contract
	GetEpoch() (uint64, error)
	// GetNextCommittedIndex retrieves next committed bridge message index, based on the chain type
	GetNextCommittedIndex(chainID uint64, chainType ChainType) (uint64, error)
	// GetBridgeBatchByNumber return bridge batch by number
	GetBridgeBatchByNumber(numberOfBatch *big.Int) (*contractsapi.SignedBridgeMessageBatch, error)
	// GetValidatorSetByNumber return validator set by number
	GetValidatorSetByNumber(numberOfValidatorSet *big.Int) (*contractsapi.SignedValidatorSet, error)
}

var _ SystemState = &SystemStateImpl{}

// SystemStateImpl is implementation of SystemState interface
type SystemStateImpl struct {
	validatorContract     *contract.Contract
	bridgeStorageContract *contract.Contract
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
	s.bridgeStorageContract = contract.NewContract(
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
func (s *SystemStateImpl) GetNextCommittedIndex(chainID uint64, chainType ChainType) (uint64, error) {
	var funcName string

	switch chainType {
	case Internal:
		funcName = "lastCommittedInternal"
	case External:
		funcName = "lastCommitted"
	default:
		return 0, fmt.Errorf("unsupported chain type: %d", chainType)
	}

	rawResult, err := s.bridgeStorageContract.Call(funcName, ethgo.Latest, new(big.Int).SetUint64(chainID))
	if err != nil {
		return 0, err
	}

	nextCommittedIndex, isOk := rawResult["0"].(*big.Int)
	if !isOk {
		return 0, fmt.Errorf("failed to decode next committed index")
	}

	return nextCommittedIndex.Uint64() + 1, nil
}

func (s *SystemStateImpl) GetBridgeBatchByNumber(numberOfBatch *big.Int) (
	*contractsapi.SignedBridgeMessageBatch, error) {
	rawResult, err := s.bridgeStorageContract.Call(
		"getCommittedBatch",
		ethgo.Latest,
		numberOfBatch)
	if err != nil {
		return nil, err
	}
	rawResult = rawResult["0"].(map[string]interface{})

	sbmb := &contractsapi.SignedBridgeMessageBatch{}
	signatures := rawResult["signature"].([2]*big.Int)
	sbmb.Signature = signatures
	bitmap := rawResult["bitmap"].([]uint8)
	sbmb.Bitmap = bitmap
	batch := rawResult["batch"].(map[string]interface{})
	messages := batch["messages"].([]map[string]interface{})
	bridgeMessages := []*contractsapi.BridgeMessage{}
	for _, message := range messages {
		bridgeMessages = append(bridgeMessages, &contractsapi.BridgeMessage{
			ID:                 message["id"].(*big.Int),
			SourceChainID:      message["sourceChainId"].(*big.Int),
			DestinationChainID: message["destinationChainId"].(*big.Int),
			Sender:             types.Address(message["sender"].(ethgo.Address)),
			Receiver:           types.Address(message["receiver"].(ethgo.Address)),
			Payload:            message["payload"].([]byte),
		})
	}
	bmb := &contractsapi.BridgeMessageBatch{}
	bmb.Messages = bridgeMessages
	bmb.SourceChainID = batch["sourceChainId"].(*big.Int)
	bmb.DestinationChainID = batch["destinationChainId"].(*big.Int)
	sbmb.Batch = bmb

	return sbmb, nil
}

func (s *SystemStateImpl) GetValidatorSetByNumber(numberOfValidatorSet *big.Int) (
	*contractsapi.SignedValidatorSet, error) {
	rawResult, err := s.bridgeStorageContract.Call(
		"getCommittedValidatorSet",
		ethgo.Latest,
		numberOfValidatorSet,
	)
	if err != nil {
		return nil, err
	}
	rawResult = rawResult["0"].(map[string]interface{})

	svs := &contractsapi.SignedValidatorSet{}
	svs.Signature = rawResult["signature"].([2]*big.Int)
	svs.Bitmap = rawResult["bitmap"].([]uint8)
	validatorSet := []*contractsapi.Validator{}
	for _, validator := range rawResult["newValidatorSet"].([]map[string]interface{}) {
		validatorSet = append(validatorSet, &contractsapi.Validator{
			Address:     types.Address(validator["_address"].(ethgo.Address)),
			BlsKey:      validator["blsKey"].([4]*big.Int),
			VotingPower: validator["votingPower"].(*big.Int),
		})
	}
	svs.NewValidatorSet = validatorSet

	return svs, nil
}

var _ contract.Provider = &stateProvider{}

type stateProvider struct {
	transition *state.Transition
}

// NewStateProvider initializes EVM against given state and chain config and returns stateProvider instance
// which is an abstraction for smart contract calls
func NewStateProvider(transition *state.Transition) contract.Provider {
	return &stateProvider{transition: transition}
}

// Call implements the contract.Provider interface to make contract calls directly to the state
func (s *stateProvider) Call(addr ethgo.Address, input []byte, opts *contract.CallOpts) ([]byte, error) {
	result := s.transition.Call2(
		contracts.SystemCaller,
		types.Address(addr),
		input,
		big.NewInt(0),
		10000000,
	)
	if result.Failed() {
		return nil, result.Err
	}

	return result.ReturnValue, nil
}

// Txn is part of the contract.Provider interface to make Ethereum transactions. We disable this function
// since the system state does not make any transaction
func (s *stateProvider) Txn(_ ethgo.Address, _ ethgo.Key, _ []byte) (contract.Txn, error) {
	return nil, errSendTxnUnsupported
}
