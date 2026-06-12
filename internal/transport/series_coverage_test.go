package transport_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/service/series"
	"github.com/vizvim/omnibus/internal/transport"
)

// fakeSeriesServicer is a hand-rolled stub of the SeriesServicer the handler depends on,
// letting the handler's mapping + error branches be exercised deterministically without
// a real service, DB, or River client.
type fakeSeriesServicer struct {
	searchResults []series.SearchResult
	searchErr     error

	addSeries series.Series
	addErr    error

	listSeries []series.Series
	listErr    error

	view    series.View
	viewErr error

	issueDetail series.IssueDetail
	issueErr    error

	updateSeries series.Series
	updateErr    error

	refreshSeries series.Series
	refreshErr    error
}

func (f *fakeSeriesServicer) SearchComicVine(context.Context, string) ([]series.SearchResult, error) {
	return f.searchResults, f.searchErr
}

func (f *fakeSeriesServicer) AddSeries(context.Context, int64) (series.Series, error) {
	return f.addSeries, f.addErr
}

func (f *fakeSeriesServicer) ListSeries(context.Context, int32) ([]series.Series, error) {
	return f.listSeries, f.listErr
}

func (f *fakeSeriesServicer) GetSeries(context.Context, int64) (series.View, error) {
	return f.view, f.viewErr
}

func (f *fakeSeriesServicer) GetIssue(context.Context, int64) (series.IssueDetail, error) {
	return f.issueDetail, f.issueErr
}

func (f *fakeSeriesServicer) UpdateSeriesSettings(context.Context, int64, string) (series.Series, error) {
	return f.updateSeries, f.updateErr
}

func (f *fakeSeriesServicer) RefreshSeries(context.Context, int64) (series.Series, error) {
	return f.refreshSeries, f.refreshErr
}

