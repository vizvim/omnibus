package metadata_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
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
		case strings.Contains(r.URL.Path, "/issue/"):
			_, _ = w.Write(fixture(t, "issue_detail.json"))
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

func TestComicVineListIssuesMapsCreditsAndDescription(t *testing.T) {
	t.Parallel()
	p, _ := cvServer(t, http.StatusOK)
	issues, _, _, err := p.ListIssues(context.Background(), 4050, 0)
	require.NoError(t, err)

	byID := make(map[int64]metadata.IssueDetail, len(issues))
	for _, i := range issues {
		byID[i.ComicvineIssueID] = i
	}

	// Issue 1001: deck is preferred over the longer description; comma roles split into
	// one credit each; the credit with an empty name is dropped.
	seven := byID[1001]
	require.Equal(t, "A short deck summary.", seven.Description)
	require.Equal(t, "2026-05-01 12:00:00", seven.CVLastUpdated)
	require.ElementsMatch(t, []metadata.Credit{
		{Role: "writer", Name: "Brian K. Vaughan", ComicvinePersonID: 5001},
		{Role: "penciler", Name: "Fiona Staples", ComicvinePersonID: 5002},
		{Role: "cover", Name: "Fiona Staples", ComicvinePersonID: 5002},
	}, seven.Credits)

	// Issue 1002: no deck, falls back to the description; no person_credits.
	tieIn := byID[1002]
	require.Equal(t, "<p>Only a description, no deck.</p>", tieIn.Description)
	require.Empty(t, tieIn.Credits)
}

func TestComicVineGetIssueSingleObjectMapsCredits(t *testing.T) {
	t.Parallel()
	p, _ := cvServer(t, http.StatusOK)

	detail, err := p.GetIssue(context.Background(), 1001)
	require.NoError(t, err)

	// Mapped from the single-object results payload (not an array).
	require.Equal(t, int64(1001), detail.ComicvineIssueID)
	require.Equal(t, "7", detail.IssueNumber)
	require.Equal(t, "Issue Seven", detail.Title)
	require.Equal(t, "2026-05-01 12:00:00", detail.CVLastUpdated)
	require.NotEmpty(t, detail.CoverURL)

	// person_credits are present on the DETAIL endpoint: comma roles split, empty name
	// dropped.
	require.ElementsMatch(t, []metadata.Credit{
		{Role: "writer", Name: "Brian K. Vaughan", ComicvinePersonID: 5001},
		{Role: "penciler", Name: "Fiona Staples", ComicvinePersonID: 5002},
		{Role: "cover", Name: "Fiona Staples", ComicvinePersonID: 5002},
	}, detail.Credits)
}

func TestComicVineGetIssueAcceptsArrayResults(t *testing.T) {
	t.Parallel()
	// Defensive: some CV responses wrap a single issue in a one-element array; GetIssue
	// must accept that shape too.
	body := `{"status_code":1,"results":[{"id":1001,"issue_number":"7","name":"Issue Seven",` +
		`"person_credits":[{"id":5001,"name":"Brian K. Vaughan","role":"writer"}]}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	p := metadata.NewComicVineProvider("k", metadata.WithBaseURL(srv.URL), metadata.WithHTTPClient(srv.Client()))

	detail, err := p.GetIssue(context.Background(), 1001)
	require.NoError(t, err)
	require.Equal(t, int64(1001), detail.ComicvineIssueID)
	require.Len(t, detail.Credits, 1)
	require.Equal(t, "writer", detail.Credits[0].Role)
}

func TestComicVineGetIssueEmptyArrayResults(t *testing.T) {
	t.Parallel()
	// An empty results array yields a zero-value detail with no error (no issue found).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status_code":1,"results":[]}`))
	}))
	t.Cleanup(srv.Close)
	p := metadata.NewComicVineProvider("k", metadata.WithBaseURL(srv.URL), metadata.WithHTTPClient(srv.Client()))

	detail, err := p.GetIssue(context.Background(), 999)
	require.NoError(t, err)
	require.Zero(t, detail.ComicvineIssueID)
	require.Empty(t, detail.Credits)
}

func TestComicVineGetIssueBanIsTypedError(t *testing.T) {
	t.Parallel()
	p, _ := cvServer(t, http.StatusTooManyRequests)
	_, err := p.GetIssue(context.Background(), 1001)
	require.Error(t, err)
	require.ErrorIs(t, err, metadata.ErrProviderUnavailable)
}

func TestComicVineBanResponseIsTypedError(t *testing.T) {
	t.Parallel()
	p, _ := cvServer(t, http.StatusTooManyRequests) // 429-style ban
	_, err := p.SearchSeries(context.Background(), "x")
	require.Error(t, err)
	require.ErrorIs(t, err, metadata.ErrProviderUnavailable)
}

// rewriteTransport routes every request to target, regardless of the request URL's
// host, so a test can drive GetCover's happy path with a CV-host URL (which passes the
// SSRF guard) while the bytes are actually served by a local httptest server.
type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = rt.target.Scheme
	req.URL.Host = rt.target.Host
	return rt.base.RoundTrip(req)
}

func TestComicVineGetCoverHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("\xff\xd8\xff cover bytes"))
	}))
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	require.NoError(t, err)

	client := &http.Client{Transport: rewriteTransport{target: target, base: srv.Client().Transport}}
	p := metadata.NewComicVineProvider("k", metadata.WithHTTPClient(client))

	// A CV-host https URL passes the SSRF guard; the transport reroutes to the test server.
	data, err := p.GetCover(context.Background(), "https://comicvine.gamespot.com/a/uploads/x.jpg")
	require.NoError(t, err)
	require.NotEmpty(t, data)
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

	// GetIssue returns a DETAIL with credits (the LIST fixture lacks them); the supplied
	// id is reflected onto the returned detail.
	detail, err := fake.GetIssue(context.Background(), 1001)
	require.NoError(t, err)
	require.Equal(t, int64(1001), detail.ComicvineIssueID)
	require.NotEmpty(t, detail.Credits)
}
