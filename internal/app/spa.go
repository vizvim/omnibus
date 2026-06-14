package app

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/vizvim/omnibus/frontend"
)

// placeholderIndex is served as the SPA shell when the embedded build has no index.html
// (an empty/dev build). It keeps the binary from panicking at startup or 500ing on "/"
// before a real `vite build` is embedded — the production build overwrites it.
const placeholderIndex = `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
	`<title>omnibus</title></head><body><div id="root">omnibus — UI build not embedded ` +
	`(run vite build)</div></body></html>`

// NewSPAHandler serves the embedded React SPA: real static assets (JS/CSS/images) are
// served directly from the embedded FS, and every other path falls back to index.html so
// client-side routes (/settings, /library/...) load the app shell. It is mounted as the
// "/" catch-all AFTER the /api and /covers handlers so it never shadows them.
//
// Security posture (T-6-01): the handler resolves paths only against the embedded FS via
// fs.Open — there is no request-driven filesystem path resolution, so "../" traversal
// cannot escape the embedded tree (a cleaned request path that misses an embedded asset
// simply falls back to index.html, never to an arbitrary file). It mirrors cover.go's
// whitelist-then-fallback closure idiom.
func NewSPAHandler() http.Handler {
	dist, err := frontend.Dist()
	if err != nil {
		// A malformed embed is a build-time invariant; degrade to placeholder-only rather
		// than panicking at package/handler init (Environment Availability: tolerate an
		// empty/dev build).
		dist = nil
	}

	hasIndex := hasEmbeddedIndex(dist)
	fileServer := http.FileServerFS(dist)

	serveIndex := func(w http.ResponseWriter, r *http.Request) {
		if hasIndex {
			// Re-route to "/" so the file server returns the embedded index.html.
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The placeholder shell is a static HTML constant; a write failure (client
		// disconnect) is unrecoverable here, so the checked error is intentionally dropped.
		if _, err := w.Write([]byte(placeholderIndex)); err != nil {
			return
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// "/" always serves the shell.
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			serveIndex(w, r)
			return
		}

		// Serve a real embedded asset if it exists; otherwise fall back to index.html so
		// deep client routes load the SPA shell (the canonical SPA-fallback behavior).
		if dist != nil && assetExists(dist, clean) {
			fileServer.ServeHTTP(w, r)
			return
		}
		serveIndex(w, r)
	})
}

// hasEmbeddedIndex reports whether the embedded build contains an index.html shell.
func hasEmbeddedIndex(dist fs.FS) bool {
	if dist == nil {
		return false
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return false
	}
	return true
}

// assetExists reports whether name is a regular file in the embedded build. Directories
// are treated as non-assets so they fall through to the index.html SPA fallback.
func assetExists(dist fs.FS, name string) bool {
	info, err := fs.Stat(dist, name)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