func newFakeSeriesClient(t *testing.T, svc transport.SeriesServicer) omnibusv1connect.SeriesServiceClient {
	t.Helper()
	handler := transport.NewSeriesHandler(svc)
	path, h := omnibusv1connect.NewSeriesServiceHandler(handler)

	mux := http.NewServeMux()
	mux.Handle(path, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return omnibusv1connect.NewSeriesServiceClient(srv.Client(), srv.URL)
}

func TestSeriesHandlerSearchComicVine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("maps candidates", func(t *testing.T) {
		t.Parallel()
		client := newFakeSeriesClient(t, &fakeSeriesServicer{searchResults: []series.SearchResult{
			{ComicvineVolumeID: 10, Name: "Hellboy", StartYear: 1994, Publisher: "Dark Horse", CountOfIssues: 3, CoverURL: "http://cv/c.jpg"},
		}})
		resp, err := client.SearchComicVine(ctx, connect.NewRequest(&omnibusv1.SearchComicVineRequest{Query: "hellboy"}))
		require.NoError(t, err)
		require.Len(t, resp.Msg.GetCandidates(), 1)
		require.Equal(t, "Hellboy", resp.Msg.GetCandidates()[0].GetName())
		require.Equal(t, int32(3), resp.Msg.GetCandidates()[0].GetTotalIssues())
	})

	t.Run("service error surfaces as internal", func(t *testing.T) {
		t.Parallel()
		client := newFakeSeriesClient(t, &fakeSeriesServicer{searchErr: errors.New("cv down")})
		_, err := client.SearchComicVine(ctx, connect.NewRequest(&omnibusv1.SearchComicVineRequest{Query: "x"}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	})
}

func TestSeriesHandlerAddSeriesServiceError(t *testing.T) {
	t.Parallel()
	client := newFakeSeriesClient(t, &fakeSeriesServicer{addErr: errors.New("boom")})
	_, err := client.AddSeries(context.Background(), connect.NewRequest(&omnibusv1.AddSeriesRequest{ComicvineVolumeId: 5}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}

func TestSeriesHandlerListSeries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("maps rows", func(t *testing.T) {
		t.Parallel()
		client := newFakeSeriesClient(t, &fakeSeriesServicer{listSeries: []series.Series{
			{ID: 1, Name: "A", Status: "Active"},
			{ID: 2, Name: "B", Status: "Paused"},
		}})
		resp, err := client.ListSeries(ctx, connect.NewRequest(&omnibusv1.ListSeriesRequest{Page: 0}))
		require.NoError(t, err)
		require.Len(t, resp.Msg.GetSeries(), 2)
		require.Equal(t, "A", resp.Msg.GetSeries()[0].GetName())
	})

	t.Run("service error", func(t *testing.T) {
		t.Parallel()
		client := newFakeSeriesClient(t, &fakeSeriesServicer{listErr: errors.New("db down")})
		_, err := client.ListSeries(ctx, connect.NewRequest(&omnibusv1.ListSeriesRequest{}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	})
}

func TestSeriesHandlerGetSeries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("maps view with issues and arcs", func(t *testing.T) {
		t.Parallel()
		client := newFakeSeriesClient(t, &fakeSeriesServicer{view: series.View{
			Series:    series.Series{ID: 1, Name: "S", Status: "Active", HasCover: true},
			Issues:    []series.Issue{{ID: 7, SeriesID: 1, IssueNumber: "1", Status: "Wanted", HasCover: true}},
			Publisher: "Marvel",
			StoryArcs: []series.StoryArc{{ID: 3, ComicvineArcID: 99, Name: "Arc"}},
		}})
		resp, err := client.GetSeries(ctx, connect.NewRequest(&omnibusv1.GetSeriesRequest{SeriesId: 1}))
		require.NoError(t, err)
		require.Equal(t, "Marvel", resp.Msg.GetPublisher())
		require.Len(t, resp.Msg.GetIssues(), 1)
		require.Len(t, resp.Msg.GetStoryArcs(), 1)
		require.Equal(t, "Arc", resp.Msg.GetStoryArcs()[0].GetName())
		// HasCover=true maps to a backend /covers route.
		require.Contains(t, resp.Msg.GetSeries().GetCoverUrl(), "/covers/series/1")
		require.Contains(t, resp.Msg.GetIssues()[0].GetCoverUrl(), "/covers/issues/7")
	})

	t.Run("service error", func(t *testing.T) {
		t.Parallel()
		client := newFakeSeriesClient(t, &fakeSeriesServicer{viewErr: errors.New("nope")})
		_, err := client.GetSeries(ctx, connect.NewRequest(&omnibusv1.GetSeriesRequest{SeriesId: 1}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	})

	t.Run("invalid id", func(t *testing.T) {
		t.Parallel()
		client := newFakeSeriesClient(t, &fakeSeriesServicer{})
		_, err := client.GetSeries(ctx, connect.NewRequest(&omnibusv1.GetSeriesRequest{SeriesId: 0}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})
}

func TestSeriesHandlerGetIssue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("maps detail with credits and status", func(t *testing.T) {
		t.Parallel()
		client := newFakeSeriesClient(t, &fakeSeriesServicer{issueDetail: series.IssueDetail{
			Issue:          series.Issue{ID: 5, SeriesID: 1, IssueNumber: "1", Status: string(series.StatusDownloaded), IssueType: "standard", HasCover: true},
			Description:    "summary",
			IssueType:      "standard",
			AltIssueNumber: "1A",
			PageCount:      32,
			StoreDate:      "2020-01-01",
			CVLastUpdated:  "2020-02-02",
			Credits: []series.Credit{
				{Role: "writer", Name: "Zoe", ComicvinePersonID: 3},
				{Role: "penciller", Name: "Al", ComicvinePersonID: 1},
			},
		}})
		resp, err := client.GetIssue(ctx, connect.NewRequest(&omnibusv1.GetIssueRequest{IssueId: 5}))
		require.NoError(t, err)
		d := resp.Msg.GetIssue()
		require.Equal(t, "summary", d.GetDescription())
		require.Equal(t, "1A", d.GetAltIssueNumber())
		require.Equal(t, int32(32), d.GetPageCount())
		require.Len(t, d.GetCredits(), 2)
		require.Equal(t, "writer", d.GetCredits()[0].GetRole())
		require.Equal(t, "Zoe", d.GetCredits()[0].GetName())
		// status maps through statusToProto.
		require.Equal(t, omnibusv1.IssueStatus_ISSUE_STATUS_DOWNLOADED, d.GetIssue().GetStatus())
	})

	t.Run("service error", func(t *testing.T) {
		t.Parallel()
		client := newFakeSeriesClient(t, &fakeSeriesServicer{issueErr: errors.New("missing")})
		_, err := client.GetIssue(ctx, connect.NewRequest(&omnibusv1.GetIssueRequest{IssueId: 9}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	})

	t.Run("invalid id", func(t *testing.T) {
		t.Parallel()
		client := newFakeSeriesClient(t, &fakeSeriesServicer{})
		_, err := client.GetIssue(ctx, connect.NewRequest(&omnibusv1.GetIssueRequest{IssueId: -1}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})
}

func TestSeriesHandlerUpdateSeriesSettings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("maps updated series", func(t *testing.T) {
		t.Parallel()
		client := newFakeSeriesClient(t, &fakeSeriesServicer{updateSeries: series.Series{ID: 1, Name: "S", Status: "Paused"}})
		resp, err := client.UpdateSeriesSettings(ctx, connect.NewRequest(&omnibusv1.UpdateSeriesSettingsRequest{SeriesId: 1, Status: "Paused"}))
		require.NoError(t, err)
		require.Equal(t, "Paused", resp.Msg.GetSeries().GetStatus())
	})

	t.Run("invalid id", func(t *testing.T) {
		t.Parallel()
		client := newFakeSeriesClient(t, &fakeSeriesServicer{})
		_, err := client.UpdateSeriesSettings(ctx, connect.NewRequest(&omnibusv1.UpdateSeriesSettingsRequest{SeriesId: 0, Status: "Active"}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("service error (invalid status)", func(t *testing.T) {
		t.Parallel()
		client := newFakeSeriesClient(t, &fakeSeriesServicer{updateErr: errors.New("invalid series status")})
		_, err := client.UpdateSeriesSettings(ctx, connect.NewRequest(&omnibusv1.UpdateSeriesSettingsRequest{SeriesId: 1, Status: "Bogus"}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	})
}

func TestSeriesHandlerRefreshSeries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("maps current series", func(t *testing.T) {
		t.Parallel()
		client := newFakeSeriesClient(t, &fakeSeriesServicer{refreshSeries: series.Series{ID: 1, Name: "S", Status: "Active"}})
		resp, err := client.RefreshSeries(ctx, connect.NewRequest(&omnibusv1.RefreshSeriesRequest{SeriesId: 1}))
		require.NoError(t, err)
		require.Equal(t, int64(1), resp.Msg.GetSeries().GetId())
	})

	t.Run("service error", func(t *testing.T) {
		t.Parallel()
		client := newFakeSeriesClient(t, &fakeSeriesServicer{refreshErr: errors.New("enqueue failed")})
		_, err := client.RefreshSeries(ctx, connect.NewRequest(&omnibusv1.RefreshSeriesRequest{SeriesId: 1}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	})
}

// TestStatusToProtoViaGetIssue drives every IssueStatus value through the handler so the
// statusToProto switch is fully covered (each case + the unspecified default).
func TestStatusToProtoViaGetIssue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		status string
		want   omnibusv1.IssueStatus
	}{
		{string(series.StatusWanted), omnibusv1.IssueStatus_ISSUE_STATUS_WANTED},
		{string(series.StatusSnatched), omnibusv1.IssueStatus_ISSUE_STATUS_SNATCHED},
		{string(series.StatusDownloaded), omnibusv1.IssueStatus_ISSUE_STATUS_DOWNLOADED},
		{string(series.StatusArchived), omnibusv1.IssueStatus_ISSUE_STATUS_ARCHIVED},
		{string(series.StatusSkipped), omnibusv1.IssueStatus_ISSUE_STATUS_SKIPPED},
		{string(series.StatusFailed), omnibusv1.IssueStatus_ISSUE_STATUS_FAILED},
		{string(series.StatusIgnored), omnibusv1.IssueStatus_ISSUE_STATUS_IGNORED},
		{"NonsenseStatus", omnibusv1.IssueStatus_ISSUE_STATUS_UNSPECIFIED},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			t.Parallel()
			client := newFakeSeriesClient(t, &fakeSeriesServicer{issueDetail: series.IssueDetail{
				Issue: series.Issue{ID: 1, Status: tc.status},
			}})
			resp, err := client.GetIssue(ctx, connect.NewRequest(&omnibusv1.GetIssueRequest{IssueId: 1}))
			require.NoError(t, err)
			require.Equal(t, tc.want, resp.Msg.GetIssue().GetIssue().GetStatus())
		})
	}
}
