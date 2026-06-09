package jobs

import (
	"context"

	"github.com/riverqueue/river"
)

// RefreshRunner is the inverted dependency the refresh worker calls to run the
// conditional metadata refresh. The series service satisfies it via its exported
// RunRefresh method. Mirrors ImportRunner so internal/jobs stays free of any import of
// internal/service/series (no import cycle).
type RefreshRunner interface {
	RunRefresh(ctx context.Context, seriesID, comicvineVolumeID int64) error
}

// RefreshWorker is the River worker for RefreshArgs. It delegates to a RefreshRunner so
// the durable job wraps the service's conditional refresh logic.
type RefreshWorker struct {
	river.WorkerDefaults[RefreshArgs]
	runner RefreshRunner
}

// NewRefreshWorker builds the refresh worker over the given runner.
func NewRefreshWorker(runner RefreshRunner) *RefreshWorker {
	return &RefreshWorker{runner: runner}
}

// Work runs the conditional refresh for the job's series, returning the runner's error
// unchanged so River's retry/backoff applies under the max-attempts cap.
func (w *RefreshWorker) Work(ctx context.Context, job *river.Job[RefreshArgs]) error {
	return w.runner.RunRefresh(ctx, job.Args.SeriesID, job.Args.ComicvineVolumeID)
}
