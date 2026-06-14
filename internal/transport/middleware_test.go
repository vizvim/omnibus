package transport_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/auth"
	"github.com/vizvim/omnibus/internal/transport"
)

// fakeAuthGate stubs the AuthGate the middleware depends on. validToken is the only token
// VerifySession accepts; any other token (or none) is rejected.
type fakeAuthGate struct {
	cfg        auth.Config
	cfgErr     error
	validToken string
}

func (f *fakeAuthGate) GetConfig(context.Context) (auth.Config, error) {
	return f.cfg, f.cfgErr
}

func (f *fakeAuthGate) VerifySession(token string) (string, error) {
	if f.validToken != "" && token == f.validToken {
		return "admin", nil
	}
	return "", auth.ErrInvalidSession
}

// nextRecorder is the wrapped handler; called records whether the gate let the request
// through.
type nextRecorder struct{ called bool }

func (n *nextRecorder) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	n.called = true
	w.WriteHeader(http.StatusOK)
}

func serve(t *testing.T, gate transport.AuthGate, trustProxy bool, r *http.Request) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	next := &nextRecorder{}
	mw := transport.NewAuthMiddleware(gate, trustProxy, next)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, r)
	return rec, next.called
}

// TestAuthMiddlewareOffPasses asserts that with auth Off (the default) every request
// passes through unauthenticated.
func TestAuthMiddlewareOffPasses(t *testing.T) {
	t.Parallel()
	gate := &fakeAuthGate{cfg: auth.Config{Mode: auth.ModeOff}}
	r := httptest.NewRequest(http.MethodGet, "/api/whatever", http.NoBody)
	r.RemoteAddr = "203.0.113.7:5555" // public peer, no cookie — still passes when Off

	rec, called := serve(t, gate, false, r)
	require.True(t, called, "Off lets every request through")
	require.Equal(t, http.StatusOK, rec.Code)
}

// TestAuthMiddlewareFailClosed asserts that with auth On a request for a GATED data
// resource without a valid session cookie is rejected (401) and next is NOT called —
// including on a config-load error. It exercises the real security boundary (a protected
// /api RPC and a /covers blob), NOT the SPA shell (which is intentionally ungated, see
// TestAuthMiddlewareServesSPAShellWhenGated).
func TestAuthMiddlewareFailClosed(t *testing.T) {
	t.Parallel()

	// No cookie at all — a protected data RPC.
	gate := &fakeAuthGate{cfg: auth.Config{Mode: auth.ModeOn}, validToken: "good-token"}
	r := httptest.NewRequest(http.MethodPost, "/api/omnibus.v1.SeriesService/ListSeries", http.NoBody)
	r.RemoteAddr = "203.0.113.7:5555"
	rec, called := serve(t, gate, false, r)
	require.False(t, called, "On with no cookie does not call next on a protected /api RPC")
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	// A gated cover blob with no cookie is also denied.
	rCov := httptest.NewRequest(http.MethodGet, "/covers/123.jpg", http.NoBody)
	rCov.RemoteAddr = "203.0.113.7:5555"
	recCov, calledCov := serve(t, gate, false, rCov)
	require.False(t, calledCov, "On with no cookie does not call next on a /covers blob")
	require.Equal(t, http.StatusUnauthorized, recCov.Code)

	// Invalid/tampered cookie on a protected /api RPC.
	r2 := httptest.NewRequest(http.MethodPost, "/api/omnibus.v1.SeriesService/ListSeries", http.NoBody)
	r2.RemoteAddr = "203.0.113.7:5555"
	r2.AddCookie(&http.Cookie{Name: "omnibus_session", Value: "tampered"})
	rec2, called2 := serve(t, gate, false, r2)
	require.False(t, called2, "On with an invalid cookie does not call next")
	require.Equal(t, http.StatusUnauthorized, rec2.Code)

	// Config-load error also fails closed (on a gated /api RPC).
	errGate := &fakeAuthGate{cfgErr: context.DeadlineExceeded}
	r3 := httptest.NewRequest(http.MethodPost, "/api/omnibus.v1.SeriesService/ListSeries", http.NoBody)
	r3.RemoteAddr = "127.0.0.1:5555"
	rec3, called3 := serve(t, errGate, false, r3)
	require.False(t, called3, "a config-load error fails closed")
	require.Equal(t, http.StatusUnauthorized, rec3.Code)
}

// TestAuthMiddlewareServesSPAShellWhenGated asserts the standard SPA-auth pattern: with
// auth On and NO cookie, the static SPA shell, its assets, and client-side deep routes load
// ungated (so the in-app login screen is reachable), while the real data boundary — a
// protected /api RPC and a /covers blob — stays gated (401).
func TestAuthMiddlewareServesSPAShellWhenGated(t *testing.T) {
	t.Parallel()
	gate := &fakeAuthGate{cfg: auth.Config{Mode: auth.ModeOn}, validToken: "good-token"}

	// SPA shell, assets, and client-side deep routes pass ungated (no cookie, public peer).
	for _, path := range []string{"/", "/index.html", "/assets/app.js", "/series/123"} {
		r := httptest.NewRequest(http.MethodGet, path, http.NoBody)
		r.RemoteAddr = "203.0.113.7:5555" // public, no cookie
		_, called := serve(t, gate, false, r)
		require.True(t, called, "%s must load ungated so the in-app login screen is reachable", path)
	}

	// The real data boundary stays gated.
	rAPI := httptest.NewRequest(http.MethodPost, "/api/omnibus.v1.SeriesService/ListSeries", http.NoBody)
	rAPI.RemoteAddr = "203.0.113.7:5555"
	recAPI, calledAPI := serve(t, gate, false, rAPI)
	require.False(t, calledAPI, "a protected /api RPC stays gated while the shell is ungated")
	require.Equal(t, http.StatusUnauthorized, recAPI.Code)

	rCov := httptest.NewRequest(http.MethodGet, "/covers/123.jpg", http.NoBody)
	rCov.RemoteAddr = "203.0.113.7:5555"
	recCov, calledCov := serve(t, gate, false, rCov)
	require.False(t, calledCov, "a /covers blob stays gated while the shell is ungated")
	require.Equal(t, http.StatusUnauthorized, recCov.Code)
}

