// Package frontend embeds the built React SPA (frontend/dist) into the Go binary so
// the server can serve the UI from a single static binary (D-07: the auth gate must
// cover the served SPA, which first requires Go to serve it).
//
// The embed declaration lives here, colocated with frontend/dist, because a //go:embed
// pattern cannot reference a parent directory (no "..") — so it cannot be declared from
// internal/app. internal/app/spa.go consumes Dist() to build the HTTP handler.
//
// In a clean checkout / dev build, frontend/dist holds only the committed placeholder
// index.html (a real `vite build` overwrites the directory with the production assets,
// which stay gitignored). The SPA handler tolerates an empty/dev build, so the binary
// never panics when the real build is absent.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed dist
var distFS embed.FS

// Dist returns the embedded built SPA rooted at the dist directory (so paths are
// "index.html", "assets/...", not "dist/index.html"). The error is non-nil only if the
// embedded tree is malformed, which is a compile-time-fixed invariant.
func Dist() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
