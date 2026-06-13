package transport_test

import "testing"

// Wave-0 RED scaffolds for the auth gate middleware (AUTH-01). The http.Handler middleware
// that wraps the whole mux — covering /api, the embedded SPA, and the stream uniformly — is
// written in Plan 02; these skips fix the security contract Plan 02 must satisfy.

// TestAuthMiddlewareFailClosed will assert that, with auth enabled, a request without a
// valid session cookie is rejected (401) — the gate fails closed.
func TestAuthMiddlewareFailClosed(t *testing.T) {
	t.Parallel()
	t.Skip("Wave 0 scaffold — implemented in Plan 02 (AUTH-01 fail-closed gate)")
}

// TestAuthMiddlewareOffPasses will assert that, with auth disabled (the default), all
// requests pass through unauthenticated.
func TestAuthMiddlewareOffPasses(t *testing.T) {
	t.Parallel()
	t.Skip("Wave 0 scaffold — implemented in Plan 02 (AUTH-01 auth-off passthrough)")
}

// TestAuthMiddlewareLocalBypass will assert that requests from a loopback peer
// (real RemoteAddr) bypass the gate when local-bypass is enabled.
func TestAuthMiddlewareLocalBypass(t *testing.T) {
	t.Parallel()
	t.Skip("Wave 0 scaffold — implemented in Plan 02 (AUTH-01 local-bypass on real peer IP)")
}

// TestAuthMiddlewareIgnoresXFF will assert that the gate uses the raw socket RemoteAddr and
// ignores client-supplied X-Forwarded-For, so a spoofed XFF cannot forge a local bypass.
func TestAuthMiddlewareIgnoresXFF(t *testing.T) {
	t.Parallel()
	t.Skip("Wave 0 scaffold — implemented in Plan 02 (AUTH-01 ignore X-Forwarded-For)")
}
