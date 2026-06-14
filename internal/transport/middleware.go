package transport

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/vizvim/omnibus/internal/auth"
)

// AuthGate is the subset of auth.Service the middleware depends on. The transport layer
// depends only on the service package, never on repository/db. It loads the current auth
// config (to decide whether/how to gate) and verifies a signed session token.
type AuthGate interface {
	GetConfig(ctx context.Context) (auth.Config, error)
	VerifySession(token string) (string, error)
}

// authServicePathPrefix is the AuthService mount prefix that MUST stay reachable even when
// the gate is On with no session — otherwise a user could never log in (Login) nor learn
// the gate is enabled (GetAuthConfig). This is the one deliberate, narrow exemption to the
// otherwise-uniform gate; it exposes only credential verification (which fails closed on
// its own) and the read config (which never returns the hash), not any protected resource.
const authServicePathPrefix = "/api/omnibus.v1.AuthService/"

// Gated data namespaces — the REAL security boundary. Everything under these prefixes stays
// fully gated in every mode. /api is the Connect data RPC surface; /covers serves cover
// image blobs (protected data). The SPA shell exemption below deliberately does NOT cover
// these.
const (
	apiPathPrefix    = "/api/"
	coversPathPrefix = "/covers/"
)

// isSPAShellRequest reports whether r is a safe navigation/asset request for the embedded
// SPA shell (the React bundle, its static assets, and client-side deep routes) rather than
// a request for protected data. Only idempotent GET/HEAD requests that are NOT under the
// gated data namespaces (/api, /covers) qualify.
func isSPAShellRequest(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	p := r.URL.Path
	return !strings.HasPrefix(p, apiPathPrefix) && !strings.HasPrefix(p, coversPathPrefix)
}

// NewAuthMiddleware wraps next with the single fail-closed optional-auth gate (AUTH-01,
// ADR 0008). It is the ONLY enforcement seam for the data boundary (the /api Connect RPCs,
// the long-lived stream, and /covers blobs) because it sits as an http.Handler in front of
// the whole mux (a Connect interceptor could see neither the SPA nor the raw socket peer,
// 06-RESEARCH.md Architectural Responsibility Map). Two requests fall through ungated: the
// AuthService itself (Login/GetAuthConfig/Logout, so the gate is unlock-able) and the
// static SPA shell + assets (so the in-app login screen can load). The SPA shell carries no
// protected data; the real boundary is the gated /api and /covers namespaces.
//
// Decision order (Pattern 3): (0) let the AuthService through (login must work); (0b) let
// the static SPA shell/assets through (safe GET/HEAD outside /api and /covers) so the login
// UI can load even when gated; (1) load config; on ANY load error -> 401 (fail-closed); if
// Off -> pass; (2) if BypassLocal and the REAL peer IP is loopback/RFC1918 -> pass; (3)
// validate the signed session cookie -> pass on valid, else 401. No code path returns
// without either calling next (pass) or writing a 401 (deny) — there is no fail-open on the
// gated data namespaces.
func NewAuthMiddleware(gate AuthGate, trustProxy bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The AuthService endpoints must always be reachable so a gated instance can be
		// logged into; they are credential-verifying/read-only and fail closed themselves.
		if strings.HasPrefix(r.URL.Path, authServicePathPrefix) {
			next.ServeHTTP(w, r)
			return
		}

		// SPA shell exemption (standard SPA-auth pattern): the static React shell, its
		// JS/CSS assets, and client-side deep routes (any safe GET/HEAD request NOT under
		// the gated data namespaces) load ungated in every mode, so the in-app login screen
		// — which is rendered INSIDE the React app — is reachable on a fresh load / new tab
		// / expired cookie. The shell carries no protected data (HTML/JS/CSS only); the data
		// RPCs (/api) and cover images (/covers) below remain the enforced security
		// boundary. The frontend gate is purely defense-in-depth on top of that boundary.
		if isSPAShellRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		cfg, err := gate.GetConfig(r.Context())
		if err != nil {
			// Fail-closed: we cannot determine the auth posture, so deny (D-04).
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Auth disabled (default): every request passes ungated.
		if cfg.Mode == auth.ModeOff {
			next.ServeHTTP(w, r)
			return
		}

		// Local-bypass: only on the real, non-spoofable socket peer (X-Forwarded-For is
		// honored only behind an explicitly trusted proxy, D-05).
		if cfg.Mode == auth.ModeBypassLocal && isLocal(realClientIP(r, trustProxy)) {
			next.ServeHTTP(w, r)
			return
		}

		// Mode On (or BypassLocal for a remote peer): require a valid signed session.
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if _, err := gate.VerifySession(cookie.Value); err != nil {
			// Tampered, expired, or malformed token -> deny (fail-closed).
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// realClientIP resolves the request's client IP for the local-bypass decision. It uses the
// real socket peer (r.RemoteAddr) by default — the only non-spoofable signal. The
// X-Forwarded-For header is read ONLY when trustProxy is explicitly configured (the operator
// vouches for the proxy chain); otherwise a client-supplied XFF is ignored so a spoofed
// header cannot forge a local bypass (D-05, T-6-11).
//
// When trustProxy is set we trust the RIGHT-MOST parseable XFF entry — the address the
// trusted reverse proxy itself observed and appended — NOT the left-most entry. The
// left-most entry is the value the original client sent and is fully client-spoofable: a
// remote attacker could otherwise send "X-Forwarded-For: 127.0.0.1" to forge a local bypass
// (WR-01). This assumes the single trusted proxy appends the real peer it saw (the standard
// reverse-proxy behavior).
func realClientIP(r *http.Request, trustProxy bool) net.IP {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			last := strings.TrimSpace(parts[len(parts)-1])
			if ip := net.ParseIP(last); ip != nil {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr may be a bare IP (no port) in some test/transport setups.
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

// isLocal reports whether ip is a loopback or private (RFC1918) address — the "local
// network" set the bypass applies to (D-05). net.IP.IsPrivate (Go 1.17+) covers the three
// RFC1918 IPv4 ranges plus IPv6 ULA (fc00::/7); matching ULA as "local" is acceptable for
// a self-hosted LAN appliance.
func isLocal(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	return ip.IsPrivate()
}
