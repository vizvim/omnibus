// Package transport holds the Connect RPC handlers and HTTP transport wiring. The
// SeriesService handler maps RPCs to the series.Service and translates domain types to
// proto messages. It imports the service layer only — never repository, provider,
// or db.
package transport

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/service/series"
)

// SeriesServicer is the subset of series.Service the handler depends on (interface so
// the handler stays testable and the dependency is explicit). The transport
// layer depends only on the service package, never on repository/provider/db.
type SeriesServicer interface {
	SearchComicVine(ctx context.Context, query string) ([]series.SearchResult, error)
	AddSeries(ctx context.Context, volumeID int64) (series.Series, error)
	ListSeries(ctx context.Context, page int32) ([]series.Series, error)
	GetSeries(ctx context.Context, seriesID int64) (series.View, error)
	GetIssue(ctx context.Context, issueID int64) (series.IssueDetail, error)
	UpdateSeriesSettings(ctx context.Context, seriesID int64, status string) (series.Series, error)
	RefreshSeries(ctx context.Context, seriesID int64) (series.Series, error)
}

// SeriesHandler implements the generated SeriesServiceHandler by delegating to the
// series service.
type SeriesHandler struct {
	omnibusv1connect.UnimplementedSeriesServiceHandler
	svc SeriesServicer
}

// NewSeriesHandler builds the SeriesService Connect handler over the series service.
func NewSeriesHandler(svc SeriesServicer) *SeriesHandler {
	return &SeriesHandler{svc: svc}
}

// SearchComicVine handles the search RPC.
func (h *SeriesHandler) SearchComicVine(
	ctx context.Context, req *connect.Request[omnibusv1.SearchComicVineRequest],
) (*connect.Response[omnibusv1.SearchComicVineResponse], error) {
	query := req.Msg.GetQuery()
	if query == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("query must not be empty"))
	}
	results, err := h.svc.SearchComicVine(ctx, query)
	if err != nil {
		return nil, serviceError(err)
	}
	candidates := make([]*omnibusv1.Series, 0, len(results))
	for _, r := range results {
		candidates = append(candidates, &omnibusv1.Series{
			ComicvineVolumeId: r.ComicvineVolumeID,
			Name:              r.Name,
			StartYear:         r.StartYear,
			Publisher:         r.Publisher,
			CoverUrl:          r.CoverURL,
			TotalIssues:       r.CountOfIssues,
		})
	}
	return connect.NewResponse(&omnibusv1.SearchComicVineResponse{Candidates: candidates}), nil
}

// AddSeries handles the add RPC.
func (h *SeriesHandler) AddSeries(
	ctx context.Context, req *connect.Request[omnibusv1.AddSeriesRequest],
) (*connect.Response[omnibusv1.AddSeriesResponse], error) {
	volumeID := req.Msg.GetComicvineVolumeId()
	if volumeID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("comicvine_volume_id must be positive"))
	}
	s, err := h.svc.AddSeries(ctx, volumeID)
	if err != nil {
		return nil, serviceError(err)
	}
	return connect.NewResponse(&omnibusv1.AddSeriesResponse{Series: seriesToProto(s)}), nil
}

// ListSeries handles the list RPC.
func (h *SeriesHandler) ListSeries(
	ctx context.Context, req *connect.Request[omnibusv1.ListSeriesRequest],
) (*connect.Response[omnibusv1.ListSeriesResponse], error) {
	rows, err := h.svc.ListSeries(ctx, req.Msg.GetPage())
	if err != nil {
		return nil, serviceError(err)
	}
	out := make([]*omnibusv1.Series, 0, len(rows))
	for _, r := range rows {
		out = append(out, seriesToProto(r))
	}
	return connect.NewResponse(&omnibusv1.ListSeriesResponse{Series: out}), nil
}

// GetSeries handles the detail RPC.
func (h *SeriesHandler) GetSeries(
	ctx context.Context, req *connect.Request[omnibusv1.GetSeriesRequest],
) (*connect.Response[omnibusv1.GetSeriesResponse], error) {
	seriesID := req.Msg.GetSeriesId()
	if seriesID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("series_id must be positive"))
	}
	view, err := h.svc.GetSeries(ctx, seriesID)
	if err != nil {
		return nil, serviceError(err)
	}

	issues := make([]*omnibusv1.Issue, 0, len(view.Issues))
	for _, i := range view.Issues {
		issues = append(issues, issueToProto(i))
	}
	arcs := make([]*omnibusv1.StoryArc, 0, len(view.StoryArcs))
	for _, a := range view.StoryArcs {
		arcs = append(arcs, &omnibusv1.StoryArc{
			Id:             a.ID,
			ComicvineArcId: a.ComicvineArcID,
			Name:           a.Name,
		})
	}

	return connect.NewResponse(&omnibusv1.GetSeriesResponse{
		Series:    seriesToProto(view.Series),
		Issues:    issues,
		Publisher: view.Publisher,
		StoryArcs: arcs,
	}), nil
}

// GetIssue handles the per-issue detail RPC.
func (h *SeriesHandler) GetIssue(
	ctx context.Context, req *connect.Request[omnibusv1.GetIssueRequest],
) (*connect.Response[omnibusv1.GetIssueResponse], error) {
	issueID := req.Msg.GetIssueId()
	if issueID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("issue_id must be positive"))
	}
	detail, err := h.svc.GetIssue(ctx, issueID)
	if err != nil {
		return nil, serviceError(err)
	}
	return connect.NewResponse(&omnibusv1.GetIssueResponse{Issue: issueDetailToProto(detail)}), nil
}

