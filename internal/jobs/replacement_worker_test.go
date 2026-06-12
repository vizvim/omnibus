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

// recordingReplacementRunner is a stub ReplacementRunner that records the issue ids its
// RunReplacement was invoked with and signals a channel for deterministic waits.
type recordingReplacementRunner struct {
	mu     sync.Mutex
	issues []int64
	called chan struct{}
}

func newRecordingReplacementRunner() *recordingReplacementRunner {
	return &recordingReplacementRunner{called: make(chan struct{}, 8)}
}

func (r *recordingReplacementRunner) RunReplacement(_ context.Context, issueID int64) error {
	r.mu.Lock()
	r.issues = append(r.issues, issueID)
	r.mu.Unlock()
	r.called <- struct{}{}
	return nil
}

func (r *recordingReplacementRunner) snapshot() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int64, len(r.issues))
	copy(out, r.issues)
	return out
}

// TestEnqueueReplacementSearchRunsWorker proves an enqueued ReplacementSearchArgs is worked:
// the ReplacementWorker delegates to RunReplacement with the expected issue id (05-04).
func TestEnqueueReplacementSearchRunsWorker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "replacement.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	runner := newRecordingReplacementRunner()
	workers := river.NewWorkers()
	river.AddWorker(workers, jobs.NewReplacementWorker(runner))

	c, err := jobs.New(ctx, d.Write, d.Read, 2, 0, 0, 0, 0, testLogger(), workers)
	require.NoError(t, err)
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() {
		sc, cn := context.WithTimeout(context.Background(), 10*time.Second)
		defer cn()
		_ = c.Stop(sc)
	})

	require.NoError(t, c.EnqueueReplacementSearch(ctx, 42))

	select {
	case <-runner.called:
	case <-time.After(10 * time.Second):
		t.Fatal("replacement worker was not invoked within the deadline")
	}
	require.Equal(t, []int64{42}, runner.snapshot())
}

// TestEnqueueReplacementSearchIsUnique proves duplicate replacement enqueues for the same
// issue, while one is pending, collapse into a single job (D-12/D-14 coalescing). The client
// is constructed but NOT started so the first job stays pending.
func TestEnqueueReplacementSearchIsUnique(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "replacement_unique.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	workers := river.NewWorkers()
	river.AddWorker(workers, jobs.NewReplacementWorker(newRecordingReplacementRunner()))

	c, err := jobs.New(ctx, d.Write, d.Read, 2, 0, 0, 0, 0, testLogger(), workers)
	require.NoError(t, err)

	require.NoError(t, c.EnqueueReplacementSearch(ctx, 5))
	require.NoError(t, c.EnqueueReplacementSearch(ctx, 5))

	var pending int
	require.NoError(t, d.Read.QueryRowContext(ctx,
		"SELECT count(*) FROM river_job WHERE kind = 'replacement_search'").Scan(&pending))
	require.Equal(t, 1, pending, "duplicate replacement enqueue must collapse to a single job")
}
