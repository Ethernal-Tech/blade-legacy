package polybft

import (
	"math/big"
	"testing"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/stretchr/testify/require"
)

func TestBridgeBatchSigned_Hash(t *testing.T) {
	t.Parallel()

	const (
		eventsCount = 10
	)

	bridgeBatchSigned1 := newTestBridgeBatchSigned(t, 1, 0)
	bridgeBatchSigned2 := newTestBridgeBatchSigned(t, 1, 0)
	bridgeBatchSigned3 := newTestBridgeBatchSigned(t, 2, 0)
	bridgeBatchSigned4 := newTestBridgeBatchSigned(t, 1, 3)

	hash1, err := bridgeBatchSigned1.Hash()
	require.NoError(t, err)
	hash2, err := bridgeBatchSigned2.Hash()
	require.NoError(t, err)
	hash3, err := bridgeBatchSigned3.Hash()
	require.NoError(t, err)
	hash4, err := bridgeBatchSigned4.Hash()
	require.NoError(t, err)

	require.Equal(t, hash1, hash2)
	require.NotEqual(t, hash1, hash3)
	require.NotEqual(t, hash1, hash4)
	require.NotEqual(t, hash3, hash4)
}

func TestBridgeBatch_ToRegisterBridgeBatchInputData(t *testing.T) {
	t.Parallel()

	const epoch, eventsCount = uint64(100), 11
	pendingBridgeBatch, _, _ := buildBridgeBatchAndBridgeEvents(t, eventsCount, epoch, uint64(2))
	blsKey1, err := bls.GenerateBlsKey()
	require.NoError(t, err)

	blsKey2, err := bls.GenerateBlsKey()
	require.NoError(t, err)

	data, err := pendingBridgeBatch.BridgeMessageBatch.EncodeAbi()
	require.NoError(t, err)

	signature1, err := blsKey1.Sign(data, domain)
	require.NoError(t, err)

	signature2, err := blsKey2.Sign(data, domain)
	require.NoError(t, err)

	signatures := bls.Signatures{signature1, signature2}

	aggSig, err := signatures.Aggregate().Marshal()
	require.NoError(t, err)

	expectedSignedBridgeBatchMsg := &BridgeBatchSigned{
		MessageBatch: pendingBridgeBatch.BridgeMessageBatch,
		AggSignature: Signature{
			Bitmap:              []byte{5, 1},
			AggregatedSignature: aggSig,
		},
		PublicKeys: [][]byte{blsKey1.PublicKey().Marshal(), blsKey2.PublicKey().Marshal()},
	}
	inputData, err := expectedSignedBridgeBatchMsg.EncodeAbi()
	require.NoError(t, err)
	require.NotEmpty(t, inputData)

	var actualSignedBridgeBatchMsg BridgeBatchSigned

	numberOfMessages := len(expectedSignedBridgeBatchMsg.MessageBatch.Messages)

	require.NoError(t, actualSignedBridgeBatchMsg.DecodeAbi(inputData))
	require.NoError(t, err)
	require.Equal(t, *expectedSignedBridgeBatchMsg.MessageBatch.Messages[0].ID, *actualSignedBridgeBatchMsg.MessageBatch.Messages[0].ID)
	require.Equal(t, *expectedSignedBridgeBatchMsg.MessageBatch.Messages[numberOfMessages-1].ID, *actualSignedBridgeBatchMsg.MessageBatch.Messages[numberOfMessages-1].ID)
	require.Equal(t, expectedSignedBridgeBatchMsg.AggSignature, actualSignedBridgeBatchMsg.AggSignature)
}

func newTestBridgeBatchSigned(t *testing.T, sourceChainId, destinationChainId uint64) *BridgeBatchSigned {
	t.Helper()

	return &BridgeBatchSigned{
		MessageBatch: &contractsapi.BridgeMessageBatch{
			SourceChainID:      new(big.Int).SetUint64(sourceChainId),
			DestinationChainID: new(big.Int).SetUint64(destinationChainId),
		},
		AggSignature: Signature{},
		PublicKeys:   [][]byte{},
	}
}

func buildBridgeBatchAndBridgeEvents(t *testing.T, bridgeMessageCount int,
	epoch, startIdx uint64) (*PendingBridgeBatch, *BridgeBatchSigned, []*contractsapi.BridgeMessageEventEvent) {
	t.Helper()

	bridgeMessageEvents := generateBridgeMessageEvents(t, bridgeMessageCount, startIdx)
	commitment, err := NewPendingBridgeBatch(epoch, bridgeMessageEvents)
	require.NoError(t, err)

	blsKey, err := bls.GenerateBlsKey()
	require.NoError(t, err)

	data, err := commitment.BridgeMessageBatch.EncodeAbi()
	require.NoError(t, err)

	signature, err := blsKey.Sign(data, domain)
	require.NoError(t, err)

	signatures := bls.Signatures{signature}

	aggSig, err := signatures.Aggregate().Marshal()
	require.NoError(t, err)

	commitmentSigned := &BridgeBatchSigned{
		MessageBatch: commitment.BridgeMessageBatch,
		AggSignature: Signature{
			AggregatedSignature: aggSig,
			Bitmap:              []byte{},
		},
		PublicKeys: [][]byte{blsKey.PublicKey().Marshal()},
	}

	return commitment, commitmentSigned, bridgeMessageEvents
}
