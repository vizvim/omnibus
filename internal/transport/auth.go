package transport

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"connectrpc.com/connect"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/auth"
)

// sessionCookieName is the name of the signed session cookie the Login handler sets and
// the gate middleware reads. Kept here (transport) as the single source of truth.
const sessionCookieName = "omnibus_session"

// sessionMaxAge bounds the session cookie's lifetime (30 days), matching the token TTL so
// the browser drops the cookie around the same time the server stops honoring it.
const sessionMaxAge = 60 * 60 * 24 * 30

// errGenericAuth is the single combined credential error returned for ANY login failure,
// so a caller cannot distinguish "no such user" from "wrong password" (no
// user-enumeration oracle, D-04).
var errGenericAuth = errors.New("incorrect username or password")

// AuthServicer is the subset of auth.Service the handler depends on. The transport layer
// depends only on the service package, never on repository/db.
type AuthServicer interface {
	GetConfig(ctx context.Context) (auth.Config, error)
	UpdateConfig(ctx context.Context, in auth.Input) (auth.Config, error)
	VerifyCredentials(ctx context.Context, username, password string) (bool, error)
	SignSession(username string) string
}

// AuthHandler implements the generated AuthServiceHandler over the service.
type AuthHandler struct {
	omnibusv1connect.UnimplementedAuthServiceHandler
	svc AuthServicer
	// trustProxy mirrors the gate's policy: only when the operator vouches for the proxy
	// chain do we honor X-Forwarded-Proto to decide the cookie's Secure flag (WR-02),
	// consistent with the X-Forwarded-For trust policy in the middleware.
	trustProxy bool
}

// NewAuthHandler builds the AuthService Connect handler. trustProxy controls whether the
// forwarded X-Forwarded-Proto header is honored when deciding the session cookie's Secure
// flag (see secureCookie).
func NewAuthHandler(svc AuthServicer, trustProxy bool) *AuthHandler {
	return &AuthHandler{svc: svc, trustProxy: trustProxy}
}

// GetAuthConfig handles the get RPC. On a fresh DB the service returns the D-01 default
// (Off, not configured). The response carries `configured` but never the password hash.
func (h *AuthHandler) GetAuthConfig(
	ctx context.Context, _ *connect.Request[omnibusv1.GetAuthConfigRequest],
) (*connect.Response[omnibusv1.GetAuthConfigResponse], error) {
	cfg, err := h.svc.GetConfig(ctx)
	if err != nil {
		return nil, authServiceError(err)
	}
	return connect.NewResponse(&omnibusv1.GetAuthConfigResponse{Config: authConfigToProto(cfg)}), nil
}

// UpdateAuthConfig handles the update RPC. An empty password keeps the stored hash;
// enabling auth without a stored credential is surfaced as InvalidArgument (fail-closed).
func (h *AuthHandler) UpdateAuthConfig(
	ctx context.Context, req *connect.Request[omnibusv1.UpdateAuthConfigRequest],
) (*connect.Response[omnibusv1.UpdateAuthConfigResponse], error) {
	m := req.Msg
	cfg, err := h.svc.UpdateConfig(ctx, auth.Input{
		Mode:     modeFromProto(m.GetMode()),
		Username: m.GetUsername(),
		Password: m.GetPassword(),
	})
	if err != nil {
		return nil, authServiceError(err)
	}
	return connect.NewResponse(&omnibusv1.UpdateAuthConfigResponse{Config: authConfigToProto(cfg)}), nil
}

