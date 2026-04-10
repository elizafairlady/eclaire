package bus

import (
	"context"
	"sync"
	"time"
)

// Event is a message published on the bus.
type Event struct {
	Topic   string    `json:"topic"`
	Payload any       `json:"payload"`
	Time    time.Time `json:"time"`
}

// Bus is an in-memory typed pub/sub message bus.
type Bus struct {
	brokers map[string]*broker
	mu      sync.RWMutex
	seq     uint64
}

// NextSeq returns a monotonically increasing sequence number.
func (b *Bus) NextSeq() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	return b.seq
}

// New creates a new Bus.
func New() *Bus {
	return &Bus{
		brokers: make(map[string]*broker),
	}
}

// Publish sends an event to all subscribers of the given topic.
func (b *Bus) Publish(topic string, payload any) {
	ev := Event{
		Topic:   topic,
		Payload: payload,
		Time:    time.Now(),
	}

	b.mu.RLock()
	br, ok := b.brokers[topic]
	b.mu.RUnlock()

	if !ok {
		return
	}

	br.publish(ev)
}

// Subscribe returns a channel that receives events for the given topic.
// The subscription is cleaned up when ctx is cancelled.
func (b *Bus) Subscribe(ctx context.Context, topic string) <-chan Event {
	br := b.ensureBroker(topic)
	ch := make(chan Event, 32)

	br.mu.Lock()
	br.subs[ch] = struct{}{}
	br.mu.Unlock()

	go func() {
		<-ctx.Done()
		br.mu.Lock()
		delete(br.subs, ch)
		br.mu.Unlock()
		close(ch)
	}()

	return ch
}

// SubscribeFunc registers a callback for events on the given topic.
func (b *Bus) SubscribeFunc(ctx context.Context, topic string, fn func(Event)) {
	ch := b.Subscribe(ctx, topic)
	go func() {
		for ev := range ch {
			fn(ev)
		}
	}()
}

func (b *Bus) ensureBroker(topic string) *broker {
	b.mu.Lock()
	defer b.mu.Unlock()

	if br, ok := b.brokers[topic]; ok {
		return br
	}

	br := &broker{
		subs: make(map[chan Event]struct{}),
	}
	b.brokers[topic] = br
	return br
}

type broker struct {
	subs map[chan Event]struct{}
	mu   sync.RWMutex
}

func (br *broker) publish(ev Event) {
	br.mu.RLock()
	defer br.mu.RUnlock()

	for ch := range br.subs {
		select {
		case ch <- ev:
		default:
			// drop if subscriber is slow
		}
	}
}
