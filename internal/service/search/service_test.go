package search_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/provider/download"
	"github.com/vizvim/omnibus/internal/provider/indexer"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/service/search"
)

// fakeGateway returns canned candidates regardless of the providers passed, so service
// tests need no network. Search() returns candidates; Feed() returns feed (falling back
// to candidates when feed is nil) so search and RSS paths can be driven independently.
//
// It also records every query string it received (gotQueries) and, when perQuery is set,
// returns the candidates mapped for that exact query (falling back to the flat candidates
// slice for queries not present in the map). This lets tests assert the composed mylar-style
// queries and the service-layer union/dedup behavior.
type fakeGateway struct {
	candidates []indexer.Candidate
	feed       []indexer.Candidate
	perQuery   map[string][]indexer.Candidate
	gotQueries []string
	err        error
}

func (f *fakeGateway) Search(_ context.Context, _ []indexer.IndexerProvider, query string) ([]indexer.Candidate, error) {
	f.gotQueries = append(f.gotQueries, query)
	if f.err != nil {
		return nil, f.err
	}
	if f.perQuery != nil {
		return f.perQuery[query], nil
	}
	return f.candidates, nil
}

func (f *fakeGateway) Feed(_ context.Context, _ []indexer.IndexerProvider) ([]indexer.Candidate, error) {
	if f.feed != nil {
		return f.feed, f.err
	}
	return f.candidates, f.err
}

func newSearchService(t *testing.T, gwCands []indexer.Candidate) (*search.Service, *repository.Repositories, int64) {
	t.Helper()
	svc, repos, issueID, _ := newSearchServiceWithGateway(t, &fakeGateway{candidates: gwCands}, issueSeed{
		seriesName: "Saga", issueNumberRaw: "7", issueNumberSort: 7.0,
	})
	return svc, repos, issueID
}

// issueSeed parameterizes the series + issue the helper seeds so tests can exercise
// standard, annual, and one-shot query composition.
type issueSeed struct {
	seriesName      string
	issueNumberRaw  string
	issueNumberSort float64
	issueNumberQual string
	issueType       string
}

// newSearchServiceWithGateway seeds a series + issue (per seed) and an enabled newznab
// indexer, wires the supplied gateway, and returns the service, repos, the issue id, and
// the gateway (so the caller can inspect the queries it received).
func newSearchServiceWithGateway(t *testing.T, gw *fakeGateway, seed issueSeed) (*search.Service, *repository.Repositories, int64, *fakeGateway) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "svc.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	repos := repository.NewRepositories(d)
	ctx := context.Background()
	s, err := repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 4050, Name: seed.seriesName, Status: "Active", CreatedAt: ts,
	})
	require.NoError(t, err)
	iss, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: s.ID, ComicvineIssueID: 9001, IssueNumberRaw: seed.issueNumberRaw,
		IssueNumberSort: seed.issueNumberSort, IssueNumberQual: seed.issueNumberQual,
		IssueType: seed.issueType, Status: "Wanted", CreatedAt: ts,
	})
	require.NoError(t, err)

	// An enabled indexer so gatherCandidates builds a provider (the fake gateway then
	// ignores it and returns the canned candidates).
	_, err = repos.Indexers.Create(ctx, repository.IndexerUpsert{
		Name: "nzb", Kind: "newznab", BaseURL: "http://nzb.test", Enabled: true, Categories: "7030",
	}, ts)
	require.NoError(t, err)

	svc := search.New(search.Deps{
		Gateway:           gw,
		Repos:             repos,
		DownloadProviders: map[string]download.DownloadProvider{"sabnzbd": download.NewFakeProvider("sabnzbd", "nzo_1")},
		AttemptCap:        5,
	})
	return svc, repos, iss.ID, gw
}

