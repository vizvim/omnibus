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
	"github.com/vizvim/omnibus/internal/renameconfig"
	"github.com/vizvim/omnibus/internal/transport"
)

// fakeRenameConfigServicer is a hand-rolled stub of the servicer the handler depends on.
type fakeRenameConfigServicer struct {
	getResult    renameconfig.Config
	updateResult renameconfig.Config
	updateErr    error

	gotInput renameconfig.Input
}

func (f *fakeRenameConfigServicer) Get(context.Context) (renameconfig.Config, error) {
	return f.getResult, nil
}

func (f *fakeRenameConfigServicer) Update(_ context.Context, in renameconfig.Input) (renameconfig.Config, error) {
	f.gotInput = in
	return f.updateResult, f.updateErr
}

func newRenameConfigTestClient(t *testing.T, svc transport.RenameConfigServicer) omnibusv1connect.RenameConfigServiceClient {
	t.Helper()
	handler := transport.NewRenameConfigHandler(svc)
	path, h := omnibusv1connect.NewRenameConfigServiceHandler(handler)

	mux := http.NewServeMux()
	mux.Handle(path, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return omnibusv1connect.NewRenameConfigServiceClient(srv.Client(), srv.URL)
}

func TestGetRenameConfigReturnsServiceConfig(t *testing.T) {
	t.Parallel()
	client := newRenameConfigTestClient(t, &fakeRenameConfigServicer{
		getResult: renameconfig.DefaultRenameConfig(),
	})

	resp, err := client.GetRenameConfig(context.Background(),
		connect.NewRequest(&omnibusv1.GetRenameConfigRequest{}))
	require.NoError(t, err)
	cfg := resp.Msg.GetConfig()
	require.Equal(t, "$Publisher/$Series ($Year)", cfg.GetFolderFormat())
	require.Equal(t, "$Series $VolumeY $Annual #$Issue ($monthname $Year)", cfg.GetFileFormat())
	require.True(t, cfg.GetRenameEnabled())
	require.True(t, cfg.GetIssuePaddingEnabled())
	require.Equal(t, "00x", cfg.GetPaddingFormat())
}

func TestUpdateRenameConfigMapsInputAndReturnsConfig(t *testing.T) {
	t.Parallel()
	fake := &fakeRenameConfigServicer{
		updateResult: renameconfig.Config{
			FolderFormat:  "$Series/$Year",
			FileFormat:    "$Series #$Issue",
			RenameEnabled: true,
			PaddingFormat: "0x",
			ReplaceSpaces: true,
		},
	}
	client := newRenameConfigTestClient(t, fake)

	resp, err := client.UpdateRenameConfig(context.Background(),
		connect.NewRequest(&omnibusv1.UpdateRenameConfigRequest{
			FolderFormat:        "$Series/$Year",
			FileFormat:          "$Series #$Issue",
			RenameEnabled:       true,
			IssuePaddingEnabled: false,
			PaddingFormat:       "0x",
			ReplaceSpaces:       true,
			Lowercase:           false,
		}))
	require.NoError(t, err)
	require.Equal(t, "$Series/$Year", resp.Msg.GetConfig().GetFolderFormat())
	require.True(t, resp.Msg.GetConfig().GetReplaceSpaces())

	// The handler maps the proto request faithfully into the service Input.
	require.Equal(t, "$Series/$Year", fake.gotInput.FolderFormat)
	require.True(t, fake.gotInput.RenameEnabled)
	require.Equal(t, "0x", fake.gotInput.PaddingFormat)
}

func TestUpdateRenameConfigInvalidIsInvalidArgument(t *testing.T) {
	t.Parallel()
	client := newRenameConfigTestClient(t, &fakeRenameConfigServicer{
		updateErr: renameconfig.ErrMissingField,
	})

	_, err := client.UpdateRenameConfig(context.Background(),
		connect.NewRequest(&omnibusv1.UpdateRenameConfigRequest{
			RenameEnabled: true,
		}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}
