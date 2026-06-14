package events_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	"github.com/vizvim/omnibus/internal/events"
)

// TestBusPublishFanOut asserts a published envelope reaches all current subscribers.
func TestBusPublishFanOut(t *testing.T) {
	t.Parallel()

	bus := events.NewBus()
	subA := bus.Subscribe()
	defer subA.Close()
	subB := bus.Subscribe()
	defer subB.Close()

	env := &omnibusv1.EventEnvelope{
		OccurredAt: "2026-06-13T00:00:00Z",
		Event: &omnibusv1.EventEnvelope_JobState{
			JobState: &omnibusv1.JobStateEvent{Kind: "import_series", State: "running"},
		},
	}
	bus.Publish(env)

	gotA := receive(t, subA)
	gotB := receive(t, subB)
	assert.Equal(t, "import_series", gotA.GetJobState().GetKind())
	assert.Equal(t, "import_series", gotB.GetJobState().GetKind())
}

// TestBusCloseStopsDelivery asserts a Close()d subscriber stops receiving new events.
func TestBusCloseStopsDelivery(t *testing.T) {
	t.Parallel()

	bus := events.NewBus()
	sub := bus.Subscribe()
	sub.Close()

	// Publishing after Close must not panic and must not deliver to the closed subscriber.
	bus.Publish(&omnibusv1.EventEnvelope{OccurredAt: "x"})

	select {
	case _, ok := <-sub.C:
		// A closed subscriber's channel may be drained/closed, but it must never deliver a
		// live event published after Close.
		assert.False(t, ok, "closed subscriber received an event published after Close")
	case <-time.After(50 * time.Millisecond):
		// No delivery — expected.
	}

	// A second Close is a safe no-op (idempotent).
	assert.NotPanics(t, func() { sub.Close() })
}

// TestBusSlowSubscriberDoesNotBlockProducers asserts a slow/full subscriber never blocks
// Publish — producers (job/tracking loops) must never stall on a dead client (T-6-21).
func TestBusSlowSubscriberDoesNotBlockProducers(t *testing.T) {
	t.Parallel()

	bus := events.NewBus()
	slow := bus.Subscribe() // never drained
	defer slow.Close()
	fast := bus.Subscribe()
	defer fast.Close()

	done := make(chan struct{})
	go func() {
		// Publish far more than any per-subscriber buffer could hold; this must return
		// promptly (drop-on-full), not block on the undrained slow subscriber.
		for range 10_000 {
			bus.Publish(&omnibusv1.EventEnvelope{OccurredAt: "y"})
		}
		close(done)
	}()

	select {
	case <-done:
		// Producers completed without blocking — expected.
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a slow subscriber (producers can stall)")
	}

	// The fast subscriber still receives at least one event despite the slow one.
	bus.Publish(&omnibusv1.EventEnvelope{
		OccurredAt: "z",
		Event:      &omnibusv1.EventEnvelope_JobState{JobState: &omnibusv1.JobStateEvent{Kind: "k", State: "s"}},
	})
	got := receive(t, fast)
	assert.NotNil(t, got)
}

// TestBusConcurrentSubscribePublish exercises concurrent Subscribe/Publish/Close for the
// race detector (no panic, no deadlock).
func TestBusConcurrentSubscribePublish(t *testing.T) {
	t.Parallel()

	bus := events.NewBus()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			sub := bus.Subscribe()
			for range 100 {
				bus.Publish(&omnibusv1.EventEnvelope{OccurredAt: "c"})
			}
			sub.Close()
		})
	}
	wg.Wait()
}

// receive reads one envelope from a subscription with a bounded timeout.
func receive(t *testing.T, sub *events.Subscription) *omnibusv1.EventEnvelope {
	t.Helper()
	select {
	case env, ok := <-sub.C:
		require.True(t, ok, "subscription channel closed unexpectedly")
		return env
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return nil
	}
}
