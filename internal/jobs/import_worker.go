package jobs

import (
	"context"

	"github.com/riverqueue/river"
)

// ImportRunner is the inverted dependency the import worker calls to run the actual
// (idempotent) series import. The series service satisfies it via its exported
// RunImport method. Defining the interface here — and having the worker depend on it
// rather than on the concrete series service — keeps internal/jobs free of any import
// of internal/series, avoiding an import cycle.
type ImportRunner interface {
	RunImport(ctx context.Context, seriesID, comicvineVolumeID int64) error
}

// ImportWorker is the River worker for ImportArgs. It delegates to an ImportRunner so
// the durable job wraps (rather than reimplements) the existing idempotent import.
type ImportWorker struct {
	river.WorkerDefaults[ImportArgs]
	runner ImportRunner
}

// NewImportWorker builds the import worker over the given runner.
func NewImportWorker(runner ImportRunner) *ImportWorker {
	return &ImportWorker{runner: runner}
}

// Work runs the import for the job's series. It returns the runner's error unchanged
// so River's default retry/backoff applies under the max-attempts cap. On context
// cancellation during shutdown drain it returns ctx.Err(), leaving the job for
// recovery — re-running the idempotent import is safe (D-03 / D-10).
func (w *ImportWorker) Work(ctx context.Context, job *river.Job[ImportArgs]) error {
	return w.runner.RunImport(ctx, job.Args.SeriesID, job.Args.ComicvineVolumeID)
}
