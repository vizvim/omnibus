package jobs

import (
	"context"

	"github.com/riverqueue/river"
)

// AutoSearchRunner is the inverted dependency the auto-search workers call. The search
// service satisfies it via RunAutoSearchSweep + RunSearchIssue. Keeping it here (rather
// than importing internal/search) keeps internal/jobs free of service imports,
// mirroring StaleSweepRunner. The runner owns the enqueuer it fans out through (set on
// the service post-construction, exactly like the series service's enqueuer), so the
// worker stays free of any job-engine wiring and the service<->jobs cycle resolves the
// same way as the import/refresh path.
type AutoSearchRunner interface {
	// RunAutoSearchSweep loads the bounded Wanted batch and enqueues one SearchIssue job
	// per issue via the enqueuer the runner holds (D-08/D-10).
	RunAutoSearchSweep(ctx context.Context) error
	// RunSearchIssue runs the shared pipeline for one issue and either auto-grabs a
	// floor-clearing pick or increments search_attempts (D-02/D-09).
	RunSearchIssue(ctx context.Context, issueID int64) error
}

// SearchEnqueuer is the seam the sweep uses to fan out one-off search jobs. The jobs
// Client satisfies it via EnqueueSearchIssue. It is re-declared here (not imported from
// the service) so the worker's runner contract is self-contained.
type SearchEnqueuer interface {
	EnqueueSearchIssue(ctx context.Context, issueID int64) error
}

// AutoSearchSweepWorker is the River worker for AutoSearchSweepArgs (the periodic
// auto-search sweep). It delegates to an AutoSearchRunner.
type AutoSearchSweepWorker struct {
	river.WorkerDefaults[AutoSearchSweepArgs]
	runner AutoSearchRunner
}

// NewAutoSearchSweepWorker builds the sweep worker over the given runner.
func NewAutoSearchSweepWorker(runner AutoSearchRunner) *AutoSearchSweepWorker {
	return &AutoSearchSweepWorker{runner: runner}
}

// Work runs one auto-search sweep tick, returning the runner's error unchanged so
// River's retry/backoff applies.
func (w *AutoSearchSweepWorker) Work(ctx context.Context, _ *river.Job[AutoSearchSweepArgs]) error {
	return w.runner.RunAutoSearchSweep(ctx)
}

// SearchIssueWorker is the River worker for SearchIssueArgs (the one-off per-issue
// auto-search). It delegates to the same AutoSearchRunner.
type SearchIssueWorker struct {
	river.WorkerDefaults[SearchIssueArgs]
	runner AutoSearchRunner
}

// NewSearchIssueWorker builds the per-issue search worker over the given runner.
func NewSearchIssueWorker(runner AutoSearchRunner) *SearchIssueWorker {
	return &SearchIssueWorker{runner: runner}
}

// Work runs the auto-search for the job's issue, returning the runner's error unchanged.
func (w *SearchIssueWorker) Work(ctx context.Context, job *river.Job[SearchIssueArgs]) error {
	return w.runner.RunSearchIssue(ctx, job.Args.IssueID)
}
