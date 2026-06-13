package transport

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/search"
)

// SearchServicer is the subset of search.Service the handler depends on. The transport
// layer depends only on the service package, never on repository/provider/db.
type SearchServicer interface {
	SearchIssue(ctx context.Context, issueID int64) (search.Result, error)
	SelectCandidate(ctx context.Context, issueID int64, provider, releaseKey string) (search.DownloadResult, error)
	GetTimeline(ctx context.Context, issueID int64) ([]search.TimelineEvent, error)
	SearchAndGrab(ctx context.Context, issueID int64) (search.AutoSearchOutcome, error)
	BlacklistRelease(ctx context.Context, issueID int64, provider, releaseKey string) error
	RetryDownload(ctx context.Context, issueID int64) (search.AutoSearchOutcome, error)
}

// SearchHandler implements the generated SearchServiceHandler over the search service.
type SearchHandler struct {
	omnibusv1connect.UnimplementedSearchServiceHandler
	svc SearchServicer
}

// NewSearchHandler builds the SearchService Connect handler.
func NewSearchHandler(svc SearchServicer) *SearchHandler {
	return &SearchHandler{svc: svc}
}

// SearchIssue handles the manual search RPC.
func (h *SearchHandler) SearchIssue(
	ctx context.Context, req *connect.Request[omnibusv1.SearchIssueRequest],
) (*connect.Response[omnibusv1.SearchIssueResponse], error) {
	issueID := req.Msg.GetIssueId()
	if issueID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("issue_id must be positive"))
	}
	res, err := h.svc.SearchIssue(ctx, issueID)
	if err != nil {
		return nil, serviceError(err)
	}
	cands := make([]*omnibusv1.Candidate, 0, len(res.Candidates))
	for _, c := range res.Candidates {
		cands = append(cands, &omnibusv1.Candidate{
			Provider:   c.Provider,
			ReleaseKey: c.ReleaseKey,
			Title:      c.Title,
			SizeBytes:  c.SizeBytes,
			Score:      c.Score,
			Reason:     c.Reason,
		})
	}
	return connect.NewResponse(&omnibusv1.SearchIssueResponse{
		Candidates:  cands,
		Acceptable:  res.Acceptable,
		FloorReason: res.FloorReason,
	}), nil
}

// SelectCandidate handles the grab RPC.
func (h *SearchHandler) SelectCandidate(
	ctx context.Context, req *connect.Request[omnibusv1.SelectCandidateRequest],
) (*connect.Response[omnibusv1.SelectCandidateResponse], error) {
	m := req.Msg
	if m.GetIssueId() <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("issue_id must be positive"))
	}
	if m.GetProvider() == "" || m.GetReleaseKey() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("provider and release_key must not be empty"))
	}
	dl, err := h.svc.SelectCandidate(ctx, m.GetIssueId(), m.GetProvider(), m.GetReleaseKey())
	if err != nil {
		if errors.Is(err, search.ErrCrossIssueGrab) {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		return nil, serviceError(err)
	}
	return connect.NewResponse(&omnibusv1.SelectCandidateResponse{
		Download: &omnibusv1.Download{
			Id:         dl.DownloadID,
			IssueId:    m.GetIssueId(),
			Provider:   dl.Provider,
			ReleaseKey: dl.ReleaseKey,
			Status:     dl.Status,
			ClientRef:  dl.ClientRef,
		},
	}), nil
}

// GetIssueTimeline handles the timeline read RPC.
func (h *SearchHandler) GetIssueTimeline(
	ctx context.Context, req *connect.Request[omnibusv1.GetIssueTimelineRequest],
) (*connect.Response[omnibusv1.GetIssueTimelineResponse], error) {
	issueID := req.Msg.GetIssueId()
	if issueID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("issue_id must be positive"))
	}
	events, err := h.svc.GetTimeline(ctx, issueID)
	if err != nil {
		return nil, serviceError(err)
	}
	out := make([]*omnibusv1.IssueEvent, 0, len(events))
	for _, e := range events {
		out = append(out, &omnibusv1.IssueEvent{
			Id:         e.ID,
			IssueId:    e.IssueID,
			Type:       issueEventTypeToProto(e.Type),
			OccurredAt: e.OccurredAt,
			Detail:     e.Detail,
		})
	}
	return connect.NewResponse(&omnibusv1.GetIssueTimelineResponse{Events: out}), nil
}

