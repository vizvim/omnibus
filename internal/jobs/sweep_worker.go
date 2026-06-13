package jobs

import (
	"context"

	"github.com/riverqueue/river"
)

// StaleSweepRunner is the inverted dependency the sweep worker calls to run the
// scheduled stale-only refresh sweep. The series service satisfies it via RunSweep.
// Mirrors the Import/RefreshRunner inversion so internal/jobs stays free of any import
// of internal/series.
type StaleSweepRunner interface {
	RunSweep(ctx context.Context) error
}

// SweepWorker is the River worker for SweepArgs (the periodic sweep). It delegates to a
// StaleSweepRunner.
type SweepWorker struct {
	river.WorkerDefaults[SweepArgs]
	runner StaleSweepRunner
}

// NewSweepWorker builds the sweep worker over the given runner.
func NewSweepWorker(runner StaleSweepRunner) *SweepWorker {
	return &SweepWorker{runner: runner}
}

// Work runs one sweep tick, returning the runner's error unchanged so River's
// retry/backoff applies.
func (w *SweepWorker) Work(ctx context.Context, _ *river.Job[SweepArgs]) error {
	return w.runner.RunSweep(ctx)
}
