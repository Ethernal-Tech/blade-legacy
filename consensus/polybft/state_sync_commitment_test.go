package polybft

import (
	"testing"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/stretchr/testify/require"
)

func TestCommitmentMessage_Hash(t *testing.T) {
	t.Parallel()

	const (
		eventsCount = 10
	)

	commitmentMessage1 := newTestCommitmentSigned(t)
	commitmentMessage2 := newTestCommitmentSigned(t)
	commitmentMessage3 := newTestCommitmentSigned(t)
	commitmentMessage4 := newTestCommitmentSigned(t)

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
	expectedSignedCommitmentMsg := &CommitmentMessageSigned{
		MessageBatch: pendingCommitment.BridgeMessageBatch,
		AggSignature: Signature{
			Bitmap:              []byte{5, 1},
			AggregatedSignature: []byte{1, 1},
		},
		PublicKeys: [][]byte{{0, 1}, {2, 3}, {4, 5}},
	}
	inputData, err := expectedSignedCommitmentMsg.EncodeAbi()
	require.NoError(t, err)
	require.NotEmpty(t, inputData)

	var actualSignedCommitmentMsg CommitmentMessageSigned

	require.NoError(t, actualSignedCommitmentMsg.DecodeAbi(inputData))
	require.NoError(t, err)
	require.Equal(t, *expectedSignedCommitmentMsg.MessageBatch, *actualSignedCommitmentMsg.MessageBatch)
	require.Equal(t, expectedSignedCommitmentMsg.AggSignature, actualSignedCommitmentMsg.AggSignature)
}

func newTestCommitmentSigned(t *testing.T) *CommitmentMessageSigned {
	t.Helper()

	return &CommitmentMessageSigned{
		MessageBatch: &contractsapi.BridgeMessageBatch{},
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

	commitmentSigned := &CommitmentMessageSigned{
		MessageBatch: commitment.BridgeMessageBatch,
		AggSignature: Signature{
			AggregatedSignature: []byte{},
			Bitmap:              []byte{},
		},
		PublicKeys: [][]byte{},
	}

	return commitment, commitmentSigned, bridgeMessageEvents
}