// Login validates the credentials and, on success, sets the signed, httpOnly, SameSite
// session cookie directly on the response header (Pattern 2 — no interceptor needed to
// SET the cookie). Any failure returns Unauthenticated with the single generic message.
func (h *AuthHandler) Login(
	ctx context.Context, req *connect.Request[omnibusv1.LoginRequest],
) (*connect.Response[omnibusv1.LoginResponse], error) {
	ok, err := h.svc.VerifyCredentials(ctx, req.Msg.GetUsername(), req.Msg.GetPassword())
	if err != nil || !ok {
		// Fail-closed: a verify error is treated identically to bad creds — no oracle.
		return nil, connect.NewError(connect.CodeUnauthenticated, errGenericAuth)
	}

	resp := connect.NewResponse(&omnibusv1.LoginResponse{})
	cookie := &http.Cookie{ //nolint:gosec // G124: Secure is set conditionally (secureCookie) — true over HTTPS, false on plain-HTTP LAN self-hosts by design (WR-02); HttpOnly+SameSite=Lax always set.
		Name:     sessionCookieName,
		Value:    h.svc.SignSession(req.Msg.GetUsername()),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Secure is set conditionally (WR-02): when the request arrived over HTTPS we mark
		// the cookie Secure so it can never leak over an accidental plaintext request. On a
		// plain-HTTP LAN install (no TLS, no trusted X-Forwarded-Proto) Secure stays false so
		// login keeps working — an unconditional Secure flag silently breaks plain-HTTP
		// self-hosts (Anti-Patterns, 06-RESEARCH.md). HttpOnly + SameSite=Lax carry the
		// XSS/CSRF defenses regardless.
		Secure: h.secureCookie(req),
		MaxAge: sessionMaxAge,
	}
	resp.Header().Set("Set-Cookie", cookie.String())
	return resp, nil
}

// secureCookie reports whether the session cookie should carry the Secure flag for this
// request — i.e. whether the connection is HTTPS. It honors X-Forwarded-Proto == "https"
// only when the handler is configured to trust the proxy (trustProxy), mirroring the gate's
// X-Forwarded-For trust policy so an untrusted client cannot influence the flag. Plain-HTTP
// local dev (no trusted forwarded-proto) yields false so login still works.
func (h *AuthHandler) secureCookie(req interface{ Header() http.Header }) bool {
	if h.trustProxy && strings.EqualFold(req.Header().Get("X-Forwarded-Proto"), "https") {
		return true
	}
	return false
}

// Logout clears the session cookie (MaxAge < 0 expires it immediately). The expiring cookie
// carries the same Secure attribute as the one Login set (WR-02) so the browser reliably
// matches and clears it on an HTTPS deployment.
func (h *AuthHandler) Logout(
	_ context.Context, req *connect.Request[omnibusv1.LogoutRequest],
) (*connect.Response[omnibusv1.LogoutResponse], error) {
	resp := connect.NewResponse(&omnibusv1.LogoutResponse{})
	cookie := &http.Cookie{ //nolint:gosec // G124: Secure mirrors Login's conditional value (secureCookie); this expires the cookie (MaxAge<0). HttpOnly+SameSite=Lax always set.
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.secureCookie(req),
		MaxAge:   -1,
	}
	resp.Header().Set("Set-Cookie", cookie.String())
	return resp, nil
}

// authConfigToProto maps the domain READ Config to its proto view. The password hash is
// never present in Config, so it cannot leak here — only `configured` is emitted.
func authConfigToProto(c auth.Config) *omnibusv1.AuthConfig {
	return &omnibusv1.AuthConfig{
		Mode:       modeToProto(c.Mode),
		Username:   c.Username,
		Configured: c.Configured,
	}
}

// modeFromProto maps the AuthMode proto enum to the domain mode string.
func modeFromProto(m omnibusv1.AuthMode) string {
	switch m {
	case omnibusv1.AuthMode_AUTH_MODE_ON:
		return auth.ModeOn
	case omnibusv1.AuthMode_AUTH_MODE_BYPASS_LOCAL:
		return auth.ModeBypassLocal
	case omnibusv1.AuthMode_AUTH_MODE_OFF, omnibusv1.AuthMode_AUTH_MODE_UNSPECIFIED:
		return auth.ModeOff
	default:
		return auth.ModeOff
	}
}

// modeToProto maps the domain mode string to the AuthMode proto enum.
func modeToProto(m string) omnibusv1.AuthMode {
	switch m {
	case auth.ModeOn:
		return omnibusv1.AuthMode_AUTH_MODE_ON
	case auth.ModeBypassLocal:
		return omnibusv1.AuthMode_AUTH_MODE_BYPASS_LOCAL
	default:
		return omnibusv1.AuthMode_AUTH_MODE_OFF
	}
}

// authServiceError maps validation/misconfig errors to InvalidArgument and the rest to
// internal.
func authServiceError(err error) error {
	if errors.Is(err, auth.ErrMissingCredential) || errors.Is(err, auth.ErrMissingUsername) {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}
