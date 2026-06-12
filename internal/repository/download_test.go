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

func TestDownloadHistory(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repos := repository.NewRepositories(d)
	ctx := context.Background()
	issueID := seedIssueForDownload(t, repos)

	// Append-only history round-trips every field (DL-03).
	got, err := repos.Downloads.InsertHistory(ctx, repository.DownloadHistoryInsert{
		IssueID: issueID, Provider: "sabnzbd", ReleaseKey: "rk-1",
		Result: "completed", Detail: "/comics/Saga/Saga 07.cbz", OccurredAt: ts,
	})
	require.NoError(t, err)
	require.NotZero(t, got.ID)
	require.Equal(t, "sabnzbd", got.Provider)
	require.Equal(t, "rk-1", got.ReleaseKey)
	require.Equal(t, "completed", got.Result)
	require.Equal(t, "/comics/Saga/Saga 07.cbz", got.Detail)
	require.Equal(t, ts, got.OccurredAt)

	// A second, distinct outcome appends a NEW row (never updates in place).
	_, err = repos.Downloads.InsertHistory(ctx, repository.DownloadHistoryInsert{
		IssueID: issueID, Provider: "sabnzbd", ReleaseKey: "rk-1",
		Result: "failed", Detail: "unpack error", OccurredAt: "2026-01-02T00:00:00Z",
	})
	require.NoError(t, err)
}

func TestActiveDownloads(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repos := repository.NewRepositories(d)
	ctx := context.Background()
	issueID := seedIssueForDownload(t, repos)

	// One Queued, one Downloading (both active), one Completed (terminal, not active).
	queued, err := repos.Downloads.Upsert(ctx, repository.DownloadUpsert{
		IssueID: issueID, Provider: "sabnzbd", ReleaseKey: "rk-q", Status: "Queued", ClientRef: "nzo_q",
	}, ts)
	require.NoError(t, err)
	_, err = repos.Downloads.Upsert(ctx, repository.DownloadUpsert{
		IssueID: issueID, Provider: "sabnzbd", ReleaseKey: "rk-d", Status: "Downloading", ClientRef: "nzo_d",
	}, "2026-01-01T00:00:01Z")
	require.NoError(t, err)
	_, err = repos.Downloads.Upsert(ctx, repository.DownloadUpsert{
		IssueID: issueID, Provider: "sabnzbd", ReleaseKey: "rk-c", Status: "Completed", ClientRef: "nzo_c",
	}, ts)
	require.NoError(t, err)

	active, err := repos.Downloads.ListActive(ctx)
	require.NoError(t, err)
	require.Len(t, active, 2, "only Queued + Downloading are active")
	for _, row := range active {
		require.Contains(t, []string{"Queued", "Downloading"}, row.Status)
	}

	// UpdateStatus flips a row and bumps updated_at, returning the new row.
	updated, err := repos.Downloads.UpdateStatus(ctx, queued.ID, "Downloading", "2026-01-01T00:05:00Z")
	require.NoError(t, err)
	require.Equal(t, "Downloading", updated.Status)
	require.Equal(t, "2026-01-01T00:05:00Z", updated.UpdatedAt)
}

func TestDeadReleaseKeys(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repos := repository.NewRepositories(d)
	ctx := context.Background()
	issueID := seedIssueForDownload(t, repos)

	// Failed + Blacklisted rows are "dead"; Queued/Completed are not (D-12 dedup).
	_, err := repos.Downloads.Upsert(ctx, repository.DownloadUpsert{
		IssueID: issueID, Provider: "sabnzbd", ReleaseKey: "rk-failed", Status: "Failed", ClientRef: "x",
	}, ts)
	require.NoError(t, err)
	_, err = repos.Downloads.Upsert(ctx, repository.DownloadUpsert{
		IssueID: issueID, Provider: "getcomics", ReleaseKey: "rk-bl", Status: "Blacklisted", ClientRef: "y",
	}, ts)
	require.NoError(t, err)
	_, err = repos.Downloads.Upsert(ctx, repository.DownloadUpsert{
		IssueID: issueID, Provider: "sabnzbd", ReleaseKey: "rk-live", Status: "Queued", ClientRef: "z",
	}, ts)
	require.NoError(t, err)

	dead, err := repos.Downloads.ListDeadReleaseKeys(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, dead, 2)
	keys := map[string]string{}
	for _, k := range dead {
		keys[k.ReleaseKey] = k.Provider
	}
	require.Equal(t, "sabnzbd", keys["rk-failed"])
	require.Equal(t, "getcomics", keys["rk-bl"])
	require.NotContains(t, keys, "rk-live", "an active Queued release is not dead")
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
