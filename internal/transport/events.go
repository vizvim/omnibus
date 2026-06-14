package transport

import (
	"context"

	"connectrpc.com/connect"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/events"
)

// EventBus is the subset of events.Bus the stream handler depends on: it Subscribes to the
// in-process fan-out the producers Publish to. Declaring the narrow interface keeps the
// transport layer depending only on the seam it uses.
type EventBus interface {
	Subscribe() *events.Subscription
}

// EventHandler implements the generated EventServiceHandler: a single server-streaming RPC
// that subscribes to the in-process event bus and Sends each published envelope to the
// client until the client context is canceled (UI-05, D-08). The stream rides the same auth
// gate as every other route — the wrapping middleware validates the cookie on the initial
// request (ADR 0008, T-6-20).
type EventHandler struct {
	omnibusv1connect.UnimplementedEventServiceHandler
	bus EventBus
}

// NewEventHandler builds the EventService Connect handler over the bus.
func NewEventHandler(bus EventBus) *EventHandler {
	return &EventHandler{bus: bus}
}

// StreamEvents subscribes to the bus and Sends each envelope to the client. It returns nil
// when the client context is canceled (clean teardown, no leak): the deferred Close
// unsubscribes so a disconnected client stops consuming buffer. A Send error (client gone)
// returns that error so Connect tears the stream down.
func (h *EventHandler) StreamEvents(
	ctx context.Context,
	_ *connect.Request[omnibusv1.StreamEventsRequest],
	stream *connect.ServerStream[omnibusv1.EventEnvelope],
) error {
	sub := h.bus.Subscribe()
	defer sub.Close()

	for {
		select {
		case <-ctx.Done():
			// Client disconnected / canceled: return cleanly so the goroutine and
			// subscription are released (no leak).
			return nil
		case env := <-sub.C:
			if err := stream.Send(env); err != nil {
				return err
			}
		}
	}
}
