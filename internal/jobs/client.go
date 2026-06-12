// Package jobs owns the River background job engine. It wraps a single
// *river.Client[*sql.Tx] bound to the application's single-writer SQLite write pool
// (ADR 0002) so every River write serializes through the one writer. River applies
// its own schema via its own migrator (rivermigrate) — kept distinct from the
// golang-migrate application migrations (ADR 0003 / D-12). The engine is started and
// drained (Stop) as part of the app lifecycle (D-12 / JOBS-03).
package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riversqlite"
	"github.com/riverqueue/river/rivermigrate"
)

// defaultMaxAttempts bounds River's retry/backoff for a job before it is discarded.
const defaultMaxAttempts = 5

// Client wraps the River client and the queue configuration omnibus runs. All writes
// flow through the single-writer write pool; job-run history reads go through the
// read pool (ADR 0002).
type Client struct {
	river  *river.Client[*sql.Tx]
	readDB *sql.DB
	logger *slog.Logger
}

// New constructs the jobs client against the single-writer write pool (for River's own
// engine writes) and the read pool (for job-run history reads). It runs River's own
// schema migration (via rivermigrate, NOT golang-migrate) before the client is
// returned, so River's tables exist and stay distinct from the application migrations.
// workers is the registry of River workers to run; pass the number of concurrent
// worker goroutines via maxWorkers (single-user app → low). Each of sweepInterval,
// autoSearchInterval, and rssPollInterval, when > 0, registers a River native periodic
// job (the stale-refresh sweep, the auto-search sweep, and the RSS poll respectively) at
// that interval. None run on start, to avoid a burst of external calls right after boot.
// The returned client is not yet started — call Start to begin working jobs.
func New(ctx context.Context, writeDB, readDB *sql.DB, maxWorkers int, sweepInterval, autoSearchInterval, rssPollInterval time.Duration, logger *slog.Logger, workers *river.Workers) (*Client, error) {
	if maxWorkers < 1 {
		maxWorkers = 1
	}

	driver := riversqlite.New(writeDB)

	// Apply River's own schema (its migrator owns River's tables, parallel to the
	// golang-migrate application migrations — D-12) before constructing the client.
	migrator, err := rivermigrate.New(driver, &rivermigrate.Config{Logger: logger})
	if err != nil {
		return nil, fmt.Errorf("build river migrator: %w", err)
	}
	if _, migErr := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); migErr != nil {
		return nil, fmt.Errorf("apply river migrations: %w", migErr)
	}

	var periodicJobs []*river.PeriodicJob
	if sweepInterval > 0 {
		// River's native periodic scheduler runs the sweep (D-03) — no hand-rolled
		// time.Ticker. RunOnStart is false so boot does not trigger a ComicVine burst.
		periodicJobs = append(periodicJobs, river.NewPeriodicJob(
			river.PeriodicInterval(sweepInterval),
			func() (river.JobArgs, *river.InsertOpts) { return SweepArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: false},
		))
	}
	if autoSearchInterval > 0 {
		// The scheduled auto-search sweep (SRCH-07). RunOnStart false (no boot burst).
		periodicJobs = append(periodicJobs, river.NewPeriodicJob(
			river.PeriodicInterval(autoSearchInterval),
			func() (river.JobArgs, *river.InsertOpts) { return AutoSearchSweepArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: false},
		))
	}
	if rssPollInterval > 0 {
		// The scheduled RSS auto-grab poll (SRCH-08). RunOnStart false.
		periodicJobs = append(periodicJobs, river.NewPeriodicJob(
			river.PeriodicInterval(rssPollInterval),
			func() (river.JobArgs, *river.InsertOpts) { return RSSPollArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: false},
		))
	}

	riverClient, err := river.NewClient(driver, &river.Config{
		Logger:      logger,
		MaxAttempts: defaultMaxAttempts,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: maxWorkers},
		},
		Workers:      workers,
		PeriodicJobs: periodicJobs,
	})
	if err != nil {
		return nil, fmt.Errorf("build river client: %w", err)
	}

	return &Client{river: riverClient, readDB: readDB, logger: logger}, nil
}

// Start begins working jobs. Jobs executed by the client inherit from ctx.
func (c *Client) Start(ctx context.Context) error {
	if err := c.river.Start(ctx); err != nil {
		return fmt.Errorf("start river client: %w", err)
	}
	return nil
}

// Stop gracefully drains in-flight jobs, honoring ctx's deadline (the bounded
// shutdown drain — D-12 / JOBS-03). Jobs not finished within the deadline are left
// for recovery on the next start (idempotent re-run is safe).
func (c *Client) Stop(ctx context.Context) error {
	if err := c.river.Stop(ctx); err != nil {
		return fmt.Errorf("stop river client: %w", err)
	}
	return nil
}

// EnqueueImport durably enqueues an import job for a series. Duplicate enqueues for
// the same series collapse via the job's uniqueness opts (see ImportArgs.InsertOpts).
func (c *Client) EnqueueImport(ctx context.Context, seriesID, comicvineVolumeID int64) error {
	_, err := c.river.Insert(ctx, ImportArgs{
		SeriesID:          seriesID,
		ComicvineVolumeID: comicvineVolumeID,
	}, nil)
	if err != nil {
		return fmt.Errorf("enqueue import for series %d: %w", seriesID, err)
	}
	return nil
}

// EnqueueRefresh durably enqueues a conditional refresh job for a series. Duplicate
// enqueues for the same series collapse via the job's uniqueness opts (see
// RefreshArgs.InsertOpts) so a double "Refresh now" click does not double-queue.
func (c *Client) EnqueueRefresh(ctx context.Context, seriesID, comicvineVolumeID int64) error {
	_, err := c.river.Insert(ctx, RefreshArgs{
		SeriesID:          seriesID,
		ComicvineVolumeID: comicvineVolumeID,
	}, nil)
	if err != nil {
		return fmt.Errorf("enqueue refresh for series %d: %w", seriesID, err)
	}
	return nil
}

// EnqueueSearchIssue durably enqueues a one-off auto-search job for an issue. Duplicate
// enqueues for the same issue collapse via the job's uniqueness opts (see
// SearchIssueArgs.InsertOpts), so the immediate-enqueue-on-Wanted burst (D-10) does not
// stack redundant searches. The bounded River worker pool absorbs the fan-out.
func (c *Client) EnqueueSearchIssue(ctx context.Context, issueID int64) error {
	_, err := c.river.Insert(ctx, SearchIssueArgs{IssueID: issueID}, nil)
	if err != nil {
		return fmt.Errorf("enqueue search for issue %d: %w", issueID, err)
	}
	return nil
}
