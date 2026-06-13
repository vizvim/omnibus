package transport

import (
	"context"

	"connectrpc.com/connect"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/jobhistory"
)

const (
	defaultJobRunLimit = 50
	maxJobRunLimit     = 100
)

// JobServicer is the subset of the jobs service the handler depends on (interface so
// the handler stays testable). The transport layer depends only on the service
// package, never on the internal/jobs engine.
type JobServicer interface {
	ListJobRuns(ctx context.Context, limit int32) ([]jobhistory.JobRunView, error)
}

// JobHandler implements the generated JobServiceHandler by delegating to the jobs
// service.
type JobHandler struct {
	omnibusv1connect.UnimplementedJobServiceHandler
	svc JobServicer
}

// NewJobHandler builds the JobService Connect handler over the jobs service.
func NewJobHandler(svc JobServicer) *JobHandler {
	return &JobHandler{svc: svc}
}

// ListJobRuns handles the job-run history RPC. It clamps limit to 1..maxJobRunLimit,
// defaulting a non-positive limit to defaultJobRunLimit.
func (h *JobHandler) ListJobRuns(
	ctx context.Context, req *connect.Request[omnibusv1.ListJobRunsRequest],
) (*connect.Response[omnibusv1.ListJobRunsResponse], error) {
	limit := req.Msg.GetLimit()
	if limit <= 0 {
		limit = defaultJobRunLimit
	}
	if limit > maxJobRunLimit {
		limit = maxJobRunLimit
	}

	runs, err := h.svc.ListJobRuns(ctx, limit)
	if err != nil {
		return nil, serviceError(err)
	}
	out := make([]*omnibusv1.JobRun, 0, len(runs))
	for _, r := range runs {
		out = append(out, &omnibusv1.JobRun{
			Id:         r.ID,
			Kind:       r.Kind,
			State:      jobRunStateToProto(r.State),
			StartedAt:  r.StartedAt,
			FinishedAt: r.FinishedAt,
			Error:      r.Error,
			Attempt:    r.Attempt,
		})
	}
	return connect.NewResponse(&omnibusv1.ListJobRunsResponse{Runs: out}), nil
}

func jobRunStateToProto(s jobhistory.JobRunState) omnibusv1.JobRunState {
	switch s {
	case jobhistory.StateQueued:
		return omnibusv1.JobRunState_JOB_RUN_STATE_QUEUED
	case jobhistory.StateRunning:
		return omnibusv1.JobRunState_JOB_RUN_STATE_RUNNING
	case jobhistory.StateCompleted:
		return omnibusv1.JobRunState_JOB_RUN_STATE_COMPLETED
	case jobhistory.StateFailed:
		return omnibusv1.JobRunState_JOB_RUN_STATE_FAILED
	case jobhistory.StateCancelled:
		return omnibusv1.JobRunState_JOB_RUN_STATE_CANCELLED //nolint:misspell // generated proto enum value

	default:
		return omnibusv1.JobRunState_JOB_RUN_STATE_UNSPECIFIED
	}
}
