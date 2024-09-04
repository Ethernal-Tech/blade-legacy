package contractsapi

import (
	"github.com/Ethernal-Tech/ethgo"
	"github.com/Ethernal-Tech/ethgo/abi"
)

// StateTransactionInput is an abstraction for different state transaction inputs
type StateTransactionInput interface {
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

var (
	// stateSyncABIType is a specific case where we need to encode state sync event as a tuple of tuple
	stateSyncABIType = abi.MustNewType(
		"tuple(tuple(uint256 id, address sender, address receiver, bytes data))")

	// GetCheckpointBlockABIResponse is the ABI type for getCheckpointBlock function return value
	GetCheckpointBlockABIResponse = abi.MustNewType("tuple(bool isFound, uint256 checkpointBlock)")
)

var (
	_ StateTransactionInput = &CommitEpochEpochManagerFn{}
	_ StateTransactionInput = &DistributeRewardForEpochManagerFn{}
)
