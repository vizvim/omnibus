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

// recordingDDLFetchRunner is a stub DDLFetchRunner that records the download ids its
// RunDDLFetch was invoked with and signals a channel for deterministic waits.
type recordingDDLFetchRunner struct {
	mu        sync.Mutex
	downloads []int64
	called    chan struct{}
}

func newRecordingDDLFetchRunner() *recordingDDLFetchRunner {
	return &recordingDDLFetchRunner{called: make(chan struct{}, 8)}
}

func (r *recordingDDLFetchRunner) RunDDLFetch(_ context.Context, downloadID int64) error {
	r.mu.Lock()
	r.downloads = append(r.downloads, downloadID)
	r.mu.Unlock()
	r.called <- struct{}{}
	return nil
}

func (r *recordingDDLFetchRunner) snapshot() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int64, len(r.downloads))
	copy(out, r.downloads)
	return out
}

// TestEnqueueDDLFetchRunsWorker proves an enqueued DDLFetchArgs is worked: the DDLFetchWorker
// delegates to RunDDLFetch with the expected download id, on the dedicated "ddl" queue (D-03).
func TestEnqueueDDLFetchRunsWorker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ddlfetch.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	runner := newRecordingDDLFetchRunner()
	workers := river.NewWorkers()
	river.AddWorker(workers, jobs.NewDDLFetchWorker(runner))

	c, err := jobs.New(ctx, d.Write, d.Read, 2, 0, 0, 0, 0, testLogger(), workers)
	require.NoError(t, err)
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() {
		sc, cn := context.WithTimeout(context.Background(), 10*time.Second)
		defer cn()
		_ = c.Stop(sc)
	})

	require.NoError(t, c.EnqueueDDLFetch(ctx, 42))

	select {
	case <-runner.called:
	case <-time.After(10 * time.Second):
		t.Fatal("ddl-fetch worker was not invoked within the deadline")
	}
	require.Equal(t, []int64{42}, runner.snapshot())

	// The job landed on the serialized "ddl" queue (MaxWorkers:1), not the default queue.
	var queue string
	require.NoError(t, d.Read.QueryRowContext(ctx,
		"SELECT queue FROM river_job WHERE kind = 'ddl_fetch' LIMIT 1").Scan(&queue))
	require.Equal(t, "ddl", queue)
}

// TestEnqueueDDLFetchIsUnique proves duplicate DDL-fetch enqueues for the same download, while
// one is pending, collapse into a single job (so a re-grab does not start a second stream).
func TestEnqueueDDLFetchIsUnique(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ddlfetch_unique.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	workers := river.NewWorkers()
	river.AddWorker(workers, jobs.NewDDLFetchWorker(newRecordingDDLFetchRunner()))

	c, err := jobs.New(ctx, d.Write, d.Read, 2, 0, 0, 0, 0, testLogger(), workers)
	require.NoError(t, err)

	require.NoError(t, c.EnqueueDDLFetch(ctx, 7))
	require.NoError(t, c.EnqueueDDLFetch(ctx, 7))

	var pending int
	require.NoError(t, d.Read.QueryRowContext(ctx,
		"SELECT count(*) FROM river_job WHERE kind = 'ddl_fetch'").Scan(&pending))
	require.Equal(t, 1, pending, "duplicate ddl-fetch enqueue must collapse to a single job")
}
