package polybft

import (
	"bytes"
	"fmt"
	"math/big"
	"testing"

	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"
)

func TestState_InsertEvent(t *testing.T) {
	t.Parallel()

	state := newTestState(t)
	event1 := &contractsapi.BridgeMessageEventEvent{
		ID:       big.NewInt(0),
		Sender:   types.Address{},
		Receiver: types.Address{},
		Data:     []byte{},
	}

	err := state.BridgeMessageStore.insertBridgeMessageEvent(event1)
	assert.NoError(t, err)

	events, err := state.BridgeMessageStore.list()
	assert.NoError(t, err)
	assert.Len(t, events, 1)
}

func TestState_Insert_And_Get_MessageVotes(t *testing.T) {
	t.Parallel()

	state := newTestState(t)
	epoch := uint64(1)
	assert.NoError(t, state.EpochStore.insertEpoch(epoch, nil, 0))

	hash := []byte{1, 2}
	_, err := state.BridgeMessageStore.insertMessageVote(1, hash, &MessageSignature{
		From:      "NODE_1",
		Signature: []byte{1, 2},
	}, nil, 0)

	assert.NoError(t, err)

	votes, err := state.BridgeMessageStore.getMessageVotes(epoch, hash, 0)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(votes))
	assert.Equal(t, "NODE_1", votes[0].From)
	assert.True(t, bytes.Equal([]byte{1, 2}, votes[0].Signature))
}

func TestState_getStateSyncEventsForCommitment_NotEnoughEvents(t *testing.T) {
	t.Parallel()

	state := newTestState(t)

	for i := 0; i < maxCommitmentSize-2; i++ {
		assert.NoError(t, state.BridgeMessageStore.insertBridgeMessageEvent(&contractsapi.BridgeMessageEventEvent{
			ID:                 big.NewInt(int64(i)),
			Data:               []byte{1, 2},
			SourceChainID:      big.NewInt(1),
			DestinationChainID: bigZero,
		}))
	}

	_, err := state.BridgeMessageStore.getBridgeMessageEventsForCommitment(0, maxCommitmentSize-1, nil, 0)
	assert.ErrorIs(t, err, errNotEnoughStateSyncs)
}

func TestState_getStateSyncEventsForCommitment(t *testing.T) {
	t.Parallel()

	state := newTestState(t)

	for i := 0; i < maxCommitmentSize; i++ {
		assert.NoError(t, state.BridgeMessageStore.insertBridgeMessageEvent(&contractsapi.BridgeMessageEventEvent{
			ID:                 big.NewInt(int64(i)),
			Data:               []byte{1, 2},
			SourceChainID:      big.NewInt(1),
			DestinationChainID: bigZero,
		}))
	}

	t.Run("Return all - forced. Enough events", func(t *testing.T) {
		t.Parallel()

		events, err := state.BridgeMessageStore.getBridgeMessageEventsForCommitment(0, maxCommitmentSize-1, nil, 0)
		require.NoError(t, err)
		require.Equal(t, maxCommitmentSize, len(events))
	})

	t.Run("Return all - forced. Not enough events", func(t *testing.T) {
		t.Parallel()

		_, err := state.BridgeMessageStore.getBridgeMessageEventsForCommitment(0, maxCommitmentSize+1, nil, 0)
		require.ErrorIs(t, err, errNotEnoughStateSyncs)
	})

	t.Run("Return all you can. Enough events", func(t *testing.T) {
		t.Parallel()

		events, err := state.BridgeMessageStore.getBridgeMessageEventsForCommitment(0, maxCommitmentSize-1, nil, 0)
		assert.NoError(t, err)
		assert.Equal(t, maxCommitmentSize, len(events))
	})

	t.Run("Return all you can. Not enough events", func(t *testing.T) {
		t.Parallel()

		events, err := state.BridgeMessageStore.getBridgeMessageEventsForCommitment(0, maxCommitmentSize+1, nil, 0)
		assert.ErrorIs(t, err, errNotEnoughStateSyncs)
		assert.Equal(t, maxCommitmentSize, len(events))
	})
}

func TestState_insertCommitmentMessage(t *testing.T) {
	t.Parallel()

	commitment := createTestCommitmentMessage(t)

	state := newTestState(t)
	assert.NoError(t, state.BridgeMessageStore.insertCommitmentMessage(commitment, nil))

	length := len(commitment.MessageBatch.Messages)

	commitmentFromDB, err := state.BridgeMessageStore.getCommitmentMessage(commitment.MessageBatch.Messages[length-1].ID.Uint64())

	assert.NoError(t, err)
	assert.NotNil(t, commitmentFromDB)
	assert.Equal(t, commitment, commitmentFromDB)
}

