package jobs

import (
	"context"

	"github.com/riverqueue/river"
)

// PollRunner is the inverted dependency the download-poll worker calls. The tracking
// service satisfies it via RunDownloadPoll. Declaring the interface here (rather than
// importing the service) keeps internal/jobs free of any service import, mirroring
// AutoSearchRunner/ImportRunner and avoiding an import cycle.
type PollRunner interface {
	// RunDownloadPoll reconciles every active (Queued/Downloading) download against its
	// client: progress events (DL-08), terminal completed/failed flips + history (DL-03),
	// and stall detection (DL-04).
	RunDownloadPoll(ctx context.Context) error
}

// DownloadPollWorker is the River worker for DownloadPollArgs (the periodic download poll).
// It delegates to a PollRunner.
type DownloadPollWorker struct {
	river.WorkerDefaults[DownloadPollArgs]
	runner PollRunner
}

// NewDownloadPollWorker builds the poll worker over the given runner.
func NewDownloadPollWorker(runner PollRunner) *DownloadPollWorker {
	return &DownloadPollWorker{runner: runner}
}

// Work runs one download-poll tick, returning the runner's error unchanged so River's
// retry/backoff applies. On shutdown drain a canceled context surfaces as the runner's
// ctx error, leaving the periodic job to resume next tick (the poll is idempotent).
func (w *DownloadPollWorker) Work(ctx context.Context, _ *river.Job[DownloadPollArgs]) error {
	return w.runner.RunDownloadPoll(ctx)
}
