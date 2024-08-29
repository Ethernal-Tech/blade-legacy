package polybft

import (
	"bytes"
	"fmt"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/state/runtime/precompiled"
	"github.com/0xPolygon/polygon-edge/types"
	merkle "github.com/Ethernal-Tech/merkle-tree"
)

// PendingBridgeBatch holds merkle trie of bridge transactions accompanied by epoch number
type PendingBridgeBatch struct {
	*contractsapi.BridgeMessageBatch
	Epoch uint64
}

// NewPendingBridgeBatch creates a new commitment object
func NewPendingBridgeBatch(epoch uint64,
	bridgeEvents []*contractsapi.BridgeMessageEventEvent) (*PendingBridgeBatch, error) {

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

	return &PendingBridgeBatch{
		BridgeMessageBatch: &contractsapi.BridgeMessageBatch{
			Messages:           messages,
			DestinationChainID: bridgeEvents[0].DestinationChainID,
			SourceChainID:      bridgeEvents[0].SourceChainID},
		Epoch: epoch,
	}, nil
}

// Hash calculates hash value for commitment object.
func (cm *PendingBridgeBatch) Hash() (types.Hash, error) {
	data, err := cm.BridgeMessageBatch.EncodeAbi()
	if err != nil {
		return types.Hash{}, err
	}

	return crypto.Keccak256Hash(data), nil
}

var _ contractsapi.StateTransactionInput = &BridgeBatchSigned{}

// BridgeBatchSigned encapsulates commitment message with aggregated signatures
type BridgeBatchSigned struct {
	MessageBatch *contractsapi.BridgeMessageBatch
	AggSignature Signature
	PublicKeys   [][]byte
}

// Hash calculates hash value for commitment object.
func (cm *BridgeBatchSigned) Hash() (types.Hash, error) {
	data, err := cm.MessageBatch.EncodeAbi()
	if err != nil {
		return types.Hash{}, err
	}

	return crypto.Keccak256Hash(data), nil
}

// ContainsStateSync checks if commitment contains given state sync event
func (cm *BridgeBatchSigned) ContainsBridgeMessage(bridgeMessageID uint64) bool {
	length := len(cm.MessageBatch.Messages)
	if length == 0 {
		return false
	}
	return cm.MessageBatch.Messages[0].ID.Uint64() <= bridgeMessageID &&
		cm.MessageBatch.Messages[length-1].ID.Uint64() >= bridgeMessageID
}

// EncodeAbi contains logic for encoding arbitrary data into ABI format
func (cm *BridgeBatchSigned) EncodeAbi() ([]byte, error) {
	blsVerificationPart, err := precompiled.BlsVerificationABIType.Encode(
		[2]interface{}{cm.PublicKeys, cm.AggSignature.Bitmap})
	if err != nil {
		return nil, err
	}

	blsSignatrure, err := bls.UnmarshalSignature(cm.AggSignature.AggregatedSignature)
	if err != nil {
		return nil, err
	}

	signature, err := blsSignatrure.ToBigInt()
	if err != nil {
		return nil, err
	}

	commit := &contractsapi.CommitBatchBridgeStorageFn{
		Batch:     cm.MessageBatch,
		Signature: signature,
		Bitmap:    blsVerificationPart,
	}

	return commit.EncodeAbi()
}

// DecodeAbi contains logic for decoding given ABI data
func (cm *BridgeBatchSigned) DecodeAbi(txData []byte) error {
	if len(txData) < abiMethodIDLength {
		return fmt.Errorf("invalid commitment data, len = %d", len(txData))
	}

	commit := contractsapi.CommitBatchBridgeStorageFn{}

	err := commit.DecodeAbi(txData)
	if err != nil {
		return err
	}

	decoded, err := precompiled.BlsVerificationABIType.Decode(commit.Bitmap)
	if err != nil {
		return err
	}

	blsMap, isOk := decoded.(map[string]interface{})
	if !isOk {
		return fmt.Errorf("invalid commitment data. Bls verification part not in correct format")
	}

	publicKeys, isOk := blsMap["0"].([][]byte)
	if !isOk {
		return fmt.Errorf("invalid commitment data. Could not find public keys part")
	}

	bitmap, isOk := blsMap["1"].([]byte)
	if !isOk {
		return fmt.Errorf("invalid commitment data. Could not find bitmap part")
	}

	var signature []byte

	signature = append(signature, commit.Signature[0].Bytes()...)
	signature = append(signature, commit.Signature[1].Bytes()...)

	*cm = BridgeBatchSigned{
		MessageBatch: commit.Batch,
		AggSignature: Signature{
			AggregatedSignature: signature,
			Bitmap:              bitmap,
		},
		PublicKeys: publicKeys,
	}

	return nil
}

// getBridgeBatchSignedTx returns a CommitmentMessageSigned object from a commit state transaction
func getBridgeBatchSignedTx(txs []*types.Transaction) (*BridgeBatchSigned, error) {
	var commitFn contractsapi.CommitBatchBridgeStorageFn
	for _, tx := range txs {
		// skip non state CommitmentMessageSigned transactions
		if tx.Type() != types.StateTxType ||
			len(tx.Input()) < abiMethodIDLength ||
			!bytes.Equal(tx.Input()[:abiMethodIDLength], commitFn.Sig()) {
			continue
		}

		obj := &BridgeBatchSigned{}

		if err := obj.DecodeAbi(tx.Input()); err != nil {
			return nil, fmt.Errorf("get commitment message signed tx error: %w", err)
		}

		return obj, nil
	}

	return nil, nil
}

// createMerkleTree creates a merkle tree from provided state sync events
// if only one state sync event is provided, a second, empty leaf will be added to merkle tree
// so that we can have a commitment with a single state sync event
func createMerkleTree(bridgeMessageEvent []*contractsapi.BridgeMessageEventEvent) (*merkle.MerkleTree, error) {
	bridgeMessageData := make([][]byte, len(bridgeMessageEvent))

	for i, event := range bridgeMessageEvent {
		data, err := event.Encode()
		if err != nil {
			return nil, err
		}

		bridgeMessageData[i] = data
	}

	return merkle.NewMerkleTree(bridgeMessageData)
}