// TriggerAutoSearch runs an on-demand search-and-auto-grab for an issue immediately,
// bypassing the River auto-search queue. It executes the same pipeline the queue worker
// runs (SearchAndGrab) synchronously in the request, so the caller blocks until the
// search (and any auto-grab) completes. The response reports whether a release was grabbed
// (with its title) or why nothing cleared the floor.
func (h *SearchHandler) TriggerAutoSearch(
	ctx context.Context, req *connect.Request[omnibusv1.TriggerAutoSearchRequest],
) (*connect.Response[omnibusv1.TriggerAutoSearchResponse], error) {
	issueID := req.Msg.GetIssueId()
	if issueID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("issue_id must be positive"))
	}
	outcome, err := h.svc.SearchAndGrab(ctx, issueID)
	if err != nil {
		return nil, serviceError(err)
	}
	return connect.NewResponse(&omnibusv1.TriggerAutoSearchResponse{
		Grabbed:     outcome.Grabbed,
		Title:       outcome.Title,
		FloorReason: outcome.FloorReason,
	}), nil
}

// BlacklistRelease handles the per-issue blacklist RPC: it validates the inputs (T-05-13),
// blacklists the release, and triggers a replacement search (DL-05).
func (h *SearchHandler) BlacklistRelease(
	ctx context.Context, req *connect.Request[omnibusv1.BlacklistReleaseRequest],
) (*connect.Response[omnibusv1.BlacklistReleaseResponse], error) {
	m := req.Msg
	if m.GetIssueId() <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("issue_id must be positive"))
	}
	if m.GetProvider() == "" || m.GetReleaseKey() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("provider and release_key must not be empty"))
	}
	if err := h.svc.BlacklistRelease(ctx, m.GetIssueId(), m.GetProvider(), m.GetReleaseKey()); err != nil {
		return nil, serviceError(err)
	}
	return connect.NewResponse(&omnibusv1.BlacklistReleaseResponse{}), nil
}

// RetryDownload handles the manual-retry RPC (DL-07): it resets the cool-off and re-runs
// the search pipeline immediately, returning whether a replacement was grabbed.
func (h *SearchHandler) RetryDownload(
	ctx context.Context, req *connect.Request[omnibusv1.RetryDownloadRequest],
) (*connect.Response[omnibusv1.RetryDownloadResponse], error) {
	issueID := req.Msg.GetIssueId()
	if issueID <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("issue_id must be positive"))
	}
	outcome, err := h.svc.RetryDownload(ctx, issueID)
	if err != nil {
		return nil, serviceError(err)
	}
	return connect.NewResponse(&omnibusv1.RetryDownloadResponse{
		Grabbed:     outcome.Grabbed,
		Title:       outcome.Title,
		FloorReason: outcome.FloorReason,
	}), nil
}

// issueEventTypeToProto maps a stored event_type string to the proto enum.
func issueEventTypeToProto(t string) omnibusv1.IssueEventType {
	switch t {
	case "searched":
		return omnibusv1.IssueEventType_ISSUE_EVENT_TYPE_SEARCHED
	case "candidate-selected":
		return omnibusv1.IssueEventType_ISSUE_EVENT_TYPE_CANDIDATE_SELECTED
	case "snatched":
		return omnibusv1.IssueEventType_ISSUE_EVENT_TYPE_SNATCHED
	case "failed":
		return omnibusv1.IssueEventType_ISSUE_EVENT_TYPE_FAILED
	case "downloaded":
		return omnibusv1.IssueEventType_ISSUE_EVENT_TYPE_DOWNLOADED
	case "processed":
		return omnibusv1.IssueEventType_ISSUE_EVENT_TYPE_PROCESSED
	default:
		return omnibusv1.IssueEventType_ISSUE_EVENT_TYPE_UNSPECIFIED
	}
}
