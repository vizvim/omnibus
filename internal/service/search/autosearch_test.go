package search_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/provider/indexer"
	"github.com/vizvim/omnibus/internal/repository"
)

// fakeEnqueuer records the issue ids the auto-search sweep enqueues.
type fakeEnqueuer struct {
	enqueued []int64
}

func (f *fakeEnqueuer) EnqueueSearchIssue(_ context.Context, issueID int64) error {
	f.enqueued = append(f.enqueued, issueID)
	return nil
}

// seedWantedIssue inserts a Wanted issue (in the series seeded by newSearchService's
// fixture would conflict; this helper takes the repos + series id directly).
func seedWanted(t *testing.T, repos *repository.Repositories, seriesID, cvID int64, raw string, sort float64, created string, attempts int32) int64 {
	t.Helper()
	ctx := context.Background()
	iss, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: seriesID, ComicvineIssueID: cvID, IssueNumberRaw: raw,
		IssueNumberSort: sort, Status: "Wanted", CreatedAt: created,
	})
	require.NoError(t, err)
	if attempts > 0 {
		require.NoError(t, repos.Issue.UpdateStatus(ctx, iss.ID, "Wanted", attempts))
	}
	return iss.ID
}

func TestAutoSearchSweepEnqueuesCappedBatchFewestFirst(t *testing.T) {
	t.Parallel()
	// No gateway candidates needed — the sweep only enqueues.
	svc, repos, firstIssue := newSearchService(t, nil)
	ctx := context.Background()

	// The fixture already seeded one Wanted issue (firstIssue) with 0 attempts. Add two
	// more on the same series (derived from the fixture issue) with higher attempts.
	fixture, err := repos.Issue.GetByID(ctx, firstIssue)
	require.NoError(t, err)
	seriesID := fixture.SeriesID
	mid := seedWanted(t, repos, seriesID, 9002, "8", 8.0, "2026-01-02T00:00:00Z", 2)
	seedWanted(t, repos, seriesID, 9003, "9", 9.0, "2026-01-03T00:00:00Z", 9) // cold (>= cap 5)

	enq := &fakeEnqueuer{}
	svc.SetEnqueuer(enq)
	require.NoError(t, svc.RunAutoSearchSweep(ctx))

	// Cold issue excluded; fewest-attempts-first ordering.
	require.Equal(t, []int64{firstIssue, mid}, enq.enqueued)
}

func TestRunSearchIssueNoResultIncrementsAttempts(t *testing.T) {
	t.Parallel()
	// A below-floor candidate (lone pdf, unknown-ish) does not clear the acceptance floor.
	svc, repos, issueID := newSearchService(t, []indexer.Candidate{
		svcCand("newznab", "rk-pdf", "pdf", 5*1024*1024),
	})
	ctx := context.Background()

	before, err := repos.Issue.GetByID(ctx, issueID)
	require.NoError(t, err)

	require.NoError(t, svc.RunSearchIssue(ctx, issueID))

	after, err := repos.Issue.GetByID(ctx, issueID)
	require.NoError(t, err)
	require.Equal(t, before.SearchAttempts+1, after.SearchAttempts, "no-result search increments attempts")
	require.Equal(t, "Wanted", after.Status, "no auto-grab when below floor")

	// A searched event was still written (D-04).
	events, err := repos.IssueEvents.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "searched", events[0].EventType)
}

func TestRunSearchIssueClearingCandidateAutoGrabs(t *testing.T) {
	t.Parallel()
	// A cbz at neutral size clears the floor and is auto-grabbed.
	svc, repos, issueID := newSearchService(t, []indexer.Candidate{
		svcCand("newznab", "rk-cbz", "cbz", 30*1024*1024),
	})
	ctx := context.Background()

	require.NoError(t, svc.RunSearchIssue(ctx, issueID))

	iss, err := repos.Issue.GetByID(ctx, issueID)
	require.NoError(t, err)
	require.Equal(t, "Snatched", iss.Status, "clearing candidate auto-grabs to Snatched")

	// searched + snatched events written.
	events, err := repos.IssueEvents.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	types := map[string]bool{}
	for _, e := range events {
		types[e.EventType] = true
	}
	require.True(t, types["searched"])
	require.True(t, types["snatched"])

	dls, err := repos.Downloads.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, dls, 1)
	require.Equal(t, "rk-cbz", dls[0].ReleaseKey)
}
