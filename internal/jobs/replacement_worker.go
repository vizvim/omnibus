package jobs

import (
	"context"

	"github.com/riverqueue/river"
)

// ReplacementRunner is the inverted dependency the replacement-search worker calls. The
// search service satisfies it via RunReplacement. Declaring the interface here (rather than
// importing internal/service/search) keeps internal/jobs free of any service import,
// mirroring AutoSearchRunner/PollRunner and avoiding an import cycle.
type ReplacementRunner interface {
	// RunReplacement runs the attempt-capped replacement search for an issue whose download
	// failed or was blacklisted: it dedups against the dead-download keys (D-12), cools off
	// at the download_attempts cap (D-14), and otherwise re-runs the search pipeline.
	RunReplacement(ctx context.Context, issueID int64) error
}

// ReplacementWorker is the River worker for ReplacementSearchArgs (the reactive,
// attempt-capped replacement search). It delegates to a ReplacementRunner.
type ReplacementWorker struct {
	river.WorkerDefaults[ReplacementSearchArgs]
	runner ReplacementRunner
}

// NewReplacementWorker builds the replacement-search worker over the given runner.
func NewReplacementWorker(runner ReplacementRunner) *ReplacementWorker {
	return &ReplacementWorker{runner: runner}
}

// Work runs the replacement search for the job's issue, returning the runner's error
// unchanged so River's retry/backoff applies. The work is idempotent (the dedup excludes
// already-dead releases), so a retried job is safe.
func (w *ReplacementWorker) Work(ctx context.Context, job *river.Job[ReplacementSearchArgs]) error {
	return w.runner.RunReplacement(ctx, job.Args.IssueID)
}
