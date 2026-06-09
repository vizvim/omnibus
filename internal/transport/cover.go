package transport

import (
	"context"
	"net/http"
	"strconv"
	"strings"
)

// coverPrefix is the URL prefix the cover handler is mounted at.
const coverPrefix = "/covers/"

// CoverStore is the covers source the handler reads from. It is owned by the transport
// layer so transport never imports the repository package (PLAT-05); the app layer
// satisfies it with the covers repository.
type CoverStore interface {
	Get(ctx context.Context, entityType string, id int64) (image []byte, contentType string, found bool, err error)
}

// NewCoverHandler serves cover blobs from the store, e.g. /covers/series/1.jpg and
// /covers/issues/42.jpg (D-07). Because kind is whitelisted and id is parsed as a
// positive integer, no path-traversal input can reach the store (replaces the old
// FileServer confinement guard, V12 / T-02-04-PATH).
func NewCoverHandler(store CoverStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		rest := strings.TrimPrefix(r.URL.Path, coverPrefix)
		parts := strings.Split(rest, "/")
		if len(parts) != 2 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		kind := parts[0]
		if kind != "series" && kind != "issues" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		idStr, ok := strings.CutSuffix(parts[1], ".jpg")
		if !ok {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		image, contentType, found, err := store.Get(r.Context(), kind, id)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !found {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", contentType)
		// Covers are immutable per (kind,id) within a refresh window; allow client caching
		// (carried over from the prior FileServer's implicit caching).
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(image)
	})
}
