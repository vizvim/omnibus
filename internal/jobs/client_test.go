package jobs_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/jobs"
)

// recordingRunner is a stub ImportRunner that records the ids it was invoked with and
// signals a channel so tests can wait deterministically (no unbounded sleeps).
type recordingRunner struct {
	mu     sync.Mutex
	calls  [][2]int64
	called chan struct{}
}

func newRecordingRunner() *recordingRunner {
	return &recordingRunner{called: make(chan struct{}, 8)}
}

func (r *recordingRunner) RunImport(_ context.Context, seriesID, comicvineVolumeID int64) error {
	r.mu.Lock()
	r.calls = append(r.calls, [2]int64{seriesID, comicvineVolumeID})
	r.mu.Unlock()
	r.called <- struct{}{}
	return nil
}

// RunRefresh lets recordingRunner double as a RefreshRunner for the dedup test.
func (r *recordingRunner) RunRefresh(_ context.Context, seriesID, comicvineVolumeID int64) error {
	r.mu.Lock()
	r.calls = append(r.calls, [2]int64{seriesID, comicvineVolumeID})
	r.mu.Unlock()
	r.called <- struct{}{}
	return nil
}

func (r *recordingRunner) snapshot() [][2]int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][2]int64, len(r.calls))
	copy(out, r.calls)
	return out
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// newClient builds a started jobs client against the SQLite file at path, wired to the
// given runner, and registers cleanup to stop it.
func newClient(ctx context.Context, t *testing.T, path string, runner jobs.ImportRunner) *jobs.Client {
	t.Helper()
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	workers := river.NewWorkers()
	river.AddWorker(workers, jobs.NewImportWorker(runner))

	c, err := jobs.New(ctx, d.Write, d.Read, 2, 0, testLogger(), workers)
	require.NoError(t, err)
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.Stop(stopCtx)
	})
	return c
}

// TestEnqueueImportRunsWorker proves an enqueued ImportArgs is worked: the worker's
// RunImport is invoked with the expected ids (JOBS-01: queued work runs).
func TestEnqueueImportRunsWorker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "jobs.db")

	runner := newRecordingRunner()
	c := newClient(ctx, t, path, runner)

	require.NoError(t, c.EnqueueImport(ctx, 42, 4050))

	select {
	case <-runner.called:
	case <-time.After(10 * time.Second):
		t.Fatal("import worker was not invoked within the deadline")
	}

	calls := runner.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, [2]int64{42, 4050}, calls[0])
}

// TestEnqueueRefreshIsUnique proves a duplicate refresh enqueue for the same series,
// while one is already pending, collapses into a no-op (River unique jobs, D-03). The
// client is constructed but NOT started, so the first job stays pending.
func TestEnqueueRefreshIsUnique(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "unique.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	workers := river.NewWorkers()
	river.AddWorker(workers, jobs.NewRefreshWorker(newRecordingRunner()))
	c, err := jobs.New(ctx, d.Write, d.Read, 2, 0, testLogger(), workers)
	require.NoError(t, err)

	require.NoError(t, c.EnqueueRefresh(ctx, 99, 4050))
	// Second enqueue for the same series is a no-op (unique-by-args); it must not error.
	require.NoError(t, c.EnqueueRefresh(ctx, 99, 4050))

	var pending int
	require.NoError(t, d.Read.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM river_job WHERE kind = 'refresh_series'`).Scan(&pending))
	require.Equal(t, 1, pending, "duplicate refresh enqueue must collapse to a single job")
}

// TestImportSurvivesRestart proves persistence across restart (JOBS-02): a job
// enqueued by one client (stopped before it can run) is worked by a fresh client
// constructed against the SAME db file.
func TestImportSurvivesRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "restart.db")

	// First client: enqueue, but stop immediately so the job does not run here.
	require.NoError(t, db.Migrate(ctx, path))
	d1, err := db.Open(ctx, path)
	require.NoError(t, err)

	enqueueWorkers := river.NewWorkers()
	river.AddWorker(enqueueWorkers, jobs.NewImportWorker(newRecordingRunner()))
	c1, err := jobs.New(ctx, d1.Write, d1.Read, 2, 0, testLogger(), enqueueWorkers)
	require.NoError(t, err)
	// Do NOT Start c1 — the job stays durably persisted (pending) without being worked.
	require.NoError(t, c1.EnqueueImport(ctx, 7, 1234))
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = c1.Stop(stopCtx)
	require.NoError(t, d1.Close())

	// Second client against the same file: it should pick up and work the persisted job.
	runner := newRecordingRunner()
	d2, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d2.Close() })
	workers := river.NewWorkers()
	river.AddWorker(workers, jobs.NewImportWorker(runner))
	c2, err := jobs.New(ctx, d2.Write, d2.Read, 2, 0, testLogger(), workers)
	require.NoError(t, err)
	require.NoError(t, c2.Start(ctx))
	t.Cleanup(func() {
		sc, cn := context.WithTimeout(context.Background(), 10*time.Second)
		defer cn()
		_ = c2.Stop(sc)
	})

	select {
	case <-runner.called:
	case <-time.After(10 * time.Second):
		t.Fatal("persisted job did not run on the second client (restart survival)")
	}
	calls := runner.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, [2]int64{7, 1234}, calls[0])
}
