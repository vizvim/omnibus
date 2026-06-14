package transport_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/auth"
	"github.com/vizvim/omnibus/internal/transport"
)

// fakeAuthServicer is a hand-rolled stub of the servicer the handler depends on.
type fakeAuthServicer struct {
	getResult    auth.Config
	updateResult auth.Config
	updateErr    error

	verifyOK  bool
	verifyErr error

	signedToken string
	gotInput    auth.Input
}

func (f *fakeAuthServicer) GetConfig(context.Context) (auth.Config, error) {
	return f.getResult, nil
}

func (f *fakeAuthServicer) UpdateConfig(_ context.Context, in auth.Input) (auth.Config, error) {
	f.gotInput = in
	return f.updateResult, f.updateErr
}

func (f *fakeAuthServicer) VerifyCredentials(context.Context, string, string) (bool, error) {
	return f.verifyOK, f.verifyErr
}

func (f *fakeAuthServicer) SignSession(string) string {
	if f.signedToken == "" {
		return "signed.token.value"
	}
	return f.signedToken
}

func newAuthTestClient(t *testing.T, svc transport.AuthServicer) omnibusv1connect.AuthServiceClient {
	t.Helper()
	return newAuthTestClientTrustProxy(t, svc, false)
}

func newAuthTestClientTrustProxy(
	t *testing.T, svc transport.AuthServicer, trustProxy bool,
) omnibusv1connect.AuthServiceClient {
	t.Helper()
	handler := transport.NewAuthHandler(svc, trustProxy)
	path, h := omnibusv1connect.NewAuthServiceHandler(handler)

	mux := http.NewServeMux()
	mux.Handle(path, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return omnibusv1connect.NewAuthServiceClient(srv.Client(), srv.URL)
}

// TestAuthLoginSetsCookie asserts a successful Login sets a signed, httpOnly, SameSite
// session cookie on the response.
func TestAuthLoginSetsCookie(t *testing.T) {
	t.Parallel()
	client := newAuthTestClient(t, &fakeAuthServicer{verifyOK: true, signedToken: "abc.123.def"})

	resp, err := client.Login(context.Background(),
		connect.NewRequest(&omnibusv1.LoginRequest{Username: "admin", Password: "pw"}))
	require.NoError(t, err)

	setCookie := resp.Header().Get("Set-Cookie")
	require.NotEmpty(t, setCookie, "Login sets a session cookie")
	require.Contains(t, setCookie, "omnibus_session=abc.123.def")
	require.Contains(t, strings.ToLower(setCookie), "httponly")
	require.Contains(t, strings.ToLower(setCookie), "samesite=lax")
}

// TestAuthLoginCookieSecureOnHTTPS asserts the session cookie carries the Secure flag when
// the request arrived over HTTPS via a trusted proxy (X-Forwarded-Proto: https), so it
// cannot leak over accidental plaintext (WR-02).
func TestAuthLoginCookieSecureOnHTTPS(t *testing.T) {
	t.Parallel()
	client := newAuthTestClientTrustProxy(t, &fakeAuthServicer{verifyOK: true}, true)

	req := connect.NewRequest(&omnibusv1.LoginRequest{Username: "admin", Password: "pw"})
	req.Header().Set("X-Forwarded-Proto", "https")
	resp, err := client.Login(context.Background(), req)
	require.NoError(t, err)
	require.Contains(t, strings.ToLower(resp.Header().Get("Set-Cookie")), "secure")
}

// TestAuthLoginCookieNotSecureOnPlainHTTP asserts the cookie omits Secure on a plain-HTTP
// install (no TLS, no trusted forwarded-proto) so login still works on a LAN (WR-02).
func TestAuthLoginCookieNotSecureOnPlainHTTP(t *testing.T) {
	t.Parallel()
	client := newAuthTestClient(t, &fakeAuthServicer{verifyOK: true})

	resp, err := client.Login(context.Background(),
		connect.NewRequest(&omnibusv1.LoginRequest{Username: "admin", Password: "pw"}))
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(resp.Header().Get("Set-Cookie")), "secure")
}

// TestAuthLoginCookieSecureIgnoresUntrustedProto asserts that without trustProxy a
// client-supplied X-Forwarded-Proto: https does NOT make the cookie Secure (a client cannot
// influence the flag), mirroring the gate's X-Forwarded-For trust policy (WR-02).
func TestAuthLoginCookieSecureIgnoresUntrustedProto(t *testing.T) {
	t.Parallel()
	// newAuthTestClient uses trustProxy=false, so an untrusted forwarded-proto is ignored.
	client := newAuthTestClient(t, &fakeAuthServicer{verifyOK: true})

	req := connect.NewRequest(&omnibusv1.LoginRequest{Username: "admin", Password: "pw"})
	req.Header().Set("X-Forwarded-Proto", "https")
	resp, err := client.Login(context.Background(), req)
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(resp.Header().Get("Set-Cookie")), "secure")
}

// TestAuthLoginBadCredsGenericError asserts bad credentials return Unauthenticated with a
// single generic message (no user/password-existence oracle) and set no cookie.
func TestAuthLoginBadCredsGenericError(t *testing.T) {
	t.Parallel()
	client := newAuthTestClient(t, &fakeAuthServicer{verifyOK: false})

	resp, err := client.Login(context.Background(),
		connect.NewRequest(&omnibusv1.LoginRequest{Username: "admin", Password: "wrong"}))
	require.Nil(t, resp)
	require.Error(t, err)
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	require.Contains(t, err.Error(), "incorrect username or password")
}

