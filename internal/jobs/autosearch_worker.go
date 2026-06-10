package jobs

import (
	"context"

	"github.com/riverqueue/river"
)

// AutoSearchRunner is the inverted dependency the auto-search workers call. The search
// service satisfies it via RunAutoSearchSweep + RunSearchIssue. Keeping it here (rather
// than importing internal/service/search) keeps internal/jobs free of service imports,
// mirroring StaleSweepRunner.
type AutoSearchRunner interface {
	// RunAutoSearchSweep loads the bounded Wanted batch and enqueues one SearchIssue job
	// per issue via the passed enqueuer (D-08/D-10).
	RunAutoSearchSweep(ctx context.Context, enqueuer SearchEnqueuer) error
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
// auto-search sweep). It delegates to an AutoSearchRunner, passing the enqueuer it holds
// so the runner stays free of any job-engine type.
type AutoSearchSweepWorker struct {
	river.WorkerDefaults[AutoSearchSweepArgs]
	runner   AutoSearchRunner
	enqueuer SearchEnqueuer
}

// NewAutoSearchSweepWorker builds the sweep worker over a runner and the enqueuer the
// sweep fans out through.
func NewAutoSearchSweepWorker(runner AutoSearchRunner, enqueuer SearchEnqueuer) *AutoSearchSweepWorker {
	return &AutoSearchSweepWorker{runner: runner, enqueuer: enqueuer}
}

// Work runs one auto-search sweep tick, returning the runner's error unchanged so
// River's retry/backoff applies.
func (w *AutoSearchSweepWorker) Work(ctx context.Context, _ *river.Job[AutoSearchSweepArgs]) error {
	return w.runner.RunAutoSearchSweep(ctx, w.enqueuer)
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
