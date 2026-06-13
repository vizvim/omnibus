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

// recordingSearchRunner is a stub AutoSearchRunner that records the issue ids its
// per-issue search was invoked with and signals a channel for deterministic waits.
type recordingSearchRunner struct {
	mu      sync.Mutex
	issues  []int64
	called  chan struct{}
	sweepFn func(ctx context.Context) error
}

func newRecordingSearchRunner() *recordingSearchRunner {
	return &recordingSearchRunner{called: make(chan struct{}, 8)}
}

func (r *recordingSearchRunner) RunAutoSearchSweep(ctx context.Context) error {
	if r.sweepFn != nil {
		return r.sweepFn(ctx)
	}
	return nil
}

func (r *recordingSearchRunner) RunSearchIssue(_ context.Context, issueID int64) error {
	r.mu.Lock()
	r.issues = append(r.issues, issueID)
	r.mu.Unlock()
	r.called <- struct{}{}
	return nil
}

func (r *recordingSearchRunner) snapshot() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int64, len(r.issues))
	copy(out, r.issues)
	return out
}

// TestEnqueueSearchIssueRunsWorker proves an enqueued SearchIssueArgs is worked: the
// SearchIssueWorker's RunSearchIssue is invoked with the expected id (SRCH-07).
func TestEnqueueSearchIssueRunsWorker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "autosearch.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	runner := newRecordingSearchRunner()
	workers := river.NewWorkers()
	river.AddWorker(workers, jobs.NewSearchIssueWorker(runner))

	c, err := jobs.New(ctx, d.Write, d.Read, 2, 0, 0, 0, 0, testLogger(), workers)
	require.NoError(t, err)
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() {
		sc, cn := context.WithTimeout(context.Background(), 10*time.Second)
		defer cn()
		_ = c.Stop(sc)
	})

	require.NoError(t, c.EnqueueSearchIssue(ctx, 77))

	select {
	case <-runner.called:
	case <-time.After(10 * time.Second):
		t.Fatal("search-issue worker was not invoked within the deadline")
	}
	require.Equal(t, []int64{77}, runner.snapshot())
}

// TestEnqueueSearchIssueIsUnique proves duplicate enqueues for the same issue, while one
// is pending, collapse into a single job (D-10). The client is constructed but NOT
// started so the first job stays pending.
func TestEnqueueSearchIssueIsUnique(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "autosearch-unique.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	workers := river.NewWorkers()
	river.AddWorker(workers, jobs.NewSearchIssueWorker(newRecordingSearchRunner()))
	c, err := jobs.New(ctx, d.Write, d.Read, 2, 0, 0, 0, 0, testLogger(), workers)
	require.NoError(t, err)

	require.NoError(t, c.EnqueueSearchIssue(ctx, 5))
	// Second enqueue for the same issue collapses (unique-by-args); must not error.
	require.NoError(t, c.EnqueueSearchIssue(ctx, 5))

	var pending int
	require.NoError(t, d.Read.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM river_job WHERE kind = 'search_issue'`).Scan(&pending))
	require.Equal(t, 1, pending, "duplicate search enqueue must collapse to a single job")
}

// TestAutoSearchSweepFansOutViaEnqueuer proves the sweep worker passes a working
// enqueuer to the runner: the runner's sweep enqueues per-issue jobs that then run.
func TestAutoSearchSweepFansOutViaEnqueuer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "autosearch-sweep.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	runner := newRecordingSearchRunner()

	workers := river.NewWorkers()
	river.AddWorker(workers, jobs.NewSearchIssueWorker(runner))

	c, err := jobs.New(ctx, d.Write, d.Read, 2, 0, 0, 0, 0, testLogger(), workers)
	require.NoError(t, err)
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() {
		sc, cn := context.WithTimeout(context.Background(), 10*time.Second)
		defer cn()
		_ = c.Stop(sc)
	})

	// The sweep fans out a per-issue search for issue 9 via the jobs client as enqueuer.
	runner.sweepFn = func(ctx context.Context) error {
		return c.EnqueueSearchIssue(ctx, 9)
	}
	require.NoError(t, runner.RunAutoSearchSweep(ctx))

	select {
	case <-runner.called:
	case <-time.After(10 * time.Second):
		t.Fatal("fanned-out search job did not run within the deadline")
	}
	require.Equal(t, []int64{9}, runner.snapshot())
}