func svcCand(provider, releaseKey, format string, size int64) indexer.Candidate {
	return indexer.Candidate{
		Provider:       provider,
		ReleaseKey:     releaseKey,
		Title:          "Saga #7 (" + format + ")",
		IssueNumberRaw: "7",
		Format:         format,
		SizeBytes:      size,
		DownloadURL:    "http://nzb/" + releaseKey + ".nzb",
	}
}

func TestSearchIssueRanksAndWritesSearchedEvent(t *testing.T) {
	t.Parallel()
	svc, repos, issueID := newSearchService(t, []indexer.Candidate{
		svcCand("newznab", "rk-pdf", "pdf", 30*1024*1024),
		svcCand("newznab", "rk-cbz", "cbz", 30*1024*1024),
	})
	ctx := context.Background()

	res, err := svc.SearchIssue(ctx, issueID)
	require.NoError(t, err)
	require.True(t, res.Acceptable)
	require.NotEmpty(t, res.Candidates)
	// cbz ranks above pdf.
	require.Equal(t, "rk-cbz", res.Candidates[0].ReleaseKey)
	require.Greater(t, res.Candidates[0].Score, 0.0)

	// A searched event was written.
	events, err := repos.IssueEvents.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "searched", events[0].EventType)
}

func TestSearchIssueIncludesRejectedWithReason(t *testing.T) {
	t.Parallel()
	// A wrong-issue candidate is rejected and carries a reason for transparency (D-04).
	svc, _, issueID := newSearchService(t, []indexer.Candidate{
		{Provider: "newznab", ReleaseKey: "wrong", Title: "Saga #8", IssueNumberRaw: "8", Format: "cbz", SizeBytes: 30 * 1024 * 1024, DownloadURL: "http://x"},
	})
	ctx := context.Background()

	res, err := svc.SearchIssue(ctx, issueID)
	require.NoError(t, err)
	require.False(t, res.Acceptable)
	require.NotEmpty(t, res.FloorReason)

	var found bool
	for _, c := range res.Candidates {
		if c.ReleaseKey == "wrong" {
			require.Equal(t, "wrong-issue", c.Reason)
			found = true
		}
	}
	require.True(t, found, "rejected candidate should appear with its reason")
}

func TestSelectCandidateGrabsAndWritesEvents(t *testing.T) {
	t.Parallel()
	svc, repos, issueID := newSearchService(t, []indexer.Candidate{
		svcCand("newznab", "rk-cbz", "cbz", 30*1024*1024),
	})
	ctx := context.Background()

	dl, err := svc.SelectCandidate(ctx, issueID, "newznab", "rk-cbz")
	require.NoError(t, err)
	require.Equal(t, "nzo_1", dl.ClientRef)

	// Issue is Snatched.
	iss, err := repos.Issue.GetByID(ctx, issueID)
	require.NoError(t, err)
	require.Equal(t, "Snatched", iss.Status)

	// Timeline: candidate-selected then snatched.
	events, err := repos.IssueEvents.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	types := make([]string, 0, len(events))
	for _, e := range events {
		types = append(types, e.EventType)
	}
	require.Contains(t, types, "candidate-selected")
	require.Contains(t, types, "snatched")
}

func TestSelectCandidateRejectsCrossIssueRelease(t *testing.T) {
	t.Parallel()
	svc, _, issueID := newSearchService(t, []indexer.Candidate{
		svcCand("newznab", "rk-cbz", "cbz", 30*1024*1024),
	})
	ctx := context.Background()

	// A release_key the gateway never produced for this issue is rejected (T-4-02).
	_, err := svc.SelectCandidate(ctx, issueID, "newznab", "not-a-candidate")
	require.ErrorIs(t, err, search.ErrCrossIssueGrab)
}

