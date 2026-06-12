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

func TestSearchAndGrabReturnsGrabbedOutcome(t *testing.T) {
	t.Parallel()
	// A cbz at neutral size clears the floor and is auto-grabbed; the outcome reports it.
	svc, _, issueID := newSearchService(t, []indexer.Candidate{
		svcCand("newznab", "rk-cbz", "cbz", 30*1024*1024),
	})

	outcome, err := svc.SearchAndGrab(context.Background(), issueID)
	require.NoError(t, err)
	require.True(t, outcome.Grabbed)
	require.NotEmpty(t, outcome.Title, "grabbed outcome carries the release title")
	require.Empty(t, outcome.FloorReason)
}

func TestSearchAndGrabReturnsFloorReasonWhenNothingAcceptable(t *testing.T) {
	t.Parallel()
	// A lone below-floor pdf clears nothing; the outcome reports why.
	svc, _, issueID := newSearchService(t, []indexer.Candidate{
		svcCand("newznab", "rk-pdf", "pdf", 5*1024*1024),
	})

	outcome, err := svc.SearchAndGrab(context.Background(), issueID)
	require.NoError(t, err)
	require.False(t, outcome.Grabbed)
	require.Empty(t, outcome.Title)
	require.NotEmpty(t, outcome.FloorReason, "non-acceptable outcome carries the floor reason")
}

// TestReplacementDedup: a release that already Failed for an issue (a dead key in the
// downloads table) is excluded from the next search pipeline WITHOUT a blacklist row
// (D-12 loop-prevention — dedup against downloads, not blacklists). The dead key is stored
// with the download-provider kind ("sabnzbd"); blacklistFor translates it back to the
// indexer kind ("newznab") so it matches the candidate the pipeline sees.
func TestReplacementDedup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// The only candidate would otherwise clear the floor and be auto-grabbed.
	svc, repos, issueID := newSearchService(t, []indexer.Candidate{
		svcCand("newznab", "rk-cbz", "cbz", 30*1024*1024),
	})

	// Record the release as already Failed for this issue (a dead download row). The
	// downloads table stores the download-provider kind, not the indexer kind.
	_, err := repos.Downloads.Upsert(ctx, repository.DownloadUpsert{
		IssueID: issueID, Provider: "sabnzbd", ReleaseKey: "rk-cbz", Status: "Failed",
	}, "2026-01-01T00:00:00Z")
	require.NoError(t, err)

	// No blacklist row exists — the exclusion must come purely from the dead-download dedup.
	bl, err := repos.Blacklist.ListForIssue(ctx, issueID)
	require.NoError(t, err)
	require.Empty(t, bl, "no blacklist row: dedup is via the downloads table (D-12)")

	require.NoError(t, svc.RunSearchIssue(ctx, issueID))

	iss, err := repos.Issue.GetByID(ctx, issueID)
	require.NoError(t, err)
	require.Equal(t, "Wanted", iss.Status, "the dead release is excluded, so nothing is grabbed")

	// The searched event records the rejection reason as blacklisted (excluded).
	events, err := repos.IssueEvents.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "searched", events[0].EventType)
	require.Contains(t, events[0].PayloadJSON, "blacklisted")
}

// TestBlacklistIssue: a user-blacklisted release for an issue is excluded from the search
// pipeline (D-11), even though it would otherwise clear the floor.
func TestBlacklistIssue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, repos, issueID := newSearchService(t, []indexer.Candidate{
		svcCand("newznab", "rk-cbz", "cbz", 30*1024*1024),
	})

	// Blacklist the only clearing candidate (stored with the indexer kind it carries).
	require.NoError(t, repos.Blacklist.Add(ctx, repository.BlacklistAdd{
		IssueID: issueID, Provider: "newznab", ReleaseKey: "rk-cbz", Reason: "user blacklist",
	}, "2026-01-01T00:00:00Z"))

	require.NoError(t, svc.RunSearchIssue(ctx, issueID))

	iss, err := repos.Issue.GetByID(ctx, issueID)
	require.NoError(t, err)
	require.Equal(t, "Wanted", iss.Status, "a blacklisted release is excluded, so nothing is grabbed")

	dls, err := repos.Downloads.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Empty(t, dls, "the blacklisted release is never grabbed")
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
