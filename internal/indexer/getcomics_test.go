package indexer_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/indexer"
)

func getComicsServer(t *testing.T) (*indexer.GetComicsProvider, *string) {
	t.Helper()
	var lastRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(fixtureBytes(t, "getcomics_results.html"))
	}))
	t.Cleanup(srv.Close)
	p := indexer.NewGetComicsProvider(srv.URL, indexer.WithGetComicsHTTPClient(srv.Client()))
	return p, &lastRawQuery
}

func TestGetComicsSearchParsesPosts(t *testing.T) {
	t.Parallel()
	p, lastQuery := getComicsServer(t)

	cands, err := p.Search(context.Background(), "Saga")
	require.NoError(t, err)
	require.Contains(t, *lastQuery, "s=Saga")
	require.Len(t, cands, 3)

	first := cands[0]
	require.Equal(t, "getcomics", first.Provider)
	require.Equal(t, "https://getcomics.org/comic/saga-7/", first.ReleaseKey)
	require.Equal(t, "https://getcomics.org/comic/saga-7/", first.DownloadURL)
	require.Equal(t, "Saga #7 (2013)", first.Title)
	require.Equal(t, int64(50*1024*1024), first.SizeBytes)

	// 1.2 GB parsed for the second post (matches the provider's float math). Use a
	// runtime float so the non-integer constant conversion is legal.
	gbFactor := 1.2
	require.Equal(t, int64(gbFactor*1024*1024*1024), cands[1].SizeBytes)
}

func TestGetComicsMissingSizeYieldsZero(t *testing.T) {
	t.Parallel()
	p, _ := getComicsServer(t)

	cands, err := p.Search(context.Background(), "Batman")
	require.NoError(t, err)
	require.Len(t, cands, 3)

	// The third post (Batman) has no "Size :" text → 0, not an error/reject.
	third := cands[2]
	require.Equal(t, int64(0), third.SizeBytes)
	require.True(t, third.IsPack)
	require.Equal(t, "https://getcomics.org/comic/batman-vol-1/", third.ReleaseKey)
}

// TestGetComicsSchemeLessBaseURLBuildsValidRequest mirrors the newznab case: a
// scheme-less "host:port" base URL must still build a valid request after normalization.
func TestGetComicsSchemeLessBaseURLBuildsValidRequest(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write(fixtureBytes(t, "getcomics_results.html"))
	}))
	t.Cleanup(srv.Close)

	schemeless := strings.TrimPrefix(srv.URL, "http://")
	require.NotContains(t, schemeless, "://")

	p := indexer.NewGetComicsProvider(schemeless, indexer.WithGetComicsHTTPClient(srv.Client()))
	cands, err := p.Search(context.Background(), "Saga")
	require.NoError(t, err, "scheme-less host:port must build a valid request, not a parse error")
	require.Len(t, cands, 3)
}

func TestGetComicsNon200IsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	p := indexer.NewGetComicsProvider(srv.URL, indexer.WithGetComicsHTTPClient(srv.Client()))

	_, err := p.Search(context.Background(), "x")
	require.Error(t, err)
}