// TestAuthMiddlewareAllowsAuthServiceWhenGated asserts the AuthService endpoints stay
// reachable even with auth On and no session — otherwise a gated instance could never be
// logged into (Login/GetAuthConfig must work).
func TestAuthMiddlewareAllowsAuthServiceWhenGated(t *testing.T) {
	t.Parallel()
	gate := &fakeAuthGate{cfg: auth.Config{Mode: auth.ModeOn}, validToken: "good-token"}

	for _, path := range []string{
		"/api/omnibus.v1.AuthService/Login",
		"/api/omnibus.v1.AuthService/GetAuthConfig",
		"/api/omnibus.v1.AuthService/Logout",
	} {
		r := httptest.NewRequest(http.MethodPost, path, http.NoBody)
		r.RemoteAddr = "203.0.113.7:5555" // public, no cookie
		_, called := serve(t, gate, false, r)
		require.True(t, called, "%s must be reachable when gated (login must work)", path)
	}

	// A non-AuthService /api path is still gated.
	r := httptest.NewRequest(http.MethodPost, "/api/omnibus.v1.SeriesService/ListSeries", http.NoBody)
	r.RemoteAddr = "203.0.113.7:5555"
	rec, called := serve(t, gate, false, r)
	require.False(t, called, "a protected /api path is still gated")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestAuthMiddlewareOnValidSessionPasses asserts a valid signed cookie passes the gate on a
// GATED data RPC (the SPA shell is exempt, so it must be exercised on a real /api path).
func TestAuthMiddlewareOnValidSessionPasses(t *testing.T) {
	t.Parallel()
	gate := &fakeAuthGate{cfg: auth.Config{Mode: auth.ModeOn}, validToken: "good-token"}
	r := httptest.NewRequest(http.MethodPost, "/api/omnibus.v1.SeriesService/ListSeries", http.NoBody)
	r.RemoteAddr = "203.0.113.7:5555"
	r.AddCookie(&http.Cookie{Name: "omnibus_session", Value: "good-token"})

	rec, called := serve(t, gate, false, r)
	require.True(t, called, "a valid session passes")
	require.Equal(t, http.StatusOK, rec.Code)
}

// TestAuthMiddlewareLocalBypass asserts that in BypassLocal mode a loopback/RFC1918 peer
// (real RemoteAddr) bypasses the gate, while a public peer is still gated.
func TestAuthMiddlewareLocalBypass(t *testing.T) {
	t.Parallel()
	gate := &fakeAuthGate{cfg: auth.Config{Mode: auth.ModeBypassLocal}, validToken: "good-token"}

	// Exercise a GATED data RPC so the local-bypass decision (not the SPA shell exemption)
	// is what lets the request through.
	for _, addr := range []string{"127.0.0.1:5555", "10.1.2.3:5555", "192.168.1.50:5555", "172.16.5.5:5555"} {
		r := httptest.NewRequest(http.MethodPost, "/api/omnibus.v1.SeriesService/ListSeries", http.NoBody)
		r.RemoteAddr = addr // no cookie — local peer bypasses anyway
		_, called := serve(t, gate, false, r)
		require.True(t, called, "local peer %s bypasses the gate", addr)
	}

	// A public peer with no session is still gated.
	rPub := httptest.NewRequest(http.MethodPost, "/api/omnibus.v1.SeriesService/ListSeries", http.NoBody)
	rPub.RemoteAddr = "203.0.113.7:5555"
	recPub, calledPub := serve(t, gate, false, rPub)
	require.False(t, calledPub, "a remote peer is still gated in BypassLocal mode")
	require.Equal(t, http.StatusUnauthorized, recPub.Code)
}

// TestAuthMiddlewareIgnoresXFF asserts a spoofed X-Forwarded-For claiming a local IP does
// NOT trigger the bypass when the real RemoteAddr is public and no trusted proxy is set.
func TestAuthMiddlewareIgnoresXFF(t *testing.T) {
	t.Parallel()
	gate := &fakeAuthGate{cfg: auth.Config{Mode: auth.ModeBypassLocal}, validToken: "good-token"}

	r := httptest.NewRequest(http.MethodPost, "/api/omnibus.v1.SeriesService/ListSeries", http.NoBody)
	r.RemoteAddr = "203.0.113.7:5555"            // real socket peer is public
	r.Header.Set("X-Forwarded-For", "127.0.0.1") // spoofed claim of localhost

	// No trusted proxy, so the spoofed header must be ignored.
	rec, called := serve(t, gate, false, r)
	require.False(t, called, "a spoofed XFF cannot forge a local bypass")
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	// When a trusted proxy IS configured, the first XFF entry is honored.
	r2 := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r2.RemoteAddr = "203.0.113.7:5555"
	r2.Header.Set("X-Forwarded-For", "10.0.0.5")
	// A trusted proxy is configured, so the first XFF entry is honored.
	_, called2 := serve(t, gate, true, r2)
	require.True(t, called2, "behind a trusted proxy, a local XFF triggers the bypass")
}
