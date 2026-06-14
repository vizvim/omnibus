package transport

import (
	"context"

	"connectrpc.com/connect"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/metadataprovider"
)

// MetadataProviderServicer is the subset of metadataprovider.Service the handler depends on.
// The transport layer depends only on the service package, never on repository/db.
type MetadataProviderServicer interface {
	Get(ctx context.Context) (metadataprovider.Config, error)
	Update(ctx context.Context, in metadataprovider.Input) (metadataprovider.Config, error)
	Test(ctx context.Context, apiKey string) (metadataprovider.TestResult, error)
}

// MetadataProviderHandler implements the generated MetadataProviderServiceHandler over the
// service.
type MetadataProviderHandler struct {
	omnibusv1connect.UnimplementedMetadataProviderServiceHandler
	svc MetadataProviderServicer
}

// NewMetadataProviderHandler builds the MetadataProviderService Connect handler.
func NewMetadataProviderHandler(svc MetadataProviderServicer) *MetadataProviderHandler {
	return &MetadataProviderHandler{svc: svc}
}

// GetMetadataProviderConfig handles the get RPC. The proto config view has no api_key field,
// so the response is masked by construction.
func (h *MetadataProviderHandler) GetMetadataProviderConfig(
	ctx context.Context, _ *connect.Request[omnibusv1.GetMetadataProviderConfigRequest],
) (*connect.Response[omnibusv1.GetMetadataProviderConfigResponse], error) {
	cfg, err := h.svc.Get(ctx)
	if err != nil {
		return nil, metadataProviderServiceError(err)
	}
	return connect.NewResponse(&omnibusv1.GetMetadataProviderConfigResponse{Config: metadataConfigToProto(cfg)}), nil
}

// UpdateMetadataProviderConfig handles the update RPC. An empty api_key leaves the stored key
// intact; the saved key takes effect on the next search with no restart.
func (h *MetadataProviderHandler) UpdateMetadataProviderConfig(
	ctx context.Context, req *connect.Request[omnibusv1.UpdateMetadataProviderConfigRequest],
) (*connect.Response[omnibusv1.UpdateMetadataProviderConfigResponse], error) {
	m := req.Msg
	cfg, err := h.svc.Update(ctx, metadataprovider.Input{
		Provider: m.GetProvider(),
		APIKey:   m.GetApiKey(),
	})
	if err != nil {
		return nil, metadataProviderServiceError(err)
	}
	return connect.NewResponse(&omnibusv1.UpdateMetadataProviderConfigResponse{Config: metadataConfigToProto(cfg)}), nil
}

// TestMetadataProviderConfig handles the key-validation RPC. A bad key (ok=false) is a normal
// response, NOT a connect error — only a genuine internal fault returns CodeInternal.
func (h *MetadataProviderHandler) TestMetadataProviderConfig(
	ctx context.Context, req *connect.Request[omnibusv1.TestMetadataProviderConfigRequest],
) (*connect.Response[omnibusv1.TestMetadataProviderConfigResponse], error) {
	res, err := h.svc.Test(ctx, req.Msg.GetApiKey())
	if err != nil {
		return nil, metadataProviderServiceError(err)
	}
	return connect.NewResponse(&omnibusv1.TestMetadataProviderConfigResponse{Ok: res.OK, Detail: res.Detail}), nil
}

// metadataConfigToProto maps the masked domain Config to its proto view. The proto config
// message has no api_key field, so masking is structural.
func metadataConfigToProto(c metadataprovider.Config) *omnibusv1.MetadataProviderConfig {
	return &omnibusv1.MetadataProviderConfig{
		Provider:   c.Provider,
		Configured: c.Configured,
	}
}

// metadataProviderServiceError maps service errors to connect codes. There are no validation
// sentinels today, so every error is internal.
func metadataProviderServiceError(err error) error {
	return connect.NewError(connect.CodeInternal, err)
}
