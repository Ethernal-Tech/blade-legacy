package pubsub

import "sync"

type PubSub struct {
	subscribers map[string][]chan interface{}
	mu          sync.RWMutex
}

func NewPubSub() *PubSub {
	return &PubSub{
		subscribers: make(map[string][]chan interface{}),
	}
}

func (ps *PubSub) Subscribe(topic string, event chan interface{}) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.subscribers[topic] = append(ps.subscribers[topic], event)
}

func (ps *PubSub) Publish(topic string, msg interface{}) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if chans, ok := ps.subscribers[topic]; ok {
		for _, ch := range chans {
			ch <- msg
		}
	}
}

func (ps *PubSub) Unsubscribe(topic string, sub <-chan interface{}) {
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
