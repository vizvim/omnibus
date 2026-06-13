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
	"github.com/vizvim/omnibus/internal/search"
	"github.com/vizvim/omnibus/internal/transport"
)

// fakeSearchService is a hand stub of SearchServicer for handler tests.
type fakeSearchService struct {
	result   search.Result
	download search.DownloadResult
	events   []search.TimelineEvent
	selErr   error
	outcome  search.AutoSearchOutcome
	runErr   error
	ranIssue int64

	blacklistErr error
	blIssue      int64
	blProvider   string
	blReleaseKey string

	retryErr     error
	retriedIssue int64
	retryOutcome search.AutoSearchOutcome
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

func (f *fakeSearchService) SearchAndGrab(_ context.Context, issueID int64) (search.AutoSearchOutcome, error) {
	f.ranIssue = issueID
	if f.runErr != nil {
		return search.AutoSearchOutcome{}, f.runErr
	}
	return f.outcome, nil
}

func (f *fakeSearchService) BlacklistRelease(_ context.Context, issueID int64, provider, releaseKey string) error {
	f.blIssue = issueID
	f.blProvider = provider
	f.blReleaseKey = releaseKey
	return f.blacklistErr
}

func (f *fakeSearchService) RetryDownload(_ context.Context, issueID int64) (search.AutoSearchOutcome, error) {
	f.retriedIssue = issueID
	if f.retryErr != nil {
		return search.AutoSearchOutcome{}, f.retryErr
	}
	return f.retryOutcome, nil
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

func TestTriggerAutoSearchRejectsNonPositiveID(t *testing.T) {
	t.Parallel()
	client := newSearchClient(t, &fakeSearchService{})

	_, err := client.TriggerAutoSearch(context.Background(), connect.NewRequest(&omnibusv1.TriggerAutoSearchRequest{IssueId: 0}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestTriggerAutoSearchGrabsInline(t *testing.T) {
	t.Parallel()
	svc := &fakeSearchService{outcome: search.AutoSearchOutcome{Grabbed: true, Title: "Absolute Batman 001 (cbz)"}}
	client := newSearchClient(t, svc)

	resp, err := client.TriggerAutoSearch(context.Background(), connect.NewRequest(&omnibusv1.TriggerAutoSearchRequest{IssueId: 42}))
	require.NoError(t, err)
	require.True(t, resp.Msg.GetGrabbed())
	require.Equal(t, "Absolute Batman 001 (cbz)", resp.Msg.GetTitle())
	require.Equal(t, int64(42), svc.ranIssue)
}

func TestTriggerAutoSearchReportsNothingAcceptable(t *testing.T) {
	t.Parallel()
	svc := &fakeSearchService{outcome: search.AutoSearchOutcome{Grabbed: false, FloorReason: "best candidate scored 54 < floor 60"}}
	client := newSearchClient(t, svc)

	resp, err := client.TriggerAutoSearch(context.Background(), connect.NewRequest(&omnibusv1.TriggerAutoSearchRequest{IssueId: 7}))
	require.NoError(t, err)
	require.False(t, resp.Msg.GetGrabbed())
	require.Equal(t, "best candidate scored 54 < floor 60", resp.Msg.GetFloorReason())
}

func TestBlacklistReleaseRejectsNonPositiveID(t *testing.T) {
	t.Parallel()
	client := newSearchClient(t, &fakeSearchService{})

	_, err := client.BlacklistRelease(context.Background(), connect.NewRequest(
		&omnibusv1.BlacklistReleaseRequest{IssueId: 0, Provider: "newznab", ReleaseKey: "rk"}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestBlacklistReleaseRejectsEmptyProviderOrKey(t *testing.T) {
	t.Parallel()
	client := newSearchClient(t, &fakeSearchService{})

	_, err := client.BlacklistRelease(context.Background(), connect.NewRequest(
		&omnibusv1.BlacklistReleaseRequest{IssueId: 1, Provider: "", ReleaseKey: "rk"}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestBlacklistReleasePassesThroughToService(t *testing.T) {
	t.Parallel()
	svc := &fakeSearchService{}
	client := newSearchClient(t, svc)

	_, err := client.BlacklistRelease(context.Background(), connect.NewRequest(
		&omnibusv1.BlacklistReleaseRequest{IssueId: 42, Provider: "newznab", ReleaseKey: "rk-bad"}))
	require.NoError(t, err)
	require.Equal(t, int64(42), svc.blIssue)
	require.Equal(t, "newznab", svc.blProvider)
	require.Equal(t, "rk-bad", svc.blReleaseKey)
}

func TestRetryDownloadRejectsNonPositiveID(t *testing.T) {
	t.Parallel()
	client := newSearchClient(t, &fakeSearchService{})

	_, err := client.RetryDownload(context.Background(), connect.NewRequest(&omnibusv1.RetryDownloadRequest{IssueId: 0}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestRetryDownloadReturnsOutcome(t *testing.T) {
	t.Parallel()
	svc := &fakeSearchService{retryOutcome: search.AutoSearchOutcome{Grabbed: true, Title: "Saga #7 (cbz)"}}
	client := newSearchClient(t, svc)

	resp, err := client.RetryDownload(context.Background(), connect.NewRequest(&omnibusv1.RetryDownloadRequest{IssueId: 7}))
	require.NoError(t, err)
	require.True(t, resp.Msg.GetGrabbed())
	require.Equal(t, "Saga #7 (cbz)", resp.Msg.GetTitle())
	require.Equal(t, int64(7), svc.retriedIssue)
}
