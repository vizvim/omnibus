// Package events is the small in-process pub/sub the live-status stream is fed by (UI-05,
// D-08). Producers (the job engine, the tracking poll loop, issue-event writes) Publish a
// typed EventEnvelope; the EventService stream handler Subscribes and Sends each envelope to
// its client. Publish is non-blocking: a slow or dead subscriber's buffer drops events
// rather than stalling a producer (T-6-21), so the bus can never wedge the job/tracking
// loops. The bus is server-originated only — no client input is ever reflected here.
package events

import (
	"sync"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
)

// subscriberBuffer bounds each subscriber's pending-event buffer. When a subscriber falls
// this far behind, further Publishes drop for that subscriber (drop-on-full) so producers
// never block. Sized to absorb a reasonable burst while a client reconnects without growing
// unbounded for a dead client.
const subscriberBuffer = 256

// Bus is the in-process fan-out the producers Publish to and the stream handler Subscribes
// to. It is safe for concurrent use.
type Bus struct {
	mu   sync.Mutex
	subs map[*Subscription]struct{}
}

// NewBus constructs an empty Bus.
func NewBus() *Bus {
	return &Bus{subs: make(map[*Subscription]struct{})}
}

// Subscription is one stream consumer. The handler ranges over C; Close removes the
// subscription from the bus and releases its channel. Close is idempotent.
type Subscription struct {
	// C delivers published envelopes. The bus never blocks on a send to C — a full C drops
	// the event for this subscriber (T-6-21).
	C chan *omnibusv1.EventEnvelope

	bus    *Bus
	once   sync.Once
	closed chan struct{}
}

// Subscribe registers and returns a new Subscription with a bounded buffer.
func (b *Bus) Subscribe() *Subscription {
	sub := &Subscription{
		C:      make(chan *omnibusv1.EventEnvelope, subscriberBuffer),
		bus:    b,
		closed: make(chan struct{}),
	}
	b.mu.Lock()
	b.subs[sub] = struct{}{}
	b.mu.Unlock()
	return sub
}

// Publish fans an envelope out to every current subscriber without blocking. A subscriber
// whose buffer is full (slow/dead client) has the event dropped rather than stalling the
// producer (T-6-21). Publish never blocks and is safe to call from the job/tracking loops.
func (b *Bus) Publish(env *omnibusv1.EventEnvelope) {
	b.mu.Lock()
	// Snapshot the current subscribers so the non-blocking sends below happen without holding
	// the lock (a send is bounded by the buffer, never blocks, but keeping the critical
	// section minimal avoids contending with Subscribe/Close under load).
	subs := make([]*Subscription, 0, len(b.subs))
	for sub := range b.subs {
		subs = append(subs, sub)
	}
	b.mu.Unlock()

	for _, sub := range subs {
		select {
		case <-sub.closed:
			// Subscriber is closing/closed — skip it.
		case sub.C <- env:
			// Delivered.
		default:
			// Buffer full (slow/dead subscriber): drop for this subscriber so the producer
			// never blocks (T-6-21).
		}
	}
}

// Close removes the subscription from the bus and signals that no further events should be
// delivered to it. It is idempotent and safe to call concurrently with Publish.
func (s *Subscription) Close() {
	s.once.Do(func() {
		close(s.closed)
		s.bus.mu.Lock()
		delete(s.bus.subs, s)
		s.bus.mu.Unlock()
	})
}
