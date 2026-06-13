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

// NewAuthMiddleware wraps next with the single fail-closed optional-auth gate (AUTH-01,
// ADR 0008). It is the ONLY enforcement seam and uniformly covers everything in the mux —
// /api, /covers, the embedded SPA, and the long-lived stream — because it sits as an
// http.Handler in front of the whole mux (a Connect interceptor could see neither the SPA
// nor the raw socket peer, 06-RESEARCH.md Architectural Responsibility Map).
//
// Decision order (Pattern 3): (1) load config; on ANY load error -> 401 (fail-closed);
// if Off -> pass; (2) if BypassLocal and the REAL peer IP is loopback/RFC1918 -> pass;
// (3) validate the signed session cookie -> pass on valid, else 401. No code path returns
// without either calling next (pass) or writing a 401 (deny) — there is no fail-open.
func NewAuthMiddleware(gate AuthGate, trustProxy bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
// real socket peer (r.RemoteAddr) by default — the only non-spoofable signal. The first
// X-Forwarded-For entry is read ONLY when trustProxy is explicitly configured (the
// operator vouches for the proxy chain); otherwise a client-supplied XFF is ignored so a
// spoofed header cannot forge a local bypass (D-05, T-6-11).
func realClientIP(r *http.Request, trustProxy bool) net.IP {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first := strings.TrimSpace(strings.Split(xff, ",")[0])
			if ip := net.ParseIP(first); ip != nil {
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
