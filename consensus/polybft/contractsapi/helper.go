package contractsapi

import (
	"math/big"

	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo"
	"github.com/Ethernal-Tech/ethgo/abi"
)

// ABIEncoder declares functions that are encoding and decoding data to/from ABI format
type ABIEncoder interface {
	// EncodeAbi contains logic for encoding arbitrary data into ABI format
	EncodeAbi() ([]byte, error)
	// DecodeAbi contains logic for decoding given ABI data
	DecodeAbi(b []byte) error
}

// EventAbi is an interface representing an event generated in contractsapi
type EventAbi interface {
	// Sig returns the event ABI signature or ID (which is unique for all event types)
	Sig() ethgo.Hash
	// Encode does abi encoding of given event
	Encode() ([]byte, error)
	// ParseLog parses the provided receipt log to given event type
	ParseLog(log *ethgo.Log) (bool, error)
}

// FunctionAbi is an interface representing a function in contractsapi
type FunctionAbi interface {
	ABIEncoder
	// Sig returns the function ABI signature or ID (which is unique for all function types)
	Sig() []byte
}

var (
	// GetCheckpointBlockABIResponse is the ABI type for getCheckpointBlock function return value
	GetCheckpointBlockABIResponse = abi.MustNewType("tuple(bool isFound, uint256 checkpointBlock)")

	SignedBridgeMessageBatchABIType = abi.MustNewType(
		"tuple(tuple(tuple(" +
			"uint256 id,uint256 sourceChainId,uint256 destinationChainId,address sender,address receiver,bytes payload" +
			")[] messages,uint256 sourceChainId,uint256 destinationChainId) batch,uint256[2] signature,bytes bitmap)")
	SignedValidatorABIType = abi.MustNewType(
		"tuple(tuple(address _address,uint256[4] blsKey,uint256 votingPower)[] newValidatorSet," +
			"uint256[2] signature, bytes bitmap)")
)

var (
	_ ABIEncoder = &CommitEpochEpochManagerFn{}
	_ ABIEncoder = &DistributeRewardForEpochManagerFn{}
)

type SignedBridgeMessageBatch struct {
	Batch     *BridgeMessageBatch `abi:"batch"`
	Signature [2]*big.Int         `abi:"signature"`
	Bitmap    []byte              `abi:"bitmap"`
}

func (s *SignedBridgeMessageBatch) EncodeAbi() ([]byte, error) {
	return SignedBridgeMessageBatchABIType.Encode(s)
}

func (s *SignedBridgeMessageBatch) DecodeAbi(buf []byte) error {
	return decodeStruct(SignedBridgeMessageBatchABIType, buf, &s)
}

type SignedValidatorSet struct {
	NewValidatorSet []*Validator `abi:"newValidatorSet"`
	Signature       [2]*big.Int  `abi:"signature"`
	Bitmap          []byte       `abi:"bitmap"`
}

func (s *SignedValidatorSet) EncodeAbi() ([]byte, error) {
	return SignedValidatorABIType.Encode(s)
}

func (s *SignedValidatorSet) DecodeAbi(buf []byte) error {
	return decodeStruct(SignedValidatorABIType, buf, &s)
}

// GetValidatorsAsMap returns the new validator set as a map of address to validator
func (c *CommitValidatorSetBridgeStorageFn) GetValidatorsAsMap() map[types.Address]*Validator {
	validatorMap := make(map[types.Address]*Validator, len(c.NewValidatorSet))

	for _, v := range c.NewValidatorSet {
		validatorMap[v.Address] = v
	}

	return validatorMap
}
