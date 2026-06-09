// Package jobs (service layer) exposes background job-run history as transport-agnostic
// domain types, so the transport layer never imports the internal/jobs engine package.
// It depends on a small History interface satisfied by the jobs client.
package jobs

import (
	"context"

	enginejobs "github.com/vizvim/omnibus/internal/jobs"
)

// JobRunState is the transport-agnostic five-state job-run vocabulary.
type JobRunState string

const (
	StateQueued    JobRunState = "queued"
	StateRunning   JobRunState = "running"
	StateCompleted JobRunState = "completed"
	StateFailed    JobRunState = "failed"
	StateCancelled JobRunState = "cancelled"
)

// JobRunView is the domain view of a job run the transport layer maps to proto.
type JobRunView struct {
	ID         string
	Kind       string
	State      JobRunState
	StartedAt  string
	FinishedAt string
	Error      string
	Attempt    int32
}

// History is the read surface the service depends on (satisfied by *enginejobs.Client),
// so the service depends on an interface, not the concrete engine client.
type History interface {
	ListJobRuns(ctx context.Context, limit int) ([]enginejobs.JobRun, error)
}

// Service exposes job-run history to the transport layer.
type Service struct {
	history History
}

// New constructs the JobService over a History reader.
func New(history History) *Service {
	return &Service{history: history}
}

// ListJobRuns returns the most recent job runs as transport-agnostic views.
func (s *Service) ListJobRuns(ctx context.Context, limit int32) ([]JobRunView, error) {
	runs, err := s.history.ListJobRuns(ctx, int(limit))
	if err != nil {
		return nil, err
	}
	out := make([]JobRunView, 0, len(runs))
	for _, r := range runs {
		out = append(out, JobRunView{
			ID:         r.ID,
			Kind:       r.Kind,
			State:      mapState(r.State),
			StartedAt:  r.StartedAt,
			FinishedAt: r.FinishedAt,
			Error:      r.Error,
			Attempt:    int32(r.Attempt),
		})
	}
	return out, nil
}

// mapState translates the engine's JobRunState onto the service vocabulary (a 1:1 map
// kept explicit so the transport layer never sees the engine package's type).
func mapState(s enginejobs.JobRunState) JobRunState {
	switch s {
	case enginejobs.JobRunRunning:
		return StateRunning
	case enginejobs.JobRunCompleted:
		return StateCompleted
	case enginejobs.JobRunFailed:
		return StateFailed
	case enginejobs.JobRunCancelled:
		return StateCancelled
	default:
		return StateQueued
	}
}
