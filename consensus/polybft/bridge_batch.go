package polybft

import (
	"bytes"
	"fmt"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
	merkle "github.com/Ethernal-Tech/merkle-tree"
)

// PendingBridgeBatch holds merkle trie of bridge transactions accompanied by epoch number
type PendingBridgeBatch struct {
	*contractsapi.BridgeMessageBatch
	Epoch uint64
}

// NewPendingBridgeBatch creates a new PendingBridgeBatch object
func NewPendingBridgeBatch(epoch uint64,
	bridgeEvents []*contractsapi.BridgeMsgEvent) (*PendingBridgeBatch, error) {
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

// Hash calculates hash value for PendingBridgeBatch object.
func (pbb *PendingBridgeBatch) Hash() (types.Hash, error) {
	data, err := pbb.BridgeMessageBatch.EncodeAbi()
	if err != nil {
		return types.Hash{}, err
	}

	return crypto.Keccak256Hash(data), nil
}

var _ contractsapi.StateTransactionInput = &BridgeBatchSigned{}

// BridgeBatchSigned encapsulates bridge batch with aggregated signatures
type BridgeBatchSigned struct {
	MessageBatch *contractsapi.BridgeMessageBatch
	AggSignature Signature
}

// Hash calculates hash value for BridgeBatchSigned object.
func (bbs *BridgeBatchSigned) Hash() (types.Hash, error) {
	data, err := bbs.MessageBatch.EncodeAbi()
	if err != nil {
		return types.Hash{}, err
	}

	return crypto.Keccak256Hash(data), nil
}

// ContainsStateSync checks if BridgeBatchSigned contains given state sync event
func (bbs *BridgeBatchSigned) ContainsBridgeMessage(bridgeMessageID uint64) bool {
	length := len(bbs.MessageBatch.Messages)
	if length == 0 {
		return false
	}

	return bbs.MessageBatch.Messages[0].ID.Uint64() <= bridgeMessageID &&
		bbs.MessageBatch.Messages[length-1].ID.Uint64() >= bridgeMessageID
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
		Batch:     bbs.MessageBatch,
		Signature: signature,
		Bitmap:    bbs.AggSignature.Bitmap,
	}

	return commit.EncodeAbi()
}

// DecodeAbi contains logic for decoding given ABI data
func (bbs *BridgeBatchSigned) DecodeAbi(txData []byte) error {
	if len(txData) < abiMethodIDLength {
		return fmt.Errorf("invalid commitment data, len = %d", len(txData))
	}

	commit := contractsapi.CommitBatchBridgeStorageFn{}

	err := commit.DecodeAbi(txData)
	if err != nil {
		return err
	}

	var signature []byte

	signature = append(signature, commit.Signature[0].Bytes()...)
	signature = append(signature, commit.Signature[1].Bytes()...)

	*bbs = BridgeBatchSigned{
		MessageBatch: commit.Batch,
		AggSignature: Signature{
			AggregatedSignature: signature,
			Bitmap:              commit.Bitmap,
		},
	}

	return nil
}

// getBridgeBatchSignedTx returns a BridgeBatchSigned object from a commit state transaction
func getBridgeBatchSignedTx(txs []*types.Transaction) (*BridgeBatchSigned, error) {
	var commitFn contractsapi.CommitBatchBridgeStorageFn
	for _, tx := range txs {
		// skip non state BridgeBatchSigned transactions
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

// createMerkleTree creates a merkle tree from provided bridge message events
// if only one state sync event is provided, a second, empty leaf will be added to merkle tree
// so that we can have a commitment with a single bridge message event
func createMerkleTree(bridgeMessageEvent []*contractsapi.BridgeMsgEvent) (*merkle.MerkleTree, error) {
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
