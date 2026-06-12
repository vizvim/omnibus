package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/repository"
)

// seedIssueForDownload inserts a series + issue and returns the issue id, so download /
// event rows have a valid issue_id FK.
func seedIssueForDownload(t *testing.T, repos *repository.Repositories) int64 {
	t.Helper()
	ctx := context.Background()
	s, err := repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 4050, Name: "S", Status: "Active", CreatedAt: ts,
	})
	require.NoError(t, err)
	iss, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: s.ID, ComicvineIssueID: 9001, IssueNumberRaw: "7", IssueNumberSort: 7.0,
		Status: "Wanted", CreatedAt: ts,
	})
	require.NoError(t, err)
	return iss.ID
}

func TestDownloadUpsertIdempotent(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repos := repository.NewRepositories(d)
	ctx := context.Background()
	issueID := seedIssueForDownload(t, repos)

	in := repository.DownloadUpsert{
		IssueID: issueID, Provider: "sabnzbd", ReleaseKey: "rk-1",
		ReleaseTitle: "Saga #7", SizeBytes: 1234, Status: "Queued", ClientRef: "nzo_1",
	}
	first, err := repos.Downloads.Upsert(ctx, in, ts)
	require.NoError(t, err)

	// Same (provider, release_key, issue_id) → updates in place, single row (T-4-02).
	in.ClientRef = "nzo_2"
	second, err := repos.Downloads.Upsert(ctx, in, ts)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, "nzo_2", second.ClientRef)

	all, err := repos.Downloads.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, all, 1)
}

func TestIssueEventsListInOccurredAtOrder(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repos := repository.NewRepositories(d)
	ctx := context.Background()
	issueID := seedIssueForDownload(t, repos)

	_, err := repos.IssueEvents.Insert(ctx, repository.IssueEventInsert{
		IssueID: issueID, EventType: "searched", OccurredAt: "2026-01-01T00:00:00Z",
	})
	require.NoError(t, err)
	_, err = repos.IssueEvents.Insert(ctx, repository.IssueEventInsert{
		IssueID: issueID, EventType: "candidate-selected", OccurredAt: "2026-01-01T00:01:00Z",
	})
	require.NoError(t, err)
	_, err = repos.IssueEvents.Insert(ctx, repository.IssueEventInsert{
		IssueID: issueID, EventType: "snatched", PayloadJSON: `{"release_key":"rk"}`,
		OccurredAt: "2026-01-01T00:02:00Z",
	})
	require.NoError(t, err)

	events, err := repos.IssueEvents.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.Equal(t, "searched", events[0].EventType)
	require.Equal(t, "candidate-selected", events[1].EventType)
	require.Equal(t, "snatched", events[2].EventType)
	require.JSONEq(t, `{"release_key":"rk"}`, events[2].PayloadJSON)
}
