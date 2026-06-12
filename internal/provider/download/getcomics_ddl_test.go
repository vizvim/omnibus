package download_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/provider/download"
)

// fixturePostHTML returns the captured GetComics post-page fixture, with each mirror anchor's
// href rewritten to point at base so the resolver yields URLs the test server serves.
func fixturePostHTML(t *testing.T, base string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "getcomics_post.html"))
	require.NoError(t, err)
	html := string(raw)
	// Rewrite the two primary mirror hosts to the test server so a "Download Now"/"Mirror
	// Download" anchor resolves to a streamable URL; leave the cloud-host mirrors pointing at
	// their (unreachable) real hosts so preference-ordering is exercised.
	html = strings.ReplaceAll(html, "https://dl1.example-mirror.test", base+"/dl1")
	html = strings.ReplaceAll(html, "https://dl2.example-mirror.test", base+"/dl2")
	return html
}

// TestDDLResolveAndFetch resolves a mirror from the captured fixture and streams the file from
// an httptest server that sets Content-Length, asserting the file lands under <data_path>/
// incomplete and progress is reported.
func TestDDLResolveAndFetch(t *testing.T) {
	t.Parallel()

	const payload = "PK\x03\x04 this-is-a-fake-cbz-archive-body"

	mux := http.NewServeMux()
	mux.HandleFunc("/dl1/download/example-comic-1.cbz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write([]byte(payload))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// The post page is served at /post and links to the (rewritten) mirror URLs.
	postHTML := fixturePostHTML(t, srv.URL)
	mux.HandleFunc("/post", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(postHTML))
	})

	dataPath := t.TempDir()
	p := download.NewGetComicsDDLProvider("https://getcomics.org",
		download.WithDDLHTTPClient(srv.Client()),
		download.WithDDLDataPath(dataPath),
		download.WithDDLAllowPrivateHosts(),
	)

	var lastDone, lastTotal int64
	got, err := p.Fetch(context.Background(), srv.URL+"/post", func(done, total int64) {
		lastDone, lastTotal = done, total
	})
	require.NoError(t, err)

	// The file landed under <data_path>/incomplete with the remote base name.
	wantDir := filepath.Join(dataPath, "incomplete")
	require.True(t, strings.HasPrefix(got, wantDir+string(filepath.Separator)),
		"streamed path %q should be under %q", got, wantDir)
	require.Equal(t, "example-comic-1.cbz", filepath.Base(got))

	body, err := os.ReadFile(got)
	require.NoError(t, err)
	require.Equal(t, payload, string(body))

	// Progress was reported from Content-Length.
	require.Equal(t, int64(len(payload)), lastTotal)
	require.Equal(t, int64(len(payload)), lastDone)
}

// TestDDLResolvePrefersPrimaryMirror asserts the resolver tries the "Download Now" mirror
// before falling through to a secondary one: the first (dl1) mirror 500s, so the fetch falls
// through to the second primary ("Mirror Download", dl2) and succeeds — proving ordered
// fallback across the primary mirrors.
func TestDDLResolvePrefersPrimaryMirror(t *testing.T) {
	t.Parallel()

	const payload = "PK\x03\x04 second-mirror-body"

	mux := http.NewServeMux()
	mux.HandleFunc("/dl1/download/example-comic-1.cbz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	mux.HandleFunc("/dl2/download/example-comic-1.cbz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write([]byte(payload))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	postHTML := fixturePostHTML(t, srv.URL)
	mux.HandleFunc("/post", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(postHTML))
	})

	p := download.NewGetComicsDDLProvider("https://getcomics.org",
		download.WithDDLHTTPClient(srv.Client()),
		download.WithDDLDataPath(t.TempDir()),
		download.WithDDLAllowPrivateHosts(),
	)

	got, err := p.Fetch(context.Background(), srv.URL+"/post", nil)
	require.NoError(t, err)
	body, err := os.ReadFile(got)
	require.NoError(t, err)
	require.Equal(t, payload, string(body))
}

// TestDDLMirrorExhaustion asserts that when every candidate mirror is dead (5xx) or reports no
// Content-Length, Fetch returns ErrMirrorExhaustion (D-04) and writes no file.
func TestDDLMirrorExhaustion(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	// dl1: 5xx. dl2: 200 but NO Content-Length (Mylar's ad-page rule rejects it).
	mux.HandleFunc("/dl1/download/example-comic-1.cbz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	mux.HandleFunc("/dl2/download/example-comic-1.cbz", func(w http.ResponseWriter, _ *http.Request) {
		// No Content-Length set + chunked body → resp.ContentLength <= 0 → skipped.
		w.Header().Set("Transfer-Encoding", "chunked")
		_, _ = w.Write([]byte("ad page, not the file"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	postHTML := fixturePostHTML(t, srv.URL)
	mux.HandleFunc("/post", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(postHTML))
	})

	dataPath := t.TempDir()
	p := download.NewGetComicsDDLProvider("https://getcomics.org",
		download.WithDDLHTTPClient(srv.Client()),
		download.WithDDLDataPath(dataPath),
		download.WithDDLAllowPrivateHosts(),
	)

	got, err := p.Fetch(context.Background(), srv.URL+"/post", nil)
	require.ErrorIs(t, err, download.ErrMirrorExhaustion)
	require.Empty(t, got)

	// No file should have been left behind in the intermediate dir.
	entries, derr := os.ReadDir(filepath.Join(dataPath, "incomplete"))
	if derr == nil {
		require.Empty(t, entries, "no partial file should remain after mirror exhaustion")
	} else {
		require.ErrorIs(t, derr, os.ErrNotExist)
	}
}
