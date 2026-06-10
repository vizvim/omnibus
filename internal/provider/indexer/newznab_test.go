package indexer_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/provider/indexer"
)

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

// newznabServer serves the fixture XML and records the last query the provider sent.
func newznabServer(t *testing.T) (*indexer.NewznabProvider, *string) {
	t.Helper()
	var lastRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write(fixtureBytes(t, "newznab_search.xml"))
	}))
	t.Cleanup(srv.Close)
	p := indexer.NewNewznabProvider(srv.URL, "test-key", "7030", indexer.WithNewznabHTTPClient(srv.Client()))
	return p, &lastRawQuery
}

func TestNewznabSearchParsesItems(t *testing.T) {
	t.Parallel()
	p, lastQuery := newznabServer(t)

	cands, err := p.Search(context.Background(), "Saga")
	require.NoError(t, err)
	require.Len(t, cands, 3)

	// Query carried the expected newznab params.
	require.Contains(t, *lastQuery, "t=search")
	require.Contains(t, *lastQuery, "apikey=test-key")
	require.Contains(t, *lastQuery, "cat=7030")
	require.Contains(t, *lastQuery, "q=Saga")

	first := cands[0]
	require.Equal(t, "newznab", first.Provider)
	require.Equal(t, "abc123guid", first.ReleaseKey)
	require.Equal(t, "Saga 007 (2013) (Digital) (cbz)", first.Title)
	require.Equal(t, int64(52428800), first.SizeBytes)
	require.Equal(t, "cbz", first.Format)
	require.Equal(t, "https://nzb.example.test/getnzb/abc123.nzb", first.DownloadURL)

	require.Equal(t, "cbr", cands[1].Format)
	require.Equal(t, "def456guid", cands[1].ReleaseKey)
}

func TestNewznabReleaseKeyHashFallbackWhenNoGUID(t *testing.T) {
	t.Parallel()
	p, _ := newznabServer(t)

	cands, err := p.Search(context.Background(), "Batman")
	require.NoError(t, err)
	require.Len(t, cands, 3)

	// The third item has no guid → deterministic sha256 hex fallback (never empty).
	third := cands[2]
	require.NotEmpty(t, third.ReleaseKey)
	require.Len(t, third.ReleaseKey, 64) // sha256 hex
	require.True(t, third.IsPack)
	require.Equal(t, "pdf", third.Format)
}

func TestNewznabFeedOmitsQuery(t *testing.T) {
	t.Parallel()
	p, lastQuery := newznabServer(t)

	cands, err := p.Feed(context.Background())
	require.NoError(t, err)
	require.Len(t, cands, 3)
	require.NotContains(t, *lastQuery, "q=")
}

// TestNewznabSchemeLessBaseURLBuildsValidRequest reproduces the production failure:
// an indexer saved as a scheme-less "host:port" must still build + send a valid request
// (it previously broke http.NewRequestWithContext with "first path segment in URL cannot
// contain colon"). The provider normalizes the base URL by prefixing http://.
func TestNewznabSchemeLessBaseURLBuildsValidRequest(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write(fixtureBytes(t, "newznab_search.xml"))
	}))
	t.Cleanup(srv.Close)

	// Strip the http:// scheme to mimic a base URL saved as bare host:port.
	schemeless := strings.TrimPrefix(srv.URL, "http://")
	require.NotContains(t, schemeless, "://")

	p := indexer.NewNewznabProvider(schemeless, "k", "7030", indexer.WithNewznabHTTPClient(srv.Client()))
	cands, err := p.Search(context.Background(), "Saga")
	require.NoError(t, err, "scheme-less host:port must build a valid request, not a parse error")
	require.Len(t, cands, 3)
}

// TestNewznabAPIErrorBodyIsError reproduces the false-positive: a newznab indexer
// returns HTTP 200 with a well-formed <error/> body for application failures (bad
// API key). The provider must surface a non-nil error echoing the server's own
// description, not silently return zero candidates.
func TestNewznabAPIErrorBodyIsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<error code="100" description="Incorrect user credentials"/>`))
	}))
	t.Cleanup(srv.Close)
	p := indexer.NewNewznabProvider(srv.URL, "bad-key", "", indexer.WithNewznabHTTPClient(srv.Client()))

	cands, err := p.Search(context.Background(), "Saga")
	require.Error(t, err)
	require.Contains(t, err.Error(), "Incorrect user credentials")
	require.Contains(t, err.Error(), "100")
	require.Empty(t, cands)
}

// TestNewznabAPIErrorNoDescriptionIsError covers the edge case where the server
// omits the description attribute (<error code="900"/>) — the code alone must
// still surface as an error.
func TestNewznabAPIErrorNoDescriptionIsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<error code="900"/>`))
	}))
	t.Cleanup(srv.Close)
	p := indexer.NewNewznabProvider(srv.URL, "k", "", indexer.WithNewznabHTTPClient(srv.Client()))

	cands, err := p.Search(context.Background(), "x")
	require.Error(t, err)
	require.Contains(t, err.Error(), "newznab error 900")
	require.Empty(t, cands)
}

func TestNewznabNon200IsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)
	p := indexer.NewNewznabProvider(srv.URL, "k", "", indexer.WithNewznabHTTPClient(srv.Client()))

	_, err := p.Search(context.Background(), "x")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unavailable")
}
