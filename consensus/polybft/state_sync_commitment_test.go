package polybft

import (
	"math/big"
	"testing"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/stretchr/testify/require"
)

func TestCommitmentMessage_Hash(t *testing.T) {
	t.Parallel()

	const (
		eventsCount = 10
	)

	commitmentMessage1 := newTestCommitmentSigned(t, 1, 0)
	commitmentMessage2 := newTestCommitmentSigned(t, 1, 0)
	commitmentMessage3 := newTestCommitmentSigned(t, 2, 0)
	commitmentMessage4 := newTestCommitmentSigned(t, 1, 3)

	hash1, err := commitmentMessage1.Hash()
	require.NoError(t, err)
	hash2, err := commitmentMessage2.Hash()
	require.NoError(t, err)
	hash3, err := commitmentMessage3.Hash()
	require.NoError(t, err)
	hash4, err := commitmentMessage4.Hash()
	require.NoError(t, err)

	require.Equal(t, hash1, hash2)
	require.NotEqual(t, hash1, hash3)
	require.NotEqual(t, hash1, hash4)
	require.NotEqual(t, hash3, hash4)
}

func TestCommitmentMessage_ToRegisterCommitmentInputData(t *testing.T) {
	t.Parallel()

	const epoch, eventsCount = uint64(100), 11
	pendingCommitment, _, _ := buildCommitmentAndStateSyncs(t, eventsCount, epoch, uint64(2))
	blsKey1, err := bls.GenerateBlsKey()
	require.NoError(t, err)

	blsKey2, err := bls.GenerateBlsKey()
	require.NoError(t, err)

	data, err := pendingCommitment.BridgeMessageBatch.EncodeAbi()
	require.NoError(t, err)

	signature1, err := blsKey1.Sign(data, domain)
	require.NoError(t, err)

	signature2, err := blsKey2.Sign(data, domain)
	require.NoError(t, err)

	signatures := bls.Signatures{signature1, signature2}

	aggSig, err := signatures.Aggregate().Marshal()
	require.NoError(t, err)

	expectedSignedCommitmentMsg := &CommitmentMessageSigned{
		MessageBatch: pendingCommitment.BridgeMessageBatch,
		AggSignature: Signature{
			Bitmap:              []byte{5, 1},
			AggregatedSignature: aggSig,
		},
		PublicKeys: [][]byte{blsKey1.PublicKey().Marshal(), blsKey2.PublicKey().Marshal()},
	}
	inputData, err := expectedSignedCommitmentMsg.EncodeAbi()
	require.NoError(t, err)
	require.NotEmpty(t, inputData)

	var actualSignedCommitmentMsg CommitmentMessageSigned

	numberOfMessages := len(expectedSignedCommitmentMsg.MessageBatch.Messages)

	require.NoError(t, actualSignedCommitmentMsg.DecodeAbi(inputData))
	require.NoError(t, err)
	require.Equal(t, *expectedSignedCommitmentMsg.MessageBatch.Messages[0].ID, *actualSignedCommitmentMsg.MessageBatch.Messages[0].ID)
	require.Equal(t, *expectedSignedCommitmentMsg.MessageBatch.Messages[numberOfMessages-1].ID, *actualSignedCommitmentMsg.MessageBatch.Messages[numberOfMessages-1].ID)
	require.Equal(t, expectedSignedCommitmentMsg.AggSignature, actualSignedCommitmentMsg.AggSignature)
}

func newTestCommitmentSigned(t *testing.T, sourceChainId, destinationChainId uint64) *CommitmentMessageSigned {
	t.Helper()

	return &CommitmentMessageSigned{
		MessageBatch: &contractsapi.BridgeMessageBatch{
			SourceChainID:      new(big.Int).SetUint64(sourceChainId),
			DestinationChainID: new(big.Int).SetUint64(destinationChainId),
		},
		AggSignature: Signature{},
		PublicKeys:   [][]byte{},
	}
}

func buildCommitmentAndStateSyncs(t *testing.T, bridgeMessageCount int,
	epoch, startIdx uint64) (*PendingCommitment, *CommitmentMessageSigned, []*contractsapi.BridgeMessageEventEvent) {
	t.Helper()

	bridgeMessageEvents := generateBridgeMessageEvents(t, bridgeMessageCount, startIdx)
	commitment, err := NewPendingCommitment(epoch, bridgeMessageEvents)
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

	commitmentSigned := &CommitmentMessageSigned{
		MessageBatch: commitment.BridgeMessageBatch,
		AggSignature: Signature{
			AggregatedSignature: aggSig,
			Bitmap:              []byte{},
		},
		PublicKeys: [][]byte{blsKey.PublicKey().Marshal()},
	}

	return commitment, commitmentSigned, bridgeMessageEvents
}