func TestState_getCommitmentForStateSync(t *testing.T) {
	const (
		numOfCommitments = 10
	)

	state := newTestState(t)

	insertTestCommitments(t, state, numOfCommitments)

	var cases = []struct {
		stateSyncID   uint64
		hasCommitment bool
	}{
		{1, true},
		{10, true},
		{11, true},
		{7, true},
		{999, false},
		{121, false},
		{99, true},
		{101, true},
		{111, false},
		{75, true},
		{5, true},
		{102, true},
		{211, false},
		{21, true},
		{30, true},
		{81, true},
		{90, true},
	}

	for _, c := range cases {
		commitment, err := state.BridgeMessageStore.getCommitmentForBridgeEvents(c.stateSyncID)

		if c.hasCommitment {
			require.NoError(t, err, fmt.Sprintf("state sync %v", c.stateSyncID))
			require.Equal(t, c.hasCommitment, commitment.ContainsStateSync(c.stateSyncID))
		} else {
			require.ErrorIs(t, errNoCommitmentForStateSync, err)
		}
	}
}

func TestState_GetNestedBucketInEpoch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		epochNumber uint64
		bucketName  []byte
		errMsg      string
	}{
		{
			name:        "Not existing inner bucket",
			epochNumber: 3,
			bucketName:  []byte("Foo"),
			errMsg:      "could not find Foo bucket for epoch: 3",
		},
		{
			name:        "Happy path",
			epochNumber: 5,
			bucketName:  messageVotesBucket,
			errMsg:      "",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			var (
				nestedBucket *bbolt.Bucket
				err          error
			)

			s := newTestState(t)
			require.NoError(t, s.EpochStore.insertEpoch(c.epochNumber, nil, 0))

			err = s.db.View(func(tx *bbolt.Tx) error {
				nestedBucket, err = getNestedBucketInEpoch(tx, c.epochNumber, c.bucketName, 0)

				return err
			})
			if c.errMsg != "" {
				require.ErrorContains(t, err, c.errMsg)
				require.Nil(t, nestedBucket)
			} else {
				require.NoError(t, err)
				require.NotNil(t, nestedBucket)
			}
		})
	}
}

func createTestCommitmentMessage(t *testing.T) *CommitmentMessageSigned {
	t.Helper()

	msg := &contractsapi.BridgeMessageBatch{
		Messages:           []*contractsapi.BridgeMessage{},
		SourceChainID:      big.NewInt(1),
		DestinationChainID: big.NewInt(0),
	}

	return &CommitmentMessageSigned{
		MessageBatch: msg,
		AggSignature: Signature{},
	}
}

func insertTestCommitments(t *testing.T, state *State, numberOfCommitments uint64) {
	t.Helper()

	for i := uint64(0); i <= numberOfCommitments; i++ {
		commitment := createTestCommitmentMessage(t)
		require.NoError(t, state.BridgeMessageStore.insertCommitmentMessage(commitment, nil))
	}
}

func createTestStateSync(index int64) *contractsapi.StateSyncedEvent {
	return &contractsapi.StateSyncedEvent{
		ID:       big.NewInt(index),
		Sender:   types.ZeroAddress,
		Receiver: types.ZeroAddress,
		Data:     []byte{0, 1},
	}
}

func TestState_StateSync_StateSyncRelayerDataAndEvents(t *testing.T) {
	t.Parallel()

	state := newTestState(t)

	// update
	require.NoError(t, state.BridgeMessageStore.UpdateRelayerEvents([]*RelayerEventMetaData{
		{EventID: 2, DestinationChainID: 0},
		{EventID: 4, DestinationChainID: 0},
		{EventID: 7, SentStatus: true, BlockNumber: 100, DestinationChainID: 0},
	}, []*RelayerEventMetaData{}, nil))

	// get available events
	events, err := state.BridgeMessageStore.GetAllAvailableRelayerEvents(0)

	require.NoError(t, err)
	require.Len(t, events, 3)
	require.Equal(t, uint64(2), events[0].EventID)
	require.Equal(t, uint64(4), events[1].EventID)
	require.Equal(t, uint64(7), events[2].EventID)

	// update again
	require.NoError(t, state.BridgeMessageStore.UpdateRelayerEvents(
		[]*RelayerEventMetaData{
			{EventID: 10, DestinationChainID: 0},
			{EventID: 12, DestinationChainID: 0},
			{EventID: 11, DestinationChainID: 0},
		},
		[]*RelayerEventMetaData{{EventID: 4, DestinationChainID: 0}, {EventID: 7, DestinationChainID: 0}},
		nil,
	))

	// get available events
	events, err = state.BridgeMessageStore.GetAllAvailableRelayerEvents(1000)

	require.NoError(t, err)
	require.Len(t, events, 4)
	require.Equal(t, uint64(2), events[0].EventID)
	require.Equal(t, uint64(10), events[1].EventID)
	require.Equal(t, false, events[1].SentStatus)
	require.Equal(t, uint64(11), events[2].EventID)
	require.Equal(t, uint64(12), events[3].EventID)

	events[1].SentStatus = true
	require.NoError(t, state.BridgeMessageStore.UpdateRelayerEvents(events[1:2], []*RelayerEventMetaData{{EventID: 2, DestinationChainID: 0}}, nil))

	// get available events with limit
	events, err = state.BridgeMessageStore.GetAllAvailableRelayerEvents(2)

	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, uint64(10), events[0].EventID)
	require.Equal(t, true, events[0].SentStatus)
	require.Equal(t, uint64(11), events[1].EventID)
}
