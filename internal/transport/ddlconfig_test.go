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
	"github.com/vizvim/omnibus/internal/ddlconfig"
	"github.com/vizvim/omnibus/internal/transport"
)

// fakeDDLConfigServicer is a hand-rolled stub of the servicer the handler depends on.
type fakeDDLConfigServicer struct {
	getResult    ddlconfig.Config
	updateResult ddlconfig.Config

	gotInput ddlconfig.Input
}

func (f *fakeDDLConfigServicer) Get(context.Context) (ddlconfig.Config, error) {
	return f.getResult, nil
}

func (f *fakeDDLConfigServicer) Update(_ context.Context, in ddlconfig.Input) (ddlconfig.Config, error) {
	f.gotInput = in
	return f.updateResult, nil
}

func newDDLConfigTestClient(t *testing.T, svc transport.DDLConfigServicer) omnibusv1connect.DDLConfigServiceClient {
	t.Helper()
	handler := transport.NewDDLConfigHandler(svc)
	path, h := omnibusv1connect.NewDDLConfigServiceHandler(handler)

	mux := http.NewServeMux()
	mux.Handle(path, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return omnibusv1connect.NewDDLConfigServiceClient(srv.Client(), srv.URL)
}

func TestGetDDLConfigReturnsServiceConfig(t *testing.T) {
	t.Parallel()
	client := newDDLConfigTestClient(t, &fakeDDLConfigServicer{
		getResult: ddlconfig.Config{Enabled: true},
	})

	resp, err := client.GetDDLConfig(context.Background(),
		connect.NewRequest(&omnibusv1.GetDDLConfigRequest{}))
	require.NoError(t, err)
	require.True(t, resp.Msg.GetConfig().GetEnabled())
}

func TestGetDDLConfigDefaultsDisabled(t *testing.T) {
	t.Parallel()
	client := newDDLConfigTestClient(t, &fakeDDLConfigServicer{
		getResult: ddlconfig.DefaultDDLConfig(),
	})

	resp, err := client.GetDDLConfig(context.Background(),
		connect.NewRequest(&omnibusv1.GetDDLConfigRequest{}))
	require.NoError(t, err)
	require.False(t, resp.Msg.GetConfig().GetEnabled())
}

func TestUpdateDDLConfigMapsInputAndReturnsConfig(t *testing.T) {
	t.Parallel()
	fake := &fakeDDLConfigServicer{
		updateResult: ddlconfig.Config{Enabled: true},
	}
	client := newDDLConfigTestClient(t, fake)

	resp, err := client.UpdateDDLConfig(context.Background(),
		connect.NewRequest(&omnibusv1.UpdateDDLConfigRequest{Enabled: true}))
	require.NoError(t, err)
	require.True(t, resp.Msg.GetConfig().GetEnabled())

	// The handler maps the proto request faithfully into the service Input.
	require.True(t, fake.gotInput.Enabled)
}
