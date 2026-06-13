package search_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/download"
	"github.com/vizvim/omnibus/internal/indexer"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/search"
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

	// getComicsCands are returned for a Search targeting the built-in GetComics provider;
	// getComicsCalls counts how many times the GetComics source was consulted (the DDL
	// fallback). A non-getcomics Search uses candidates/perQuery as before.
	getComicsCands []indexer.Candidate
	getComicsCalls int
}

func (f *fakeGateway) Search(_ context.Context, providers []indexer.IndexerProvider, query string) ([]indexer.Candidate, error) {
	// Record whether this Search call targeted the built-in GetComics source (the fallback
	// gather passes a single getcomics provider) so tests can assert it was/wasn't consulted.
	for _, p := range providers {
		if p.Kind() == indexer.GetComicsKind {
			f.getComicsCalls++
			f.gotQueries = append(f.gotQueries, query)
			if f.err != nil {
				return nil, f.err
			}
			return f.getComicsCands, nil
		}
	}

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
		DownloadProviders: map[string]search.Submitter{"sabnzbd": download.NewFakeProvider("sabnzbd", "nzo_1")},
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

// gcCand builds a GetComics candidate (provider "getcomics") for an issue. A DDL grab
// routes through the getcomics download provider and enqueues a DDLFetch.
func gcCand(releaseKey, format string, size int64) indexer.Candidate {
	return indexer.Candidate{
		Provider:       "getcomics",
		ReleaseKey:     releaseKey,
		Title:          "Saga #7 (" + format + ")",
		IssueNumberRaw: "7",
		Format:         format,
		SizeBytes:      size,
		DownloadURL:    "https://getcomics.org/" + releaseKey,
	}
}

// newDDLSearchService seeds the standard Saga #7 fixture with an enabled newznab indexer,
// wires the supplied gateway + a recording enqueuer (so DDL fetches are observable), and
// sets enable_ddl to the given value. It returns the service, the issue id, the gateway,
// the enqueuer, and the repos (so a test can inspect the downloads row the grab created).
func newDDLSearchService(t *testing.T, gw *fakeGateway, ddlEnabled bool) (*search.Service, int64, *fakeGateway, *recordingEnqueuer, *repository.Repositories) {
	t.Helper()
	_, repos, issueID, gwOut := newSearchServiceWithGateway(t, gw, issueSeed{
		seriesName: "Saga", issueNumberRaw: "7", issueNumberSort: 7.0,
	})
	// Rebuild the service with the getcomics download provider wired so a DDL grab can
	// submit + enqueue a DDLFetch (the base helper only wires SABnzbd).
	svc := search.New(search.Deps{
		Gateway: gwOut,
		Repos:   repos,
		DownloadProviders: map[string]search.Submitter{
			"sabnzbd":   download.NewFakeProvider("sabnzbd", "nzo_1"),
			"getcomics": download.NewFakeProvider("getcomics", "ddl_1"),
		},
		AttemptCap: 5,
	})
	enq := &recordingEnqueuer{}
	svc.SetEnqueuer(enq)
	if ddlEnabled {
		require.NoError(t, repos.UserConfig.Set(context.Background(), "enable_ddl", "true"))
	}
	return svc, issueID, gwOut, enq, repos
}

// TestDDLDisabledNeverConsultsGetComics: with enable_ddl=false, a wanted issue whose only
// acceptable release is a GetComics one is NOT grabbed, GetComics is never consulted, and
// no DDLFetch is enqueued.
func TestDDLDisabledNeverConsultsGetComics(t *testing.T) {
	t.Parallel()
	gw := &fakeGateway{
		candidates:     nil, // no acceptable Newznab candidate
		getComicsCands: []indexer.Candidate{gcCand("gc-cbz", "cbz", 30*1024*1024)},
	}
	svc, issueID, gwOut, enq, _ := newDDLSearchService(t, gw, false)
	ctx := context.Background()

	res, err := svc.SearchIssue(ctx, issueID)
	require.NoError(t, err)
	require.False(t, res.Acceptable, "DDL off: no Newznab candidate ⇒ nothing acceptable")
	require.Equal(t, 0, gwOut.getComicsCalls, "GetComics must not be consulted when enable_ddl=false")
	require.Empty(t, enq.ddlFetched, "no DDLFetch may be enqueued when DDL is off")

	// A manual grab of the getcomics release is rejected (no matching candidate).
	_, err = svc.SelectCandidate(ctx, issueID, "getcomics", "gc-cbz")
	require.ErrorIs(t, err, search.ErrCrossIssueGrab)
	require.Empty(t, enq.ddlFetched)
}

// TestDDLEnabledNewznabAcceptableSkipsGetComics: with enable_ddl=true, when an acceptable
// Newznab candidate exists, GetComics is NOT consulted (fallback only) and the grabbed pick
// is the Newznab one.
func TestDDLEnabledNewznabAcceptableSkipsGetComics(t *testing.T) {
	t.Parallel()
	gw := &fakeGateway{
		candidates:     []indexer.Candidate{svcCand("newznab", "rk-cbz", "cbz", 30*1024*1024)},
		getComicsCands: []indexer.Candidate{gcCand("gc-cbz", "cbz", 30*1024*1024)},
	}
	svc, issueID, gwOut, enq, _ := newDDLSearchService(t, gw, true)
	ctx := context.Background()

	res, err := svc.SearchIssue(ctx, issueID)
	require.NoError(t, err)
	require.True(t, res.Acceptable)
	require.Equal(t, 0, gwOut.getComicsCalls, "an acceptable Newznab candidate must short-circuit the GetComics fallback")

	dl, err := svc.SelectCandidate(ctx, issueID, "newznab", "rk-cbz")
	require.NoError(t, err)
	require.Equal(t, "nzo_1", dl.ClientRef, "the Newznab pick is grabbed via SABnzbd")
	require.Empty(t, enq.ddlFetched, "a Newznab grab enqueues no DDLFetch")
}

// TestDDLEnabledNoNewznabFallsBackToGetComics: with enable_ddl=true and NO acceptable
// Newznab candidate but an acceptable GetComics one, GetComics is consulted as a fallback,
// the GetComics release is grabbed, and a DDLFetch is enqueued.
func TestDDLEnabledNoNewznabFallsBackToGetComics(t *testing.T) {
	t.Parallel()
	gw := &fakeGateway{
		candidates:     nil, // no acceptable Newznab candidate
		getComicsCands: []indexer.Candidate{gcCand("gc-cbz", "cbz", 30*1024*1024)},
	}
	svc, issueID, gwOut, enq, _ := newDDLSearchService(t, gw, true)
	ctx := context.Background()

	res, err := svc.SearchIssue(ctx, issueID)
	require.NoError(t, err)
	require.True(t, res.Acceptable, "the GetComics fallback supplies an acceptable candidate")
	require.GreaterOrEqual(t, gwOut.getComicsCalls, 1, "GetComics must be consulted when Newznab yields nothing acceptable")

	dl, err := svc.SelectCandidate(ctx, issueID, "getcomics", "gc-cbz")
	require.NoError(t, err)
	require.Equal(t, "ddl_1", dl.ClientRef, "the GetComics pick is grabbed via the DDL provider")
	require.Len(t, enq.ddlFetched, 1, "a GetComics grab enqueues exactly one DDLFetch")
}

// TestDDLAutoGrabFallbackEnqueuesDDLFetch is the CR-01 regression guard: the AUTO-grab
// path (SearchAndGrab/RunSearchIssue/RunReplacement/RetryDownload all share runSearchIssue)
// must enqueue a DDLFetch when it falls back to a GetComics pick. Before the fix the enqueue
// lived only in the manual SelectCandidate path, so an auto-grabbed getcomics download was
// snatched then stranded in Queued forever (never fetched, never post-processed). This test
// exercises SearchAndGrab (not SelectCandidate) so it actually covers the auto path the
// existing TestDDLEnabledNoNewznabFallsBackToGetComics missed.
func TestDDLAutoGrabFallbackEnqueuesDDLFetch(t *testing.T) {
	t.Parallel()
	gw := &fakeGateway{
		candidates:     nil, // no acceptable Newznab candidate -> DDL fallback consulted
		getComicsCands: []indexer.Candidate{gcCand("gc-cbz", "cbz", 30*1024*1024)},
	}
	svc, issueID, gwOut, enq, repos := newDDLSearchService(t, gw, true)
	ctx := context.Background()

	outcome, err := svc.SearchAndGrab(ctx, issueID)
	require.NoError(t, err)
	require.True(t, outcome.Grabbed, "the GetComics fallback pick is auto-grabbed")
	require.GreaterOrEqual(t, gwOut.getComicsCalls, 1, "GetComics consulted on the auto path")
	require.Len(t, enq.ddlFetched, 1, "an auto-grabbed GetComics release enqueues exactly one DDLFetch")

	// The enqueued DDLFetch must reference the download row the grab actually created, so the
	// fetch job resolves the right download (not a stale/zero id).
	dls, err := repos.Downloads.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, dls, 1, "the auto-grab created exactly one downloads row")
	require.Equal(t, dls[0].ID, enq.ddlFetched[0], "DDLFetch is enqueued for the grabbed download's id")
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
