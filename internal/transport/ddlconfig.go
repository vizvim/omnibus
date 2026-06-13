package transport

import (
	"context"

	"connectrpc.com/connect"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/ddlconfig"
)

// DDLConfigServicer is the subset of ddlconfig.Service the handler depends on. The
// transport layer depends only on the service package, never on repository/db.
type DDLConfigServicer interface {
	Get(ctx context.Context) (ddlconfig.Config, error)
	Update(ctx context.Context, in ddlconfig.Input) (ddlconfig.Config, error)
}

// DDLConfigHandler implements the generated DDLConfigServiceHandler over the service.
type DDLConfigHandler struct {
	omnibusv1connect.UnimplementedDDLConfigServiceHandler
	svc DDLConfigServicer
}

// NewDDLConfigHandler builds the DDLConfigService Connect handler.
func NewDDLConfigHandler(svc DDLConfigServicer) *DDLConfigHandler {
	return &DDLConfigHandler{svc: svc}
}

// GetDDLConfig handles the get RPC. On a fresh DB the service returns the default-OFF
// posture; there are no secrets to mask.
func (h *DDLConfigHandler) GetDDLConfig(
	ctx context.Context, _ *connect.Request[omnibusv1.GetDDLConfigRequest],
) (*connect.Response[omnibusv1.GetDDLConfigResponse], error) {
	cfg, err := h.svc.Get(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&omnibusv1.GetDDLConfigResponse{Config: ddlConfigToProto(cfg)}), nil
}

// UpdateDDLConfig handles the update RPC. It maps the proto request to the service Input
// and returns the updated config. There are no required fields, so the only failure mode
// is an internal persistence fault.
func (h *DDLConfigHandler) UpdateDDLConfig(
	ctx context.Context, req *connect.Request[omnibusv1.UpdateDDLConfigRequest],
) (*connect.Response[omnibusv1.UpdateDDLConfigResponse], error) {
	cfg, err := h.svc.Update(ctx, ddlconfig.Input{Enabled: req.Msg.GetEnabled()})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&omnibusv1.UpdateDDLConfigResponse{Config: ddlConfigToProto(cfg)}), nil
}

// ddlConfigToProto maps the domain Config to its proto view. There are no secrets, so
// every field is emitted verbatim.
func ddlConfigToProto(c ddlconfig.Config) *omnibusv1.DDLConfig {
	return &omnibusv1.DDLConfig{Enabled: c.Enabled}
}