// TestAuthLoginVerifyErrorFailsClosed asserts a verify error is treated identically to bad
// creds — Unauthenticated, no oracle, no cookie (fail-closed, D-04).
func TestAuthLoginVerifyErrorFailsClosed(t *testing.T) {
	t.Parallel()
	client := newAuthTestClient(t, &fakeAuthServicer{verifyErr: context.DeadlineExceeded})

	_, err := client.Login(context.Background(),
		connect.NewRequest(&omnibusv1.LoginRequest{Username: "admin", Password: "pw"}))
	require.Error(t, err)
	require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

// TestGetAuthConfigNeverReturnsHash asserts the read view carries `configured` but exposes
// no password/hash field at all.
func TestGetAuthConfigNeverReturnsHash(t *testing.T) {
	t.Parallel()
	client := newAuthTestClient(t, &fakeAuthServicer{
		getResult: auth.Config{Mode: auth.ModeOn, Username: "admin", Configured: true},
	})

	resp, err := client.GetAuthConfig(context.Background(),
		connect.NewRequest(&omnibusv1.GetAuthConfigRequest{}))
	require.NoError(t, err)
	cfg := resp.Msg.GetConfig()
	require.Equal(t, omnibusv1.AuthMode_AUTH_MODE_ON, cfg.GetMode())
	require.Equal(t, "admin", cfg.GetUsername())
	require.True(t, cfg.GetConfigured())
	// The AuthConfig proto has no password/hash accessor — the read view cannot leak it.
}

// TestUpdateAuthConfigMapsInput asserts the handler maps the proto request faithfully into
// the service Input (including the write-only password and the AuthMode enum).
func TestUpdateAuthConfigMapsInput(t *testing.T) {
	t.Parallel()
	fake := &fakeAuthServicer{updateResult: auth.Config{Mode: auth.ModeOn, Username: "admin", Configured: true}}
	client := newAuthTestClient(t, fake)

	_, err := client.UpdateAuthConfig(context.Background(),
		connect.NewRequest(&omnibusv1.UpdateAuthConfigRequest{
			Mode:     omnibusv1.AuthMode_AUTH_MODE_ON,
			Username: "admin",
			Password: "new-password",
		}))
	require.NoError(t, err)
	require.Equal(t, auth.ModeOn, fake.gotInput.Mode)
	require.Equal(t, "admin", fake.gotInput.Username)
	require.Equal(t, "new-password", fake.gotInput.Password)
}

// TestUpdateAuthConfigRejectsUnknownMode asserts an unspecified/unknown AuthMode is rejected
// with InvalidArgument rather than silently coerced to Off (which would disable auth on a
// malformed write, WR-04). The service must never be called for a rejected mode.
func TestUpdateAuthConfigRejectsUnknownMode(t *testing.T) {
	t.Parallel()
	for _, mode := range []omnibusv1.AuthMode{
		omnibusv1.AuthMode_AUTH_MODE_UNSPECIFIED,
		omnibusv1.AuthMode(99), // out-of-range / corrupted value
	} {
		fake := &fakeAuthServicer{}
		client := newAuthTestClient(t, fake)

		_, err := client.UpdateAuthConfig(context.Background(),
			connect.NewRequest(&omnibusv1.UpdateAuthConfigRequest{Mode: mode, Username: "admin"}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		require.Equal(t, auth.Input{}, fake.gotInput, "service must not be called for a rejected mode")
	}
}

// TestUpdateAuthConfigAllowsOff asserts a legitimate Off mode remains settable (it is a known
// mode, not an unknown one — WR-04 must not over-reject).
func TestUpdateAuthConfigAllowsOff(t *testing.T) {
	t.Parallel()
	fake := &fakeAuthServicer{updateResult: auth.Config{Mode: auth.ModeOff}}
	client := newAuthTestClient(t, fake)

	_, err := client.UpdateAuthConfig(context.Background(),
		connect.NewRequest(&omnibusv1.UpdateAuthConfigRequest{Mode: omnibusv1.AuthMode_AUTH_MODE_OFF}))
	require.NoError(t, err)
	require.Equal(t, auth.ModeOff, fake.gotInput.Mode)
}

// TestUpdateAuthConfigMisconfigIsInvalidArgument asserts the fail-closed misconfig error
// surfaces as InvalidArgument.
func TestUpdateAuthConfigMisconfigIsInvalidArgument(t *testing.T) {
	t.Parallel()
	client := newAuthTestClient(t, &fakeAuthServicer{updateErr: auth.ErrMissingCredential})

	_, err := client.UpdateAuthConfig(context.Background(),
		connect.NewRequest(&omnibusv1.UpdateAuthConfigRequest{Mode: omnibusv1.AuthMode_AUTH_MODE_ON}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// TestAuthLogoutClearsCookie asserts Logout sets an expiring cookie.
func TestAuthLogoutClearsCookie(t *testing.T) {
	t.Parallel()
	client := newAuthTestClient(t, &fakeAuthServicer{})

	resp, err := client.Logout(context.Background(),
		connect.NewRequest(&omnibusv1.LogoutRequest{}))
	require.NoError(t, err)
	setCookie := resp.Header().Get("Set-Cookie")
	require.Contains(t, setCookie, "omnibus_session=")
	require.Contains(t, strings.ToLower(setCookie), "max-age=0")
}
