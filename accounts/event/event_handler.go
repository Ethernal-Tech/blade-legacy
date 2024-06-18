package event

import "sync"

type EventHandler struct {
	subscribers map[string][]chan Event
	mu          sync.RWMutex
}

func NewEventHandler() *EventHandler {
	return &EventHandler{
		subscribers: make(map[string][]chan Event),
	}
}

func (ps *EventHandler) Subscribe(topic string, event chan Event) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.subscribers[topic] = append(ps.subscribers[topic], event)
}

func (ps *EventHandler) Publish(topic string, msg Event) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if chans, ok := ps.subscribers[topic]; ok {
		for _, ch := range chans {
			ch <- msg
		}
	}
}

func (ps *EventHandler) Unsubscribe(topic string, sub <-chan Event) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if chans, ok := ps.subscribers[topic]; ok {
		for i, ch := range chans {
			if ch == sub {
				ps.subscribers[topic] = append(chans[:i], chans[i+1:]...)

				close(ch)

				break
			}
		}
	}
}

type EventType byte

const (
	WalletEventType EventType = 0x01

	NewBackendType EventType = 0x02
)

type Event interface {
	Type() EventType
}
