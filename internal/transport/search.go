package transport

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/service/search"
)

// SearchServicer is the subset of search.Service the handler depends on. The transport
// layer depends only on the service package, never on repository/provider/db.
type SearchServicer interface {
	SearchIssue(ctx context.Context, issueID int64) (search.Result, error)
	SelectCandidate(ctx context.Context, issueID int64, provider, releaseKey string) (search.DownloadResult, error)
	GetTimeline(ctx context.Context, issueID int64) ([]search.TimelineEvent, error)
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

// TriggerAutoSearch enqueues an auto-search job. The job workers land in Plan 06; until
// then this returns Unimplemented rather than silently succeeding.
func (h *SearchHandler) TriggerAutoSearch(
	_ context.Context, req *connect.Request[omnibusv1.TriggerAutoSearchRequest],
) (*connect.Response[omnibusv1.TriggerAutoSearchResponse], error) {
	if req.Msg.GetIssueId() <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("issue_id must be positive"))
	}
	return nil, connect.NewError(connect.CodeUnimplemented,
		errors.New("auto-search is not yet enabled (lands in plan 04-06)"))
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
