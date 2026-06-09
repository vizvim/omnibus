package transport_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	jobsvc "github.com/vizvim/omnibus/internal/service/jobs"
	"github.com/vizvim/omnibus/internal/transport"
)

// recordingJobSvc records the limit it was called with and returns canned runs.
type recordingJobSvc struct {
	lastLimit int32
	runs      []jobsvc.JobRunView
}

func (s *recordingJobSvc) ListJobRuns(_ context.Context, limit int32) ([]jobsvc.JobRunView, error) {
	s.lastLimit = limit
	return s.runs, nil
}

func newJobClient(t *testing.T, svc transport.JobServicer) omnibusv1connect.JobServiceClient {
	t.Helper()
	handler := transport.NewJobHandler(svc)
	mux := http.NewServeMux()
	p, h := omnibusv1connect.NewJobServiceHandler(handler)
	mux.Handle(p, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return omnibusv1connect.NewJobServiceClient(srv.Client(), srv.URL)
}

func TestListJobRunsDefaultsZeroLimit(t *testing.T) {
	t.Parallel()
	svc := &recordingJobSvc{}
	client := newJobClient(t, svc)

	_, err := client.ListJobRuns(context.Background(), connect.NewRequest(&omnibusv1.ListJobRunsRequest{Limit: 0}))
	require.NoError(t, err)
	require.Equal(t, int32(50), svc.lastLimit, "zero limit defaults to 50")
}

func TestListJobRunsClampsHighLimit(t *testing.T) {
	t.Parallel()
	svc := &recordingJobSvc{}
	client := newJobClient(t, svc)

	_, err := client.ListJobRuns(context.Background(), connect.NewRequest(&omnibusv1.ListJobRunsRequest{Limit: 9999}))
	require.NoError(t, err)
	require.Equal(t, int32(100), svc.lastLimit, "out-of-range limit clamps to 100")
}

func TestListJobRunsMapsRunsToProto(t *testing.T) {
	t.Parallel()
	svc := &recordingJobSvc{runs: []jobsvc.JobRunView{
		{ID: "1", Kind: "import_series", State: jobsvc.StateCompleted, StartedAt: "a", FinishedAt: "b", Attempt: 1},
		{ID: "2", Kind: "refresh_series", State: jobsvc.StateFailed, Error: "boom", Attempt: 3},
	}}
	client := newJobClient(t, svc)

	resp, err := client.ListJobRuns(context.Background(), connect.NewRequest(&omnibusv1.ListJobRunsRequest{Limit: 10}))
	require.NoError(t, err)
	runs := resp.Msg.GetRuns()
	require.Len(t, runs, 2)
	require.Equal(t, omnibusv1.JobRunState_JOB_RUN_STATE_COMPLETED, runs[0].GetState())
	require.Equal(t, omnibusv1.JobRunState_JOB_RUN_STATE_FAILED, runs[1].GetState())
	require.Equal(t, "boom", runs[1].GetError())
}