func TestGetTimelineOrdered(t *testing.T) {
	t.Parallel()
	svc, repos, issueID := newSearchService(t, nil)
	ctx := context.Background()

	_, err := repos.IssueEvents.Insert(ctx, repository.IssueEventInsert{
		IssueID: issueID, EventType: "searched", OccurredAt: "2026-01-01T00:00:00Z",
	})
	require.NoError(t, err)
	_, err = repos.IssueEvents.Insert(ctx, repository.IssueEventInsert{
		IssueID: issueID, EventType: "snatched", OccurredAt: "2026-01-01T00:05:00Z",
	})
	require.NoError(t, err)

	events, err := svc.GetTimeline(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, "searched", events[0].Type)
	require.Equal(t, "snatched", events[1].Type)
}

func TestStandardIssueComposesSeriesNameAndPaddedVariants(t *testing.T) {
	t.Parallel()
	// A standard single-digit issue searches "<clean name> 007" / " 07" / " 7". Each
	// query returns its own candidate; the union is deduped by release_key (the cbz
	// release appears for two variants but survives once, first occurrence wins).
	gw := &fakeGateway{perQuery: map[string][]indexer.Candidate{
		"Saga 007": {svcCand("newznab", "rk-cbz", "cbz", 30*1024*1024)},
		"Saga 07":  {svcCand("newznab", "rk-cbz", "cbz", 30*1024*1024)},
		"Saga 7":   {svcCand("newznab", "rk-pdf", "pdf", 30*1024*1024)},
	}}
	svc, _, issueID, gwOut := newSearchServiceWithGateway(t, gw, issueSeed{
		seriesName: "Saga", issueNumberRaw: "7", issueNumberSort: 7.0,
	})
	ctx := context.Background()

	res, err := svc.SearchIssue(ctx, issueID)
	require.NoError(t, err)

	require.Equal(t, []string{"Saga 007", "Saga 07", "Saga 7"}, gwOut.gotQueries)

	// Union deduped by release_key: rk-cbz once + rk-pdf once.
	keys := candidateKeys(res.Candidates)
	require.Contains(t, keys, "rk-cbz")
	require.Contains(t, keys, "rk-pdf")
	require.Equal(t, 1, countKey(keys, "rk-cbz"), "duplicate release_key collapses to one")
}

func TestAnnualIssueComposesAnnualQueries(t *testing.T) {
	t.Parallel()
	gw := &fakeGateway{perQuery: map[string][]indexer.Candidate{
		"Saga annual 003": {svcCand("newznab", "rk-cbz", "cbz", 30*1024*1024)},
	}}
	svc, _, issueID, gwOut := newSearchServiceWithGateway(t, gw, issueSeed{
		seriesName: "Saga", issueNumberRaw: "3", issueNumberSort: 3.0, issueType: "annual",
	})
	ctx := context.Background()

	_, err := svc.SearchIssue(ctx, issueID)
	require.NoError(t, err)

	require.Equal(t, []string{"Saga annual 003", "Saga annual 03", "Saga annual 3"}, gwOut.gotQueries)
}

func TestOneShotIssueComposesSeriesNameOnly(t *testing.T) {
	t.Parallel()
	gw := &fakeGateway{}
	svc, _, issueID, gwOut := newSearchServiceWithGateway(t, gw, issueSeed{
		seriesName: "The Killing Joke", issueNumberRaw: "1", issueNumberSort: 1.0, issueType: "one-shot",
	})
	ctx := context.Background()

	_, err := svc.SearchIssue(ctx, issueID)
	require.NoError(t, err)

	// "The" is stripped; no issue number is appended for a one-shot.
	require.Equal(t, []string{"Killing Joke"}, gwOut.gotQueries)
}

// candidateKeys extracts the release keys from a result's candidate views.
func candidateKeys(views []search.CandidateView) []string {
	out := make([]string, 0, len(views))
	for _, v := range views {
		out = append(out, v.ReleaseKey)
	}
	return out
}

// countKey counts how many times key appears in keys.
func countKey(keys []string, key string) int {
	n := 0
	for _, k := range keys {
		if k == key {
			n++
		}
	}
	return n
}
