package bridge

import (
	"fmt"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/helpers"
	polytypes "github.com/0xPolygon/polygon-edge/consensus/polybft/types"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
	merkle "github.com/Ethernal-Tech/merkle-tree"
)

// PendingBridgeBatch holds pending bridge batch for epoch
type PendingBridgeBatch struct {
	BridgeBatch *contractsapi.BridgeBatch
	Epoch       uint64
}

// NewPendingBridgeBatch creates a new PendingBridgeBatch object
func NewPendingBridgeBatch(epoch uint64,
	bridgeEvents []*contractsapi.BridgeMsgEvent) (*PendingBridgeBatch, error) {
	if len(bridgeEvents) == 0 {
		return nil, nil
	}

	messages := make([]*contractsapi.BridgeMessage, len(bridgeEvents))

	for i, bridgeEvent := range bridgeEvents {
		messages[i] = &contractsapi.BridgeMessage{
			ID:                 bridgeEvent.ID,
			Sender:             bridgeEvent.Sender,
			Receiver:           bridgeEvent.Receiver,
			Payload:            bridgeEvent.Data,
			SourceChainID:      bridgeEvent.SourceChainID,
			DestinationChainID: bridgeEvent.DestinationChainID}
	}

	tree, err := createMerkleTree(messages)
	if err != nil {
		return nil, err
	}

	return &PendingBridgeBatch{
		BridgeBatch: &contractsapi.BridgeBatch{
			RootHash:           types.Hash(tree.Hash()),
			StartID:            messages[0].ID,
			EndID:              messages[len(messages)-1].ID,
			SourceChainID:      messages[0].SourceChainID,
			DestinationChainID: messages[0].SourceChainID,
		},
		Epoch: epoch,
	}, nil
}

// Hash calculates hash value for PendingBridgeBatch object.
func (pbb *PendingBridgeBatch) Hash() (types.Hash, error) {
	data, err := pbb.BridgeBatch.EncodeAbi()
	if err != nil {
		return types.ZeroHash, err
	}

	return crypto.Keccak256Hash(data), nil
}

var _ contractsapi.StateTransactionInput = &BridgeBatchSigned{}

// BridgeBatchSigned encapsulates bridge batch with aggregated signatures
type BridgeBatchSigned struct {
	BridgeBatch  *contractsapi.BridgeBatch
	AggSignature polytypes.Signature
}

// Hash calculates hash value for BridgeBatchSigned object.
func (bbs *BridgeBatchSigned) Hash() (types.Hash, error) {
	data, err := bbs.BridgeBatch.EncodeAbi()
	if err != nil {
		return types.ZeroHash, err
	}

	return crypto.Keccak256Hash(data), nil
}

// ContainsBridgeMessage checks if BridgeBatchSigned contains given bridge message event
func (bbs *BridgeBatchSigned) ContainsBridgeMessage(bridgeMessageID uint64) bool {
	return bbs.BridgeBatch.StartID.Uint64() <= bridgeMessageID &&
		bbs.BridgeBatch.EndID.Uint64() >= bridgeMessageID
}

// EncodeAbi contains logic for encoding arbitrary data into ABI format
func (bbs *BridgeBatchSigned) EncodeAbi() ([]byte, error) {
	blsSignatrure, err := bls.UnmarshalSignature(bbs.AggSignature.AggregatedSignature)
	if err != nil {
		return nil, err
	}

	signature, err := blsSignatrure.ToBigInt()
	if err != nil {
		return nil, err
	}

	commit := &contractsapi.CommitBatchBridgeStorageFn{
		Batch: &contractsapi.SignedBridgeMessageBatch{
			RootHash:           bbs.BridgeBatch.RootHash,
			StartID:            bbs.BridgeBatch.StartID,
			EndID:              bbs.BridgeBatch.EndID,
			SourceChainID:      bbs.BridgeBatch.SourceChainID,
			DestinationChainID: bbs.BridgeBatch.DestinationChainID,
			Signature:          signature,
			Bitmap:             bbs.AggSignature.Bitmap,
		},
	}

	return commit.EncodeAbi()
}

// DecodeAbi contains logic for decoding given ABI data
func (bbs *BridgeBatchSigned) DecodeAbi(txData []byte) error {
	if len(txData) < helpers.AbiMethodIDLength {
		return fmt.Errorf("invalid batch data, len = %d", len(txData))
	}

	commit := contractsapi.CommitBatchBridgeStorageFn{}

	err := commit.DecodeAbi(txData)
	if err != nil {
		return err
	}

	signature0 := commit.Batch.Signature[0].Bytes()
	signature1 := commit.Batch.Signature[1].Bytes()
	halfSignatureSize := bls.SignatureSize / 2
	signature := make([]byte, bls.SignatureSize)

	if len(signature0) < halfSignatureSize {
		copy(signature[halfSignatureSize-len(signature0):halfSignatureSize], signature0)
	} else {
		copy(signature[:halfSignatureSize], signature0)
	}

	if len(signature1) < halfSignatureSize {
		copy(signature[bls.SignatureSize-len(signature1):], signature1)
	} else {
		copy(signature[halfSignatureSize:], signature1)
	}

	*bbs = BridgeBatchSigned{
		BridgeBatch: &contractsapi.BridgeBatch{
			RootHash:           commit.Batch.RootHash,
			StartID:            commit.Batch.StartID,
			EndID:              commit.Batch.EndID,
			SourceChainID:      commit.Batch.SourceChainID,
			DestinationChainID: commit.Batch.DestinationChainID,
		},
		AggSignature: polytypes.Signature{
			AggregatedSignature: signature,
			Bitmap:              commit.Batch.Bitmap,
		},
	}

	return nil
}

// createMerkleTree creates a merkle tree from provided state sync events
// if only one state sync event is provided, a second, empty leaf will be added to merkle tree
// so that we can have a commitment with a single state sync event
func createMerkleTree(bridgeMessages []*contractsapi.BridgeMessage) (*merkle.MerkleTree, error) {
	stateSyncData := make([][]byte, len(bridgeMessages))

	for i, bm := range bridgeMessages {
		data, err := bm.EncodeAbi()
		if err != nil {
			return nil, err
		}

		stateSyncData[i] = data
	}

	return merkle.NewMerkleTree(stateSyncData)
}
