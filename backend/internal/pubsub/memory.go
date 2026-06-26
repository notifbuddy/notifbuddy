package pubsub

import (
	"context"
	"sync"
)

// Subscriber receives messages delivered to a topic it subscribed to. It runs
// on the bus's delivery goroutine, so it should be quick or hand off async work.
type Subscriber func(ctx context.Context, msg Message)

// MemoryBus is an in-process Publisher with synchronous subscriber fan-out, used
// for local development. Subscribing is a property of this concrete backend, not
// of the Publisher interface — a production backend like SNS has no in-process
// subscribers, so callers that only publish stay backend-agnostic.
type MemoryBus struct {
	mu   sync.RWMutex
	subs map[string][]Subscriber
}

// NewMemoryBus returns an empty in-memory bus.
func NewMemoryBus() *MemoryBus {
	return &MemoryBus{subs: make(map[string][]Subscriber)}
}

// Subscribe registers fn to receive messages published to topic. Not safe to
// call concurrently with Publish for the same topic mid-delivery, but fine
// during startup wiring (the common case).
func (b *MemoryBus) Subscribe(topic string, fn Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[topic] = append(b.subs[topic], fn)
}

// Publish delivers the message to every subscriber of msg.Topic synchronously.
// With no subscribers it is a no-op. It never returns an error — local delivery
// can't fail in a way the caller should react to — keeping behavior aligned with
// the best-effort contract.
func (b *MemoryBus) Publish(ctx context.Context, msg Message) error {
	b.mu.RLock()
	subs := append([]Subscriber(nil), b.subs[msg.Topic]...)
	b.mu.RUnlock()
	for _, fn := range subs {
		fn(ctx, msg)
	}
	return nil
}
