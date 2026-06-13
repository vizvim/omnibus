package jobs_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/download"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/tracking"
)

const pollTS = "2026-01-01T00:00:00Z"

// newPollDB stands up a migrated temp SQLite DB + repositories for the poll tests.
func newPollDB(t *testing.T) *repository.Repositories {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "poll.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return repository.NewRepositories(d)
}

// seedPollIssue inserts a series + Snatched issue and returns its id.
func seedPollIssue(t *testing.T, repos *repository.Repositories) int64 {
	t.Helper()
	ctx := context.Background()
	s, err := repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 4050, Name: "Saga", Status: "Active", CreatedAt: pollTS,
	})
	require.NoError(t, err)
	iss, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: s.ID, ComicvineIssueID: 9001, IssueNumberRaw: "7", IssueNumberSort: 7.0,
		Status: "Snatched", CreatedAt: pollTS,
	})
	require.NoError(t, err)
	return iss.ID
}

// TestPollProgressEvent: a Queued download whose poll shows progress flips to Downloading
// and emits a download-progress timeline event (DL-08).
func TestPollProgressEvent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := newPollDB(t)
	issueID := seedPollIssue(t, repos)

	dl, err := repos.Downloads.Upsert(ctx, repository.DownloadUpsert{
		IssueID: issueID, Provider: "sabnzbd", ReleaseKey: "rk-1", Status: "Queued", ClientRef: "nzo_1",
	}, pollTS)
	require.NoError(t, err)

	fake := download.NewFakeProvider("sabnzbd", "nzo_1")
	fake.SetPoll("nzo_1", download.PollResult{Status: download.PollInProgress, ProgressPct: 42})

	svc := tracking.New(tracking.Deps{
		Repos:    repos,
		Trackers: map[string]download.Tracker{"sabnzbd": fake},
	})
	require.NoError(t, svc.RunDownloadPoll(ctx))

	got, err := repos.Downloads.Get(ctx, dl.ID)
	require.NoError(t, err)
	require.Equal(t, "Downloading", got.Status, "a progressing Queued download flips to Downloading")

	events, err := repos.IssueEvents.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "download-progress", events[0].EventType)
	require.Contains(t, events[0].PayloadJSON, "\"progress_pct\":42")
}

// TestPollTerminal: a Completed history result flips the row to Completed and records a
// download_history row (DL-03). Removal from the client (D-05) is NOT done here — it is
// deferred to post-process so it runs only after a confirmed import (T-05-11).
func TestPollTerminal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := newPollDB(t)
	issueID := seedPollIssue(t, repos)

	dl, err := repos.Downloads.Upsert(ctx, repository.DownloadUpsert{
		IssueID: issueID, Provider: "sabnzbd", ReleaseKey: "rk-1", Status: "Downloading", ClientRef: "nzo_done",
	}, pollTS)
	require.NoError(t, err)

	fake := download.NewFakeProvider("sabnzbd", "nzo_done")
	fake.SetPoll("nzo_done", download.PollResult{
		Status: download.PollCompleted, StoragePath: "/downloads/Saga 07", ProgressPct: 100,
	})

	svc := tracking.New(tracking.Deps{
		Repos:    repos,
		Trackers: map[string]download.Tracker{"sabnzbd": fake},
	})
	require.NoError(t, svc.RunDownloadPoll(ctx))

	got, err := repos.Downloads.Get(ctx, dl.ID)
	require.NoError(t, err)
	require.Equal(t, "Completed", got.Status)
	require.Empty(t, fake.Removed, "poll does NOT remove at SAB-completion; removal is deferred to post-process (T-05-11)")

	// History recorded the storage path (DL-03). ListDeadReleaseKeys excludes completed, so
	// assert via the absence of a dead key and the presence of a completed-status terminal row.
	dead, err := repos.Downloads.ListDeadReleaseKeys(ctx, issueID)
	require.NoError(t, err)
	require.Empty(t, dead, "a completed release is not dead")
}

// TestPollStall: a Downloading download whose last-progress timestamp is older than the
// stall window with no byte progress is reaped as Failed (DL-04 stall branch).
func TestPollStall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repos := newPollDB(t)
	issueID := seedPollIssue(t, repos)

	// updated_at is well in the past; the poll reports not-ready (no movement).
	staleTS := "2020-01-01T00:00:00Z"
	dl, err := repos.Downloads.Upsert(ctx, repository.DownloadUpsert{
		IssueID: issueID, Provider: "sabnzbd", ReleaseKey: "rk-1", Status: "Downloading", ClientRef: "nzo_stuck",
	}, staleTS)
	require.NoError(t, err)

	fake := download.NewFakeProvider("sabnzbd", "nzo_stuck")
	fake.SetPoll("nzo_stuck", download.PollResult{Status: download.PollNotReady})

	svc := tracking.New(tracking.Deps{
		Repos:       repos,
		Trackers:    map[string]download.Tracker{"sabnzbd": fake},
		StallWindow: 30 * time.Minute,
		Now:         func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	require.NoError(t, svc.RunDownloadPoll(ctx))

	got, err := repos.Downloads.Get(ctx, dl.ID)
	require.NoError(t, err)
	require.Equal(t, "Failed", got.Status, "a stalled Downloading row is reaped Failed")

	dead, err := repos.Downloads.ListDeadReleaseKeys(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, dead, 1, "the failed release is now a dead key (D-12)")

	events, err := repos.IssueEvents.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "failed", events[0].EventType)
	require.Contains(t, events[0].PayloadJSON, "stall")
}