// UpdateSeriesSettings handles the settings RPC.
func (h *SeriesHandler) UpdateSeriesSettings(
	ctx context.Context, req *connect.Request[omnibusv1.UpdateSeriesSettingsRequest],
) (*connect.Response[omnibusv1.UpdateSeriesSettingsResponse], error) {
	seriesID := req.Msg.GetSeriesId()
	if seriesID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("series_id must be positive"))
	}
	s, err := h.svc.UpdateSeriesSettings(ctx, seriesID, req.Msg.GetStatus())
	if err != nil {
		return nil, serviceError(err)
	}
	return connect.NewResponse(&omnibusv1.UpdateSeriesSettingsResponse{Series: seriesToProto(s)}), nil
}

// RefreshSeries handles the on-demand refresh RPC: it enqueues a conditional refresh
// job and returns the current series (the UI polls GetSeries for the result).
func (h *SeriesHandler) RefreshSeries(
	ctx context.Context, req *connect.Request[omnibusv1.RefreshSeriesRequest],
) (*connect.Response[omnibusv1.RefreshSeriesResponse], error) {
	seriesID := req.Msg.GetSeriesId()
	if seriesID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("series_id must be positive"))
	}
	s, err := h.svc.RefreshSeries(ctx, seriesID)
	if err != nil {
		return nil, serviceError(err)
	}
	return connect.NewResponse(&omnibusv1.RefreshSeriesResponse{Series: seriesToProto(s)}), nil
}

// seriesToProto maps a domain series to the proto message, mapping the stored cover to
// the backend /covers route (never a ComicVine hotlink).
func seriesToProto(s series.Series) *omnibusv1.Series {
	return &omnibusv1.Series{
		Id:                s.ID,
		ComicvineVolumeId: s.ComicvineVolumeID,
		Name:              s.Name,
		StartYear:         s.StartYear,
		Publisher:         s.Publisher,
		Status:            s.Status,
		CoverUrl:          coverURL("series", s.ID, s.HasCover),
		TotalIssues:       s.TotalIssues,
		HaveIssues:        s.HaveIssues,
		LastRefreshedAt:   s.LastRefreshedAt,
	}
}

func issueToProto(i series.Issue) *omnibusv1.Issue {
	return &omnibusv1.Issue{
		Id:                   i.ID,
		SeriesId:             i.SeriesID,
		ComicvineIssueId:     i.ComicvineIssueID,
		IssueNumber:          i.IssueNumber,
		IssueNumberSort:      i.IssueNumberSort,
		IssueNumberQualifier: i.IssueNumberQual,
		Title:                i.Title,
		CoverDate:            i.CoverDate,
		Status:               statusToProto(i.Status),
		CoverUrl:             coverURL("issues", i.ID, i.HasCover),
		IssueType:            i.IssueType,
	}
}

// issueDetailToProto maps a domain IssueDetail to its proto message, reusing
// issueToProto for the base issue and creditToProto for each ordered credit.
func issueDetailToProto(d series.IssueDetail) *omnibusv1.IssueDetail {
	credits := make([]*omnibusv1.Credit, 0, len(d.Credits))
	for _, c := range d.Credits {
		credits = append(credits, creditToProto(c))
	}
	return &omnibusv1.IssueDetail{
		Issue:          issueToProto(d.Issue),
		Description:    d.Description,
		IssueType:      d.IssueType,
		AltIssueNumber: d.AltIssueNumber,
		PageCount:      d.PageCount,
		StoreDate:      d.StoreDate,
		CvLastUpdated:  d.CVLastUpdated,
		Credits:        credits,
	}
}

func creditToProto(c series.Credit) *omnibusv1.Credit {
	return &omnibusv1.Credit{
		Role:              c.Role,
		Name:              c.Name,
		ComicvinePersonId: c.ComicvinePersonID,
	}
}

// coverURL returns the backend cover route for an entity, or "" if no cover is stored.
func coverURL(kind string, id int64, hasCover bool) string {
	if !hasCover {
		return ""
	}
	return fmt.Sprintf("/covers/%s/%d.jpg", kind, id)
}

func statusToProto(s string) omnibusv1.IssueStatus {
	switch series.IssueStatus(s) {
	case series.StatusWanted:
		return omnibusv1.IssueStatus_ISSUE_STATUS_WANTED
	case series.StatusSnatched:
		return omnibusv1.IssueStatus_ISSUE_STATUS_SNATCHED
	case series.StatusDownloaded:
		return omnibusv1.IssueStatus_ISSUE_STATUS_DOWNLOADED
	case series.StatusArchived:
		return omnibusv1.IssueStatus_ISSUE_STATUS_ARCHIVED
	case series.StatusSkipped:
		return omnibusv1.IssueStatus_ISSUE_STATUS_SKIPPED
	case series.StatusFailed:
		return omnibusv1.IssueStatus_ISSUE_STATUS_FAILED
	case series.StatusIgnored:
		return omnibusv1.IssueStatus_ISSUE_STATUS_IGNORED
	default:
		return omnibusv1.IssueStatus_ISSUE_STATUS_UNSPECIFIED
	}
}

// serviceError maps a service error to a Connect code (internal by default).
func serviceError(err error) error {
	return connect.NewError(connect.CodeInternal, err)
}
