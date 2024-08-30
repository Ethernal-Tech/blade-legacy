package polybft

import (
	"bytes"
	"fmt"
	"math/big"
	"testing"

	"github.com/0xPolygon/polygon-edge/bls"
	"github.com/0xPolygon/polygon-edge/consensus/polybft/contractsapi"
	"github.com/0xPolygon/polygon-edge/crypto"
	"github.com/0xPolygon/polygon-edge/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"
)

var domain = crypto.Keccak256([]byte("DOMAIN"))

func TestState_InsertEvent(t *testing.T) {
	t.Parallel()

	state := newTestState(t)
	event1 := &contractsapi.BridgeMsgEvent{
		ID:                 big.NewInt(0),
		Sender:             types.Address{},
		Receiver:           types.Address{},
		Data:               []byte{},
		SourceChainID:      big.NewInt(1),
		DestinationChainID: bigZero,
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

func TestState_getBridgeEventsForBridgeBatch_NotEnoughEvents(t *testing.T) {
	t.Parallel()

	state := newTestState(t)

	for i := 0; i < maxNumberOfEvents-2; i++ {
		assert.NoError(t, state.BridgeMessageStore.insertBridgeMessageEvent(&contractsapi.BridgeMsgEvent{
			ID:                 big.NewInt(int64(i)),
			Data:               []byte{1, 2},
			SourceChainID:      big.NewInt(1),
			DestinationChainID: bigZero,
		}))
	}

	_, err := state.BridgeMessageStore.getBridgeMessageEventsForBridgeBatch(0, maxNumberOfEvents-1, nil, 0)
	assert.ErrorIs(t, err, errNotEnoughBridgeEvents)
}

func TestState_getBridgeEventsForBridgeBatch(t *testing.T) {
	t.Parallel()

	state := newTestState(t)

	for i := 0; i < maxNumberOfEvents; i++ {
		assert.NoError(t, state.BridgeMessageStore.insertBridgeMessageEvent(&contractsapi.BridgeMsgEvent{
			ID:                 big.NewInt(int64(i)),
			Data:               []byte{1, 2},
			SourceChainID:      big.NewInt(1),
			DestinationChainID: bigZero,
		}))
	}

	t.Run("Return all - forced. Enough events", func(t *testing.T) {
		t.Parallel()

		events, err := state.BridgeMessageStore.getBridgeMessageEventsForBridgeBatch(0, maxNumberOfEvents-1, nil, 0)
		require.NoError(t, err)
		require.Equal(t, maxNumberOfEvents, len(events))
	})

	t.Run("Return all - forced. Not enough events", func(t *testing.T) {
		t.Parallel()

		_, err := state.BridgeMessageStore.getBridgeMessageEventsForBridgeBatch(0, maxNumberOfEvents+1, nil, 0)
		require.ErrorIs(t, err, errNotEnoughBridgeEvents)
	})

	t.Run("Return all you can. Enough events", func(t *testing.T) {
		t.Parallel()

		events, err := state.BridgeMessageStore.getBridgeMessageEventsForBridgeBatch(0, maxNumberOfEvents-1, nil, 0)
		assert.NoError(t, err)
		assert.Equal(t, maxNumberOfEvents, len(events))
	})

	t.Run("Return all you can. Not enough events", func(t *testing.T) {
		t.Parallel()

		events, err := state.BridgeMessageStore.getBridgeMessageEventsForBridgeBatch(0, maxNumberOfEvents+1, nil, 0)
		assert.ErrorIs(t, err, errNotEnoughBridgeEvents)
		assert.Equal(t, maxNumberOfEvents, len(events))
	})
}

func TestState_insertBridgeBatchMessage(t *testing.T) {
	t.Parallel()

	signedBridgeBatch := createTestBridgeBatchMessage(t, 0, 0)

	state := newTestState(t)
	assert.NoError(t, state.BridgeMessageStore.insertBridgeBatchMessage(signedBridgeBatch, nil))

	batchFromDB, err := state.BridgeMessageStore.getBridgeBatchSigned(0, 0)

	assert.NoError(t, err)
	assert.NotNil(t, batchFromDB)
	assert.Equal(t, signedBridgeBatch, batchFromDB)
}

func TestState_getBridgeBatchForBridgeEvents(t *testing.T) {
	const (
		numOfBridgeBatches = 10
	)

	state := newTestState(t)

	insertTestBridgeBatches(t, state, numOfBridgeBatches)

	var cases = []struct {
		bridgeMessageID uint64
		hasBatch        bool
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
		signedBridgeBatch, err := state.BridgeMessageStore.getBridgeBatchForBridgeEvents(c.bridgeMessageID, 0)

		if c.hasBatch {
			require.NoError(t, err, fmt.Sprintf("bridge event %v", c.bridgeMessageID))
			require.Equal(t, c.hasBatch, signedBridgeBatch.ContainsBridgeMessage(c.bridgeMessageID))
		} else {
			require.ErrorIs(t, errNoBridgeBatchForBridgeEvent, err)
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

func createTestBridgeBatchMessage(t *testing.T, numberOfMessages, firstIndex uint64) *BridgeBatchSigned {
	t.Helper()

	messages := make([]*contractsapi.BridgeMessage, numberOfMessages)

	for i := firstIndex; i < firstIndex+numberOfMessages; i++ {
		messages[i-firstIndex] = &contractsapi.BridgeMessage{
			ID:                 new(big.Int).SetUint64(i),
			SourceChainID:      big.NewInt(1),
			DestinationChainID: bigZero,
			Sender:             types.Address{},
			Receiver:           types.Address{},
			Payload:            []byte{}}
	}

	msg := contractsapi.BridgeMessageBatch{
		Messages:           messages,
		SourceChainID:      big.NewInt(1),
		DestinationChainID: big.NewInt(0),
	}

	blsKey, err := bls.GenerateBlsKey()
	require.NoError(t, err)

	data := generateRandomBytes(t)

	signature, err := blsKey.Sign(data, domain)
	require.NoError(t, err)

	signatures := bls.Signatures{signature}

	aggSig, err := signatures.Aggregate().Marshal()
	require.NoError(t, err)

	return &BridgeBatchSigned{
		MessageBatch: &msg,
		AggSignature: Signature{AggregatedSignature: aggSig},
	}
}

func insertTestBridgeBatches(t *testing.T, state *State, numberOfBatches uint64) {
	t.Helper()

	for i := uint64(0); i <= numberOfBatches; i++ {
		signedBridgeBatch := createTestBridgeBatchMessage(t, 10, 10*i)
		require.NoError(t, state.BridgeMessageStore.insertBridgeBatchMessage(signedBridgeBatch, nil))
	}
}

func createTestBridgeMessageEvent(index int64) *contractsapi.StateSyncedEvent {
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
