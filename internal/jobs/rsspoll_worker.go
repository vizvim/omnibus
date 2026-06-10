package jobs

import (
	"context"

	"github.com/riverqueue/river"
)

// RSSPollRunner is the inverted dependency the RSS poll worker calls. The search service
// satisfies it via RunRSSPoll. Declared here so internal/jobs stays free of any import of
// internal/service/search.
type RSSPollRunner interface {
	RunRSSPoll(ctx context.Context) error
}

// RSSPollWorker is the River worker for RSSPollArgs (the periodic RSS poll). It delegates
// to an RSSPollRunner.
type RSSPollWorker struct {
	river.WorkerDefaults[RSSPollArgs]
	runner RSSPollRunner
}

// NewRSSPollWorker builds the RSS poll worker over the given runner.
func NewRSSPollWorker(runner RSSPollRunner) *RSSPollWorker {
	return &RSSPollWorker{runner: runner}
}

// Work runs one RSS poll tick, returning the runner's error unchanged so River's
// retry/backoff applies.
func (w *RSSPollWorker) Work(ctx context.Context, _ *river.Job[RSSPollArgs]) error {
	return w.runner.RunRSSPoll(ctx)
}
