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
	"github.com/vizvim/omnibus/internal/service/search"
	"github.com/vizvim/omnibus/internal/transport"
)

// fakeSearchService is a hand stub of SearchServicer for handler tests.
type fakeSearchService struct {
	result   search.Result
	download search.DownloadResult
	events   []search.TimelineEvent
	selErr   error
}

func (f *fakeSearchService) SearchIssue(_ context.Context, _ int64) (search.Result, error) {
	return f.result, nil
}

func (f *fakeSearchService) SelectCandidate(_ context.Context, _ int64, _, _ string) (search.DownloadResult, error) {
	if f.selErr != nil {
		return search.DownloadResult{}, f.selErr
	}
	return f.download, nil
}

func (f *fakeSearchService) GetTimeline(_ context.Context, _ int64) ([]search.TimelineEvent, error) {
	return f.events, nil
}

func newSearchClient(t *testing.T, svc transport.SearchServicer) omnibusv1connect.SearchServiceClient {
	t.Helper()
	handler := transport.NewSearchHandler(svc)
	path, h := omnibusv1connect.NewSearchServiceHandler(handler)
	mux := http.NewServeMux()
	mux.Handle(path, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return omnibusv1connect.NewSearchServiceClient(srv.Client(), srv.URL)
}

func TestSearchIssueRejectsNonPositiveID(t *testing.T) {
	t.Parallel()
	client := newSearchClient(t, &fakeSearchService{})

	_, err := client.SearchIssue(context.Background(), connect.NewRequest(&omnibusv1.SearchIssueRequest{IssueId: 0}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestSearchIssueReturnsCandidatesWithScoreAndReason(t *testing.T) {
	t.Parallel()
	client := newSearchClient(t, &fakeSearchService{
		result: search.Result{
			Candidates: []search.CandidateView{
				{Provider: "newznab", ReleaseKey: "ok", Title: "Saga #7", Score: 130, SizeBytes: 100},
				{Provider: "newznab", ReleaseKey: "bad", Title: "Saga #8", Reason: "wrong-issue"},
			},
			Acceptable:  true,
			FloorReason: "",
		},
	})

	resp, err := client.SearchIssue(context.Background(), connect.NewRequest(&omnibusv1.SearchIssueRequest{IssueId: 1}))
	require.NoError(t, err)
	require.True(t, resp.Msg.GetAcceptable())
	require.Len(t, resp.Msg.GetCandidates(), 2)
	require.InDelta(t, 130.0, resp.Msg.GetCandidates()[0].GetScore(), 0.001)
	require.Equal(t, "wrong-issue", resp.Msg.GetCandidates()[1].GetReason())
}

func TestSelectCandidateCrossIssueIsInvalidArgument(t *testing.T) {
	t.Parallel()
	client := newSearchClient(t, &fakeSearchService{selErr: search.ErrCrossIssueGrab})

	_, err := client.SelectCandidate(context.Background(), connect.NewRequest(&omnibusv1.SelectCandidateRequest{
		IssueId: 1, Provider: "newznab", ReleaseKey: "x",
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestSelectCandidateReturnsDownload(t *testing.T) {
	t.Parallel()
	client := newSearchClient(t, &fakeSearchService{
		download: search.DownloadResult{DownloadID: 5, ClientRef: "nzo_1", Provider: "sabnzbd", ReleaseKey: "rk", Status: "Queued"},
	})

	resp, err := client.SelectCandidate(context.Background(), connect.NewRequest(&omnibusv1.SelectCandidateRequest{
		IssueId: 1, Provider: "newznab", ReleaseKey: "rk",
	}))
	require.NoError(t, err)
	require.Equal(t, int64(5), resp.Msg.GetDownload().GetId())
	require.Equal(t, "nzo_1", resp.Msg.GetDownload().GetClientRef())
}

func TestGetIssueTimelineMapsEvents(t *testing.T) {
	t.Parallel()
	client := newSearchClient(t, &fakeSearchService{
		events: []search.TimelineEvent{
			{ID: 1, IssueID: 1, Type: "searched", OccurredAt: "2026-01-01T00:00:00Z"},
			{ID: 2, IssueID: 1, Type: "snatched", OccurredAt: "2026-01-01T00:05:00Z", Detail: `{"x":1}`},
		},
	})

	resp, err := client.GetIssueTimeline(context.Background(), connect.NewRequest(&omnibusv1.GetIssueTimelineRequest{IssueId: 1}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetEvents(), 2)
	require.Equal(t, omnibusv1.IssueEventType_ISSUE_EVENT_TYPE_SEARCHED, resp.Msg.GetEvents()[0].GetType())
	require.Equal(t, omnibusv1.IssueEventType_ISSUE_EVENT_TYPE_SNATCHED, resp.Msg.GetEvents()[1].GetType())
}

func TestTriggerAutoSearchUnimplemented(t *testing.T) {
	t.Parallel()
	client := newSearchClient(t, &fakeSearchService{})

	_, err := client.TriggerAutoSearch(context.Background(), connect.NewRequest(&omnibusv1.TriggerAutoSearchRequest{IssueId: 1}))
	require.Error(t, err)
	require.Equal(t, connect.CodeUnimplemented, connect.CodeOf(err))
}
