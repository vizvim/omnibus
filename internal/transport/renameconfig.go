package transport

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/renameconfig"
)

// RenameConfigServicer is the subset of renameconfig.Service the handler depends on.
// The transport layer depends only on the service package, never on repository/db.
type RenameConfigServicer interface {
	Get(ctx context.Context) (renameconfig.Config, error)
	Update(ctx context.Context, in renameconfig.Input) (renameconfig.Config, error)
}

// RenameConfigHandler implements the generated RenameConfigServiceHandler over the
// service.
type RenameConfigHandler struct {
	omnibusv1connect.UnimplementedRenameConfigServiceHandler
	svc RenameConfigServicer
}

// NewRenameConfigHandler builds the RenameConfigService Connect handler.
func NewRenameConfigHandler(svc RenameConfigServicer) *RenameConfigHandler {
	return &RenameConfigHandler{svc: svc}
}

// GetRenameConfig handles the get RPC. On a fresh DB the service returns the D-06 Mylar
// defaults; there are no secrets to mask.
func (h *RenameConfigHandler) GetRenameConfig(
	ctx context.Context, _ *connect.Request[omnibusv1.GetRenameConfigRequest],
) (*connect.Response[omnibusv1.GetRenameConfigResponse], error) {
	cfg, err := h.svc.Get(ctx)
	if err != nil {
		return nil, renameConfigServiceError(err)
	}
	return connect.NewResponse(&omnibusv1.GetRenameConfigResponse{Config: renameConfigToProto(cfg)}), nil
}

// UpdateRenameConfig handles the update RPC. It maps the proto request to the service
// Input, returns the updated config, and surfaces validation errors as InvalidArgument.
func (h *RenameConfigHandler) UpdateRenameConfig(
	ctx context.Context, req *connect.Request[omnibusv1.UpdateRenameConfigRequest],
) (*connect.Response[omnibusv1.UpdateRenameConfigResponse], error) {
	m := req.Msg
	cfg, err := h.svc.Update(ctx, renameconfig.Input{
		FolderFormat:        m.GetFolderFormat(),
		FileFormat:          m.GetFileFormat(),
		RenameEnabled:       m.GetRenameEnabled(),
		IssuePaddingEnabled: m.GetIssuePaddingEnabled(),
		PaddingFormat:       m.GetPaddingFormat(),
		ReplaceSpaces:       m.GetReplaceSpaces(),
		Lowercase:           m.GetLowercase(),
	})
	if err != nil {
		return nil, renameConfigServiceError(err)
	}
	return connect.NewResponse(&omnibusv1.UpdateRenameConfigResponse{Config: renameConfigToProto(cfg)}), nil
}

// renameConfigToProto maps the domain Config to its proto view. There are no secrets, so
// every field is emitted verbatim.
func renameConfigToProto(c renameconfig.Config) *omnibusv1.RenameConfig {
	return &omnibusv1.RenameConfig{
		FolderFormat:        c.FolderFormat,
		FileFormat:          c.FileFormat,
		RenameEnabled:       c.RenameEnabled,
		IssuePaddingEnabled: c.IssuePaddingEnabled,
		PaddingFormat:       c.PaddingFormat,
		ReplaceSpaces:       c.ReplaceSpaces,
		Lowercase:           c.Lowercase,
	}
}

// renameConfigServiceError maps validation errors to InvalidArgument and the rest to internal.
func renameConfigServiceError(err error) error {
	if errors.Is(err, renameconfig.ErrMissingField) {
		return connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}
