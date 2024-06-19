package event

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	topic = "testTopic"

	TestEventType EventType = 0xff
)

type TestEvent struct {
	Data string
}

func (te TestEvent) Type() EventType {
	return TestEventType
}

func TestSubscribeAndPublish(t *testing.T) {
	ps := NewEventHandler()

	var err error

	eventChan := make(chan Event, 1)

	ps.Subscribe(topic, eventChan)

	event := TestEvent{Data: "testEvent"}

	ps.Publish(topic, event)

	select {
	case receivedEvent := <-eventChan:
		require.Equal(t, event, receivedEvent)
	case <-time.After(1 * time.Second):
		err = errors.New("did not receive event")
	}

	require.NoError(t, err)
}

func TestUnsubscribe(t *testing.T) {
	ps := NewEventHandler()

	var ok bool

	eventChan := make(chan Event, 1)

	ps.Subscribe(topic, eventChan)

	ps.Unsubscribe(topic, eventChan)

	ps.Publish(topic, TestEvent{Data: "testEvent"})

	select {
	case _, ok = <-eventChan:
	default:
	}

	require.False(t, ok, "expected channel tobe closed")
}

func TestConcurrentAccess(t *testing.T) {
	ps := NewEventHandler()

	eventChan := make(chan Event, 1)

	ps.Subscribe(topic, eventChan)

	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()
		ps.Publish(topic, TestEvent{Data: "testEvent"})
	}()

	go func() {
		defer wg.Done()
		ps.Unsubscribe(topic, eventChan)
	}()

	wg.Wait()

	select {
	case <-eventChan:
		// We don't care about the result, just checking for race conditions
	case <-time.After(1 * time.Second):
	}
}

func TestMultipleSubscribers(t *testing.T) {
	const subscribers = 2

	var (
		channels [subscribers]chan Event
		received byte
		err      error
	)

	ps := NewEventHandler()

	for i := 0; i < subscribers; i++ {
		channels[i] = make(chan Event, 1)
		ps.Subscribe(topic, channels[i])
	}

	event := TestEvent{Data: "testEvent"}

	ps.Publish(topic, event)

	for {
		var receivedEvent Event

		select {
		case receivedEvent = <-channels[0]:
			received |= 0x01
		case receivedEvent = <-channels[1]:
			received |= 0x02
		case <-time.After(1 * time.Second):
			err = errors.New("did not receive event on eventChan1")
		}

		if err != nil {
			break
		}

		require.Equal(t, event, receivedEvent)

		if received == 0x03 {
			break
		}
	}

	require.NoError(t, err)
}
