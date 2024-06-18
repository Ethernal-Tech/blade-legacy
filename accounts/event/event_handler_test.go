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
	ps := NewEventHandler()

	var err error

	eventChan1 := make(chan Event, 1)

	eventChan2 := make(chan Event, 1)

	ps.Subscribe(topic, eventChan1)

	ps.Subscribe(topic, eventChan2)

	event := TestEvent{Data: "testEvent"}

	ps.Publish(topic, event)

	select {
	case receivedEvent := <-eventChan1:
		require.Equal(t, event, receivedEvent)
	case <-time.After(1 * time.Second):
		err = errors.New("did not receive event on eventChan1")
	}

	require.NoError(t, err)

	select {
	case receivedEvent := <-eventChan2:
		require.Equal(t, event, receivedEvent)
	case <-time.After(1 * time.Second):
		err = errors.New("did not receive event on eventChan2")
	}

	require.NoError(t, err)
}
