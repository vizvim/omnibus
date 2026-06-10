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
type fakeGateway struct {
	candidates []indexer.Candidate
	feed       []indexer.Candidate
	err        error
}

func (f *fakeGateway) Search(_ context.Context, _ []indexer.IndexerProvider, _ string) ([]indexer.Candidate, error) {
	return f.candidates, f.err
}

func (f *fakeGateway) Feed(_ context.Context, _ []indexer.IndexerProvider) ([]indexer.Candidate, error) {
	if f.feed != nil {
		return f.feed, f.err
	}
	return f.candidates, f.err
}

func newSearchService(t *testing.T, gwCands []indexer.Candidate) (*search.Service, *repository.Repositories, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "svc.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	repos := repository.NewRepositories(d)
	ctx := context.Background()
	s, err := repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 4050, Name: "Saga", Status: "Active", CreatedAt: ts,
	})
	require.NoError(t, err)
	iss, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: s.ID, ComicvineIssueID: 9001, IssueNumberRaw: "7", IssueNumberSort: 7.0,
		Status: "Wanted", CreatedAt: ts,
	})
	require.NoError(t, err)

	// An enabled indexer so gatherCandidates builds a provider (the fake gateway then
	// ignores it and returns the canned candidates).
	_, err = repos.Indexers.Create(ctx, repository.IndexerUpsert{
		Name: "nzb", Kind: "newznab", BaseURL: "http://nzb.test", Enabled: true, Categories: "7030",
	}, ts)
	require.NoError(t, err)

	svc := search.New(search.Deps{
		Gateway:           &fakeGateway{candidates: gwCands},
		Repos:             repos,
		DownloadProviders: map[string]download.DownloadProvider{"sabnzbd": download.NewFakeProvider("sabnzbd", "nzo_1")},
		AttemptCap:        5,
	})
	return svc, repos, iss.ID
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
