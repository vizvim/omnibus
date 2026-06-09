package indexer_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
