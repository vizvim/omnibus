package transport_test

import "testing"

// Wave-0 RED scaffolds for the live-status server-streaming RPC (UI-05). The EventService
// stream handler is written in Plan 04; these skips fix the streaming contract Plan 04 must
// satisfy (oneof-envelope Send loop + clean context-cancel teardown).

// TestStreamEventsSendLoop will assert that the stream Sends queued event envelopes to the
// client over the server-streaming RPC.
func TestStreamEventsSendLoop(t *testing.T) {
	t.Parallel()
	t.Skip("Wave 0 scaffold — implemented in Plan 04 (UI-05 stream Send loop)")
}

// TestStreamEventsContextCancel will assert that the stream returns cleanly (no leak) when
// the client context is canceled.
func TestStreamEventsContextCancel(t *testing.T) {
	t.Parallel()
	t.Skip("Wave 0 scaffold — implemented in Plan 04 (UI-05 stream context-cancel teardown)")
}
