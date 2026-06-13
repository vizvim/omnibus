package transport

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/indexerconfig"
)

// IndexerServicer is the subset of indexerconfig.Service the handler depends on. The
// transport layer depends only on the service package, never on repository/db.
type IndexerServicer interface {
	List(ctx context.Context) ([]indexerconfig.Indexer, error)
	Create(ctx context.Context, in indexerconfig.Input) (indexerconfig.Indexer, error)
	Update(ctx context.Context, id int64, in indexerconfig.Input) (indexerconfig.Indexer, error)
	Delete(ctx context.Context, id int64) error
	Test(ctx context.Context, id int64) (indexerconfig.TestResult, error)
}

// IndexerHandler implements the generated IndexerServiceHandler over the service.
type IndexerHandler struct {
	omnibusv1connect.UnimplementedIndexerServiceHandler
	svc IndexerServicer
}

// NewIndexerHandler builds the IndexerService Connect handler.
func NewIndexerHandler(svc IndexerServicer) *IndexerHandler {
	return &IndexerHandler{svc: svc}
}

// ListIndexers handles the list RPC. The proto Indexer has no api_key field, so the
// response is masked by construction.
func (h *IndexerHandler) ListIndexers(
	ctx context.Context, _ *connect.Request[omnibusv1.ListIndexersRequest],
) (*connect.Response[omnibusv1.ListIndexersResponse], error) {
	rows, err := h.svc.List(ctx)
	if err != nil {
		return nil, serviceError(err)
	}
	out := make([]*omnibusv1.Indexer, 0, len(rows))
	for _, r := range rows {
		out = append(out, indexerToProto(r))
	}
	return connect.NewResponse(&omnibusv1.ListIndexersResponse{Indexers: out}), nil
}

// CreateIndexer handles the create RPC.
func (h *IndexerHandler) CreateIndexer(
	ctx context.Context, req *connect.Request[omnibusv1.CreateIndexerRequest],
) (*connect.Response[omnibusv1.CreateIndexerResponse], error) {
	m := req.Msg
	if m.GetName() == "" || m.GetBaseUrl() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name and base_url must not be empty"))
	}
	got, err := h.svc.Create(ctx, indexerconfig.Input{
		Name:       m.GetName(),
		Kind:       m.GetKind(),
		BaseURL:    m.GetBaseUrl(),
		APIKey:     m.GetApiKey(),
		Enabled:    true,
		Categories: m.GetCategories(),
		Priority:   m.GetPriority(),
		UseForRSS:  m.GetUseForRss(),
	})
	if err != nil {
		return nil, indexerServiceError(err)
	}
	return connect.NewResponse(&omnibusv1.CreateIndexerResponse{Indexer: indexerToProto(got)}), nil
}

// UpdateIndexer handles the update RPC. An empty api_key leaves the stored key intact.
func (h *IndexerHandler) UpdateIndexer(
	ctx context.Context, req *connect.Request[omnibusv1.UpdateIndexerRequest],
) (*connect.Response[omnibusv1.UpdateIndexerResponse], error) {
	m := req.Msg
	if m.GetId() <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("id must be positive"))
	}
	if m.GetName() == "" || m.GetBaseUrl() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name and base_url must not be empty"))
	}
	got, err := h.svc.Update(ctx, m.GetId(), indexerconfig.Input{
		Name:       m.GetName(),
		Kind:       m.GetKind(),
		BaseURL:    m.GetBaseUrl(),
		APIKey:     m.GetApiKey(),
		Enabled:    m.GetEnabled(),
		Categories: m.GetCategories(),
		Priority:   m.GetPriority(),
		UseForRSS:  m.GetUseForRss(),
	})
	if err != nil {
		return nil, indexerServiceError(err)
	}
	return connect.NewResponse(&omnibusv1.UpdateIndexerResponse{Indexer: indexerToProto(got)}), nil
}

// DeleteIndexer handles the delete RPC.
func (h *IndexerHandler) DeleteIndexer(
	ctx context.Context, req *connect.Request[omnibusv1.DeleteIndexerRequest],
) (*connect.Response[omnibusv1.DeleteIndexerResponse], error) {
	if req.Msg.GetId() <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("id must be positive"))
	}
	if err := h.svc.Delete(ctx, req.Msg.GetId()); err != nil {
		return nil, serviceError(err)
	}
	return connect.NewResponse(&omnibusv1.DeleteIndexerResponse{}), nil
}

// TestIndexer handles the connectivity-probe RPC.
func (h *IndexerHandler) TestIndexer(
	ctx context.Context, req *connect.Request[omnibusv1.TestIndexerRequest],
) (*connect.Response[omnibusv1.TestIndexerResponse], error) {
	if req.Msg.GetId() <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("id must be positive"))
	}
	res, err := h.svc.Test(ctx, req.Msg.GetId())
	if err != nil {
		return nil, serviceError(err)
	}
	return connect.NewResponse(&omnibusv1.TestIndexerResponse{Ok: res.OK, Detail: res.Detail}), nil
}

// indexerToProto maps a masked domain indexer to its proto message. The proto Indexer
// has no api_key field, so masking is structural.
func indexerToProto(i indexerconfig.Indexer) *omnibusv1.Indexer {
	return &omnibusv1.Indexer{
		Id:         i.ID,
		Name:       i.Name,
		Kind:       i.Kind,
		BaseUrl:    i.BaseURL,
		Enabled:    i.Enabled,
		Categories: i.Categories,
		Priority:   i.Priority,
		UseForRss:  i.UseForRSS,
		CreatedAt:  i.CreatedAt,
		UpdatedAt:  i.UpdatedAt,
	}
}

// indexerServiceError maps validation errors to InvalidArgument and the rest to internal.
func indexerServiceError(err error) error {
	if errors.Is(err, indexerconfig.ErrInvalidKind) || errors.Is(err, indexerconfig.ErrMissingField) {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}
