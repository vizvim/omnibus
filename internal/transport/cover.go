package transport

import (
	"net/http"
	"path/filepath"
)

// coverPrefix is the URL prefix the cover handler is mounted at.
const coverPrefix = "/covers/"

// NewCoverHandler serves locally-stored cover art from <dataPath>/covers, e.g.
// /covers/series/1.jpg and /covers/issues/42.jpg (D-07). It serves only files inside
// the fixed cover directory; path-traversal attempts are rejected by http.FileServer's
// cleaning plus the StripPrefix + Dir confinement (V12 / T-02-04-PATH).
func NewCoverHandler(dataPath string) http.Handler {
	coverDir := filepath.Join(dataPath, "covers")
	// http.Dir + ServeFile reject ".." escapes; http.FileServer also cleans the path
	// and confines reads to coverDir.
	fileServer := http.FileServer(http.Dir(coverDir))
	return http.StripPrefix(coverPrefix, fileServer)
}
