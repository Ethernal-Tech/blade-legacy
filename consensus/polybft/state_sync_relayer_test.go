package polybft

import (
	"errors"
	"testing"
	"time"

	"github.com/0xPolygon/polygon-edge/types"
	"github.com/Ethernal-Tech/ethgo"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestStateSyncRelayer_FullWorkflow(t *testing.T) {
	t.Skip()
	t.Parallel()

	testKey := createTestKey(t)
	bridgeMessageAddr := types.StringToAddress("0x56563")

	headers := []*types.Header{
		{Number: 2}, {Number: 3}, {Number: 4}, {Number: 5},
	}

	blockhainMock := &blockchainMock{}
	dummyTxRelayer := newDummyStakeTxRelayer(t, nil)
	state := newTestState(t)

	stateSyncRelayer := newStateSyncRelayer(
		dummyTxRelayer,
		state.BridgeMessageStore,
		blockhainMock,
		testKey,
		&relayerConfig{
			maxAttemptsToSend:        6,
			maxBlocksToWaitForResend: 1,
			maxEventsPerBatch:        1,
			eventExecutionAddr:       bridgeMessageAddr,
		},
		hclog.Default(),
	)

	for _, h := range headers {
		blockhainMock.On("CurrentHeader").Return(h).Once()
	}

	// send first two events without errors
	dummyTxRelayer.On("SendTransaction", mock.Anything, testKey).Return((*ethgo.Receipt)(nil), nil).Times(2)
	// fail 3rd time
	dummyTxRelayer.On("SendTransaction", mock.Anything, testKey).Return(
		(*ethgo.Receipt)(nil), errors.New("e")).Once()
	// send 3 events all at once at the end
	dummyTxRelayer.On("SendTransaction", mock.Anything, testKey).Return((*ethgo.Receipt)(nil), nil).Once()

	require.NoError(t, stateSyncRelayer.Init())

	// post 1st block
	require.NoError(t, stateSyncRelayer.PostBlock(&PostBlockRequest{}))

	time.Sleep(time.Second * 2) // wait for some time

	events, err := state.BridgeMessageStore.GetAllAvailableRelayerEvents(0)

	require.NoError(t, err)
	require.Len(t, events, 3)
	require.Equal(t, uint64(1), events[0].EventID)
	require.True(t, events[0].SentStatus)
	require.False(t, events[1].SentStatus)
	require.False(t, events[2].SentStatus)

	require.NoError(t, stateSyncRelayer.PostBlock(&PostBlockRequest{}))

	time.Sleep(time.Second * 2) // wait for some time

	events, err = state.BridgeMessageStore.GetAllAvailableRelayerEvents(0)

	require.NoError(t, err)
	require.Len(t, events, 4)
	require.True(t, events[0].SentStatus)
	require.Equal(t, uint64(2), events[0].EventID)
	require.False(t, events[1].SentStatus)
	require.False(t, events[2].SentStatus)

	time.Sleep(time.Second * 2) // wait for some time

	events, err = state.BridgeMessageStore.GetAllAvailableRelayerEvents(0)

	require.NoError(t, err)
	require.Len(t, events, 3)
	require.True(t, events[0].SentStatus)
	require.Equal(t, uint64(3), events[0].EventID)
	require.False(t, events[1].SentStatus)

	// post 4th block - will not provide result, so one more SendTransaction will be triggered
	stateSyncRelayer.config.maxEventsPerBatch = 3 // send all 3 left events at once

	require.NoError(t, stateSyncRelayer.PostBlock(&PostBlockRequest{}))

	time.Sleep(time.Second * 2) // wait for some time

	events, err = state.BridgeMessageStore.GetAllAvailableRelayerEvents(0)

	require.NoError(t, err)
	require.Len(t, events, 3)
	require.True(t, events[0].SentStatus && events[1].SentStatus && events[2].SentStatus)

	time.Sleep(time.Second * 2) // wait for some time

	events, err = state.BridgeMessageStore.GetAllAvailableRelayerEvents(0)

	require.NoError(t, err)
	require.Len(t, events, 0)

	stateSyncRelayer.Close()
	time.Sleep(time.Second)

	blockhainMock.AssertExpectations(t)
	dummyTxRelayer.AssertExpectations(t)
}
