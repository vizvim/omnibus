package transport

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/downloadclient"
)

// DownloadClientServicer is the subset of downloadclient.Service the handler depends on.
// The transport layer depends only on the service package, never on repository/db.
type DownloadClientServicer interface {
	Get(ctx context.Context) (downloadclient.Config, error)
	Update(ctx context.Context, in downloadclient.Input) (downloadclient.Config, error)
	Test(ctx context.Context) (downloadclient.TestResult, error)
}

// DownloadClientHandler implements the generated DownloadClientServiceHandler over the
// service.
type DownloadClientHandler struct {
	omnibusv1connect.UnimplementedDownloadClientServiceHandler
	svc DownloadClientServicer
}

// NewDownloadClientHandler builds the DownloadClientService Connect handler.
func NewDownloadClientHandler(svc DownloadClientServicer) *DownloadClientHandler {
	return &DownloadClientHandler{svc: svc}
}

// GetDownloadClientConfig handles the get RPC. The proto config view has no api_key field,
// so the response is masked by construction.
func (h *DownloadClientHandler) GetDownloadClientConfig(
	ctx context.Context, _ *connect.Request[omnibusv1.GetDownloadClientConfigRequest],
) (*connect.Response[omnibusv1.GetDownloadClientConfigResponse], error) {
	cfg, err := h.svc.Get(ctx)
	if err != nil {
		return nil, downloadClientServiceError(err)
	}
	return connect.NewResponse(&omnibusv1.GetDownloadClientConfigResponse{Config: configToProto(cfg)}), nil
}

// UpdateDownloadClientConfig handles the update RPC. An empty api_key leaves the stored
// key intact; an empty url is rejected at the transport edge too.
func (h *DownloadClientHandler) UpdateDownloadClientConfig(
	ctx context.Context, req *connect.Request[omnibusv1.UpdateDownloadClientConfigRequest],
) (*connect.Response[omnibusv1.UpdateDownloadClientConfigResponse], error) {
	m := req.Msg
	if m.GetUrl() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("url must not be empty"))
	}
	cfg, err := h.svc.Update(ctx, downloadclient.Input{
		URL:      m.GetUrl(),
		APIKey:   m.GetApiKey(),
		Category: m.GetCategory(),
	})
	if err != nil {
		return nil, downloadClientServiceError(err)
	}
	return connect.NewResponse(&omnibusv1.UpdateDownloadClientConfigResponse{Config: configToProto(cfg)}), nil
}

// TestDownloadClientConfig handles the connectivity-probe RPC. A failed probe (ok=false)
// is a normal response, NOT a connect error — only a genuine internal fault returns
// CodeInternal.
func (h *DownloadClientHandler) TestDownloadClientConfig(
	ctx context.Context, _ *connect.Request[omnibusv1.TestDownloadClientConfigRequest],
) (*connect.Response[omnibusv1.TestDownloadClientConfigResponse], error) {
	res, err := h.svc.Test(ctx)
	if err != nil {
		return nil, downloadClientServiceError(err)
	}
	return connect.NewResponse(&omnibusv1.TestDownloadClientConfigResponse{Ok: res.OK, Detail: res.Detail}), nil
}

// configToProto maps the masked domain Config to its proto view. The proto config message
// has no api_key field, so masking is structural.
func configToProto(c downloadclient.Config) *omnibusv1.DownloadClientConfig {
	return &omnibusv1.DownloadClientConfig{
		Url:        c.URL,
		Category:   c.Category,
		Configured: c.Configured,
	}
}

// downloadClientServiceError maps validation errors to InvalidArgument and the rest to internal.
func downloadClientServiceError(err error) error {
	if errors.Is(err, downloadclient.ErrMissingField) {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}
