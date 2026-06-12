package jobs_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/jobs"
)

// recordingPostProcessRunner is a stub PostProcessRunner that records the download ids its
// RunPostProcess was invoked with and signals a channel for deterministic waits.
type recordingPostProcessRunner struct {
	mu        sync.Mutex
	downloads []int64
	called    chan struct{}
}

func newRecordingPostProcessRunner() *recordingPostProcessRunner {
	return &recordingPostProcessRunner{called: make(chan struct{}, 8)}
}

func (r *recordingPostProcessRunner) RunPostProcess(_ context.Context, downloadID int64) error {
	r.mu.Lock()
	r.downloads = append(r.downloads, downloadID)
	r.mu.Unlock()
	r.called <- struct{}{}
	return nil
}

func (r *recordingPostProcessRunner) snapshot() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int64, len(r.downloads))
	copy(out, r.downloads)
	return out
}

// TestEnqueuePostProcessRunsWorker proves an enqueued PostProcessArgs is worked: the
// PostProcessWorker delegates to RunPostProcess with the expected download id (05-03).
func TestEnqueuePostProcessRunsWorker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "postprocess.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	runner := newRecordingPostProcessRunner()
	workers := river.NewWorkers()
	river.AddWorker(workers, jobs.NewPostProcessWorker(runner))

	c, err := jobs.New(ctx, d.Write, d.Read, 2, 0, 0, 0, 0, testLogger(), workers)
	require.NoError(t, err)
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() {
		sc, cn := context.WithTimeout(context.Background(), 10*time.Second)
		defer cn()
		_ = c.Stop(sc)
	})

	require.NoError(t, c.EnqueuePostProcess(ctx, 99))

	select {
	case <-runner.called:
	case <-time.After(10 * time.Second):
		t.Fatal("post-process worker was not invoked within the deadline")
	}
	require.Equal(t, []int64{99}, runner.snapshot())
}

// TestEnqueuePostProcessIsUnique proves duplicate post-process enqueues for the same download,
// while one is pending, collapse into a single job (idempotent re-run coalescing). The client
// is constructed but NOT started so the first job stays pending.
func TestEnqueuePostProcessIsUnique(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "postprocess_unique.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	workers := river.NewWorkers()
	river.AddWorker(workers, jobs.NewPostProcessWorker(newRecordingPostProcessRunner()))

	c, err := jobs.New(ctx, d.Write, d.Read, 2, 0, 0, 0, 0, testLogger(), workers)
	require.NoError(t, err)

	require.NoError(t, c.EnqueuePostProcess(ctx, 7))
	require.NoError(t, c.EnqueuePostProcess(ctx, 7))

	var pending int
	require.NoError(t, d.Read.QueryRowContext(ctx,
		"SELECT count(*) FROM river_job WHERE kind = 'post_process'").Scan(&pending))
	require.Equal(t, 1, pending, "duplicate post-process enqueue must collapse to a single job")
}
