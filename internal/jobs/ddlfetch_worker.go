package jobs

import (
	"context"

	"github.com/riverqueue/river"
)

// DDLFetchRunner is the inverted dependency the DDL-fetch worker calls. The tracking service
// satisfies it via RunDDLFetch: resolve+stream the GetComics mirror, then flip the download
// Completed and enqueue post-process (D-03), or mark it Failed and enqueue a replacement on
// mirror exhaustion (D-04). Declaring the interface here (rather than importing the tracking
// service) keeps internal/jobs free of any service import, mirroring PollRunner/PostProcessRunner.
type DDLFetchRunner interface {
	// RunDDLFetch fetches a GetComics DDL download to completion and feeds it into the shared
	// post-process path (or the shared failure path on mirror exhaustion). It is idempotent: a
	// re-run on an already-terminal download is a safe no-op.
	RunDDLFetch(ctx context.Context, downloadID int64) error
}

// DDLFetchWorker is the River worker for DDLFetchArgs. It runs on the serialized "ddl" queue
// (MaxWorkers:1) and delegates to a DDLFetchRunner.
type DDLFetchWorker struct {
	river.WorkerDefaults[DDLFetchArgs]
	runner DDLFetchRunner
}

// NewDDLFetchWorker builds the DDL-fetch worker over the given runner.
func NewDDLFetchWorker(runner DDLFetchRunner) *DDLFetchWorker {
	return &DDLFetchWorker{runner: runner}
}

// Work runs the DDL fetch for the job's download, returning the runner's error unchanged so
// River's retry/backoff applies under the max-attempts cap. The work is idempotent (a re-run
// on an already-terminal download is a no-op), so a retried job is safe.
func (w *DDLFetchWorker) Work(ctx context.Context, job *river.Job[DDLFetchArgs]) error {
	return w.runner.RunDDLFetch(ctx, job.Args.DownloadID)
}
