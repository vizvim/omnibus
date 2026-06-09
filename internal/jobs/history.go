package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// JobRunState is the five-state UI vocabulary for a job run, collapsed from River's
// internal state set.
type JobRunState string

const (
	JobRunQueued    JobRunState = "queued"
	JobRunRunning   JobRunState = "running"
	JobRunCompleted JobRunState = "completed"
	JobRunFailed    JobRunState = "failed"
	JobRunCancelled JobRunState = "cancelled"
)

// JobRun is one background job run as read from River's own tables.
type JobRun struct {
	ID         string
	Kind       string
	State      JobRunState
	StartedAt  string
	FinishedAt string
	Error      string
	Attempt    int
}

// historyLimitCap bounds how many runs ListJobRuns will return regardless of the
// requested limit.
const historyLimitCap = 100

// ListJobRuns reads the most recent job runs directly from River's river_job table via
// the READ pool (history is a read — ADR 0002), newest first, bounded by limit (capped
// at historyLimitCap). This is the ONLY place that knows River's table/column shape
// (D-02 tradeoff): a future swap to a hand-owned projection table is localized here.
func (c *Client) ListJobRuns(ctx context.Context, limit int) ([]JobRun, error) {
	if limit < 1 || limit > historyLimitCap {
		limit = historyLimitCap
	}

	const q = `
		SELECT id, kind, state, attempt, attempted_at, finalized_at, errors
		FROM river_job
		ORDER BY COALESCE(finalized_at, attempted_at, created_at) DESC, id DESC
		LIMIT ?`
	rows, err := c.readDB.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("query river_job history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []JobRun
	for rows.Next() {
		var (
			id          int64
			kind        string
			state       string
			attempt     int
			attemptedAt sql.NullString
			finalizedAt sql.NullString
			errorsBlob  []byte
		)
		if err := rows.Scan(&id, &kind, &state, &attempt, &attemptedAt, &finalizedAt, &errorsBlob); err != nil {
			return nil, fmt.Errorf("scan river_job row: %w", err)
		}
		out = append(out, JobRun{
			ID:         fmt.Sprintf("%d", id),
			Kind:       kind,
			State:      mapRiverState(state),
			StartedAt:  attemptedAt.String,
			FinishedAt: finalizedAt.String,
			Error:      latestError(errorsBlob),
			Attempt:    attempt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate river_job rows: %w", err)
	}
	return out, nil
}

// mapRiverState translates River's internal state onto the five UI states. River
// states: available/scheduled/pending (waiting) → queued; running → running;
// completed → completed; discarded/retryable (errored, will retry or gave up) →
// failed; cancelled → cancelled.
func mapRiverState(state string) JobRunState {
	switch state {
	case "running":
		return JobRunRunning
	case "completed":
		return JobRunCompleted
	case "discarded", "retryable":
		return JobRunFailed
	case "cancelled":
		return JobRunCancelled
	default: // available, scheduled, pending
		return JobRunQueued
	}
}

// riverError mirrors the shape River stores in the river_job.errors JSON array.
type riverError struct {
	Error string `json:"error"`
}

// latestError extracts the most recent error message from River's errors JSON array
// (empty string if absent/unparseable).
func latestError(blob []byte) string {
	if len(blob) == 0 {
		return ""
	}
	var errs []riverError
	if err := json.Unmarshal(blob, &errs); err != nil || len(errs) == 0 {
		return ""
	}
	return errs[len(errs)-1].Error
}
