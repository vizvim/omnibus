package jobs_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/jobs"
)

// newHistoryClient builds a jobs client (which runs River's migrator, creating
// river_job) against a temp DB and returns the client plus the open DB for direct
// inserts.
func newHistoryClient(ctx context.Context, t *testing.T) (*jobs.Client, *db.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	workers := river.NewWorkers()
	river.AddWorker(workers, jobs.NewImportWorker(newRecordingRunner()))
	c, err := jobs.New(ctx, d.Write, d.Read, 2, testLogger(), workers)
	require.NoError(t, err)
	return c, d
}

// insertRiverJob inserts a row straight into River's table so a test can assert the
// state mapping and ordering deterministically.
func insertRiverJob(ctx context.Context, t *testing.T, d *db.DB, kind, state, attemptedAt, finalizedAt, errorsJSON string, attempt int) {
	t.Helper()
	const q = `INSERT INTO river_job (kind, state, attempt, max_attempts, attempted_at, finalized_at, errors)
	           VALUES (?, ?, ?, 25, ?, ?, ?)`
	var (
		at  any = attemptedAt
		fin any = finalizedAt
		er  any = errorsJSON
	)
	if attemptedAt == "" {
		at = nil
	}
	if finalizedAt == "" {
		fin = nil
	}
	if errorsJSON == "" {
		er = nil
	}
	_, err := d.Write.ExecContext(ctx, q, kind, state, attempt, at, fin, er)
	require.NoError(t, err)
}

// TestListJobRunsMapsStatesAndOrders seeds known river_job rows and asserts the five-
// state mapping, newest-first ordering, the failed-run error string, and the limit.
func TestListJobRunsMapsStatesAndOrders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, d := newHistoryClient(ctx, t)

	// Insert in ascending time order; ListJobRuns must return newest first.
	insertRiverJob(ctx, t, d, "import_series", "completed", "2026-01-01T00:00:00Z", "2026-01-01T00:01:00Z", "", 1)
	insertRiverJob(ctx, t, d, "refresh_series", "running", "2026-01-02T00:00:00Z", "", "", 1)
	insertRiverJob(ctx, t, d, "refresh_series", "discarded", "2026-01-03T00:00:00Z", "2026-01-03T00:02:00Z",
		`[{"attempt":1,"error":"comicvine 500"}]`, 3)

	runs, err := c.ListJobRuns(ctx, 50)
	require.NoError(t, err)
	require.Len(t, runs, 3)

	// Newest first: the discarded refresh (finalized 01-03) leads.
	require.Equal(t, jobs.JobRunFailed, runs[0].State)
	require.Equal(t, "refresh_series", runs[0].Kind)
	require.Equal(t, "comicvine 500", runs[0].Error, "failed run surfaces its latest error")

	require.Equal(t, jobs.JobRunRunning, runs[1].State)
	require.Equal(t, jobs.JobRunCompleted, runs[2].State)

	// Limit is honored.
	limited, err := c.ListJobRuns(ctx, 1)
	require.NoError(t, err)
	require.Len(t, limited, 1)
}

// TestListJobRunsMapsQueuedAndCancelled covers the remaining state mappings.
func TestListJobRunsMapsQueuedAndCancelled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, d := newHistoryClient(ctx, t)

	insertRiverJob(ctx, t, d, "import_series", "available", "", "", "", 0)
	insertRiverJob(ctx, t, d, "import_series", "scheduled", "", "", "", 0)
	insertRiverJob(ctx, t, d, "import_series", "cancelled", "2026-01-05T00:00:00Z", "2026-01-05T00:00:30Z", "", 1) //nolint:misspell // River's own state value

	runs, err := c.ListJobRuns(ctx, 50)
	require.NoError(t, err)
	require.Len(t, runs, 3)

	states := map[jobs.JobRunState]int{}
	for _, r := range runs {
		states[r.State]++
	}
	require.Equal(t, 2, states[jobs.JobRunQueued], "available + scheduled => queued")
	require.Equal(t, 1, states[jobs.JobRunCancelled])
}
