package metadata_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/provider/metadata"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "fixtures", name))
	require.NoError(t, err)
	return b
}

// cvServer returns an httptest server that maps CV request paths to fixtures and the
// ComicVine provider pointed at it.
func cvServer(t *testing.T, status int) (*metadata.ComicVineProvider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"status_code": 107, "error": "Rate Limit Exceeded"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/search"):
			_, _ = w.Write(fixture(t, "search_same_title.json"))
		case strings.Contains(r.URL.Path, "/volume/"):
			_, _ = w.Write(fixture(t, "volume_large_series.json"))
		case strings.Contains(r.URL.Path, "/issues"):
			_, _ = w.Write(fixture(t, "issues_non_integer.json"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	p := metadata.NewComicVineProvider("test-key", metadata.WithBaseURL(srv.URL), metadata.WithHTTPClient(srv.Client()))
	return p, srv
}

func TestComicVineSearchSameTitle(t *testing.T) {
	t.Parallel()
	p, _ := cvServer(t, http.StatusOK)
	results, err := p.SearchSeries(context.Background(), "Daredevil")
	require.NoError(t, err)
	require.Len(t, results, 2)
	// Distinguishable by start_year + count (the disambiguation signal).
	require.NotEqual(t, results[0].StartYear, results[1].StartYear)
	require.Equal(t, "Marvel", results[0].Publisher)
	require.NotEmpty(t, results[0].CoverURL)
}

func TestComicVineGetVolume(t *testing.T) {
	t.Parallel()
	p, _ := cvServer(t, http.StatusOK)
	vol, raw, err := p.GetVolume(context.Background(), 4050)
	require.NoError(t, err)
	require.NotEmpty(t, raw)
	require.Equal(t, int64(4050), vol.ComicvineVolumeID)
	require.Equal(t, "Marvel", vol.Publisher.Name)
	require.NotEmpty(t, vol.CoverURL)
	require.Equal(t, "2026-05-01 12:00:00", vol.DateLastUpdated)
}

func TestComicVineListIssuesNonInteger(t *testing.T) {
	t.Parallel()
	p, _ := cvServer(t, http.StatusOK)
	issues, hasMore, raw, err := p.ListIssues(context.Background(), 4050, 0)
	require.NoError(t, err)
	require.NotEmpty(t, raw)
	require.False(t, hasMore)

	numbers := make([]string, 0, len(issues))
	for _, i := range issues {
		numbers = append(numbers, i.IssueNumber)
	}
	require.Contains(t, numbers, "7")
	require.Contains(t, numbers, "7.INH")
	require.Contains(t, numbers, "½")
	require.Contains(t, numbers, "Annual 1")
}

func TestComicVineBanResponseIsTypedError(t *testing.T) {
	t.Parallel()
	p, _ := cvServer(t, http.StatusTooManyRequests) // 429-style ban
	_, err := p.SearchSeries(context.Background(), "x")
	require.Error(t, err)
	require.ErrorIs(t, err, metadata.ErrProviderUnavailable)
}

func TestComicVineGetCoverRejectsBadURL(t *testing.T) {
	t.Parallel()
	p := metadata.NewComicVineProvider("test-key")
	ctx := context.Background()

	_, err := p.GetCover(ctx, "http://evil.example.com/x.jpg") // non-https, non-CV host
	require.Error(t, err)
	require.ErrorIs(t, err, metadata.ErrInvalidCoverURL)

	_, err = p.GetCover(ctx, "https://evil.example.com/x.jpg") // https but wrong host
	require.Error(t, err)
	require.ErrorIs(t, err, metadata.ErrInvalidCoverURL)
}

func TestFakeProviderFromFixtures(t *testing.T) {
	t.Parallel()
	// No network — proves the CI posture.
	fake, err := metadata.NewFakeProvider("testdata/fixtures")
	require.NoError(t, err)

	var _ metadata.MetadataProvider = fake // compile-time interface assertion

	results, err := fake.SearchSeries(context.Background(), "Daredevil")
	require.NoError(t, err)
	require.Len(t, results, 2)

	issues, _, _, err := fake.ListIssues(context.Background(), 4050, 0)
	require.NoError(t, err)
	numbers := make([]string, 0, len(issues))
	for _, i := range issues {
		numbers = append(numbers, i.IssueNumber)
	}
	require.Contains(t, numbers, "7.INH")
	require.Contains(t, numbers, "½")
	require.Contains(t, numbers, "Annual 1")
}
