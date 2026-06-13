package transport_test

import "testing"

// Wave-0 RED scaffolds for the AuthService transport handler (AUTH-01). The handler and
// its Login RPC are written in Plan 02; these skips mark the contract Plan 02 must satisfy
// so it converts each into a real assertion rather than inventing one.

// TestAuthLoginSetsCookie will assert that a successful Login sets a signed, httpOnly,
// SameSite session cookie on the response (cookie set directly in the Login handler).
func TestAuthLoginSetsCookie(t *testing.T) {
	t.Parallel()
	t.Skip("Wave 0 scaffold — implemented in Plan 02 (AUTH-01 Login sets session cookie)")
}

// TestAuthLoginBadCredsGenericError will assert that bad credentials return a single
// generic error (no username/password-existence oracle).
func TestAuthLoginBadCredsGenericError(t *testing.T) {
	t.Parallel()
	t.Skip("Wave 0 scaffold — implemented in Plan 02 (AUTH-01 generic auth error)")
}
