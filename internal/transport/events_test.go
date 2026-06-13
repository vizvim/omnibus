package transport_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/events"
	"github.com/vizvim/omnibus/internal/transport"
)

// newEventTestServer spins up the EventService handler over a TLS HTTP/2 test server — the
// idiomatic connect-go streaming test setup — so server-streaming flushes envelopes to the
// client frame-by-frame instead of buffering them as an HTTP/1.1 response would. The server's
// own client is pre-configured to trust its cert and negotiate HTTP/2. Returns a client plus
// the bus the producers publish to.
func newEventTestServer(t *testing.T) (omnibusv1connect.EventServiceClient, *events.Bus) {
	t.Helper()
	bus := events.NewBus()
	handler := transport.NewEventHandler(bus)
	path, h := omnibusv1connect.NewEventServiceHandler(handler)

	mux := http.NewServeMux()
	mux.Handle(path, h)
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	// Close client connections first so a server handler still parked in its stream select
	// observes ctx.Done() (conn reset) and returns, then Close the listener. Close() is run
	// with a bounded timeout so a stuck handler fails the test fast rather than wedging the
	// package to its global timeout.
	t.Cleanup(func() {
		srv.CloseClientConnections()
		closed := make(chan struct{})
		go func() { srv.Close(); close(closed) }()
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			t.Error("test server Close did not return (server-side stream handler leak)")
		}
	})

	client := omnibusv1connect.NewEventServiceClient(srv.Client(), srv.URL)
	return client, bus
}

// TestStreamEventsSendLoop asserts the stream Sends queued event envelopes published to the
// bus to the client over the server-streaming RPC (UI-05 Send loop).
func TestStreamEventsSendLoop(t *testing.T) {
	t.Parallel()

	client, bus := newEventTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	env := &omnibusv1.EventEnvelope{
		OccurredAt: "2026-06-13T00:00:00Z",
		Event: &omnibusv1.EventEnvelope_JobState{
			JobState: &omnibusv1.JobStateEvent{Kind: "import_series", State: "running"},
		},
	}

	// Connect server-streaming sends response headers lazily (on the first Send), so the
	// initial Receive blocks until the handler has an event to flush. Publish on a ticker in
	// the background so whenever the handler completes its Subscribe there is an event waiting
	// — this removes the open-vs-subscribe race without an arbitrary sleep.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			bus.Publish(env)
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
		}
	}()

	stream, err := client.StreamEvents(ctx, connect.NewRequest(&omnibusv1.StreamEventsRequest{}))
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()

	require.True(t, stream.Receive(), "stream did not receive a published event: %v", stream.Err())
	got := stream.Msg()
	require.NotNil(t, got)
	assert.Equal(t, "import_series", got.GetJobState().GetKind())
}

// TestStreamEventsContextCancel asserts the stream returns cleanly (no leak / no error
// surfaced as a server fault) when the client context is canceled. The drain is run in a
// goroutine and bounded so a regression that wedges the handler fails fast instead of
// hanging the suite to the package timeout.
func TestStreamEventsContextCancel(t *testing.T) {
	t.Parallel()

	client, bus := newEventTestServer(t)

	// Start the background publisher BEFORE opening the stream: connect server-streaming sends
	// response headers lazily (on the first Send), so the eager StreamEvents call blocks until
	// the handler has an event to flush. A running publisher guarantees the call returns.
	stopPub := make(chan struct{})
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			bus.Publish(&omnibusv1.EventEnvelope{
				OccurredAt: "t",
				Event:      &omnibusv1.EventEnvelope_JobState{JobState: &omnibusv1.JobStateEvent{Kind: "k", State: "s"}},
			})
			select {
			case <-stopPub:
				return
			case <-ticker.C:
			}
		}
	}()
	defer close(stopPub)

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := client.StreamEvents(ctx, connect.NewRequest(&omnibusv1.StreamEventsRequest{}))
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()

	// Drain the stream; once it is flowing, cancel the client context. The server handler must
	// observe ctx.Done() (the h2 transport resets the stream), return cleanly, and the
	// client-side Receive loop must then terminate (no leak, no internal fault).
	drained := make(chan error, 1)
	go func() {
		for stream.Receive() {
			// Drain frames until the stream closes.
		}
		drained <- stream.Err()
	}()

	time.Sleep(150 * time.Millisecond) // let a few frames flow
	cancel()

	select {
	case rerr := <-drained:
		if rerr != nil {
			assert.NotEqual(t, connect.CodeInternal, connect.CodeOf(rerr),
				"context cancel should not surface as an internal server error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not terminate after context cancel (handler leak)")
	}
}
