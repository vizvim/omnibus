package jobs

import (
	"context"

	"github.com/riverqueue/river"
)

// PostProcessRunner is the inverted dependency the post-process worker calls. The
// postprocess service satisfies it via Process. Declaring the interface here (rather than
// importing internal/service/postprocess) keeps internal/jobs free of any service import,
// mirroring ImportRunner/PollRunner/ReplacementRunner and avoiding an import cycle.
type PostProcessRunner interface {
	// RunPostProcess validates, renames, imports, and files a completed download, flipping
	// the issue to Downloaded (or routing a corrupt archive into the D-16 replacement path).
	// It is idempotent: a re-run on an already-Downloaded issue is a safe no-op.
	RunPostProcess(ctx context.Context, downloadID int64) error
}

// PostProcessWorker is the River worker for PostProcessArgs (reactive on a Completed
// download). It delegates to a PostProcessRunner.
type PostProcessWorker struct {
	river.WorkerDefaults[PostProcessArgs]
	runner PostProcessRunner
}

// NewPostProcessWorker builds the post-process worker over the given runner.
func NewPostProcessWorker(runner PostProcessRunner) *PostProcessWorker {
	return &PostProcessWorker{runner: runner}
}

// Work runs post-processing for the job's completed download, returning the runner's error
// unchanged so River's retry/backoff applies under the max-attempts cap. The work is
// idempotent (Process no-ops on an already-Downloaded issue), so a retried job is safe.
func (w *PostProcessWorker) Work(ctx context.Context, job *river.Job[PostProcessArgs]) error {
	return w.runner.RunPostProcess(ctx, job.Args.DownloadID)
}
