package transport_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/service/downloadclient"
	"github.com/vizvim/omnibus/internal/transport"
)

// fullDownloadClientServicer is a configurable stub covering Get + Update (the existing
// fakeDownloadClientServicer in downloadclient_test.go only exercises Test).
type fullDownloadClientServicer struct {
	getCfg    downloadclient.Config
	getErr    error
	updateCfg downloadclient.Config
	updateErr error
}

func (f *fullDownloadClientServicer) Get(context.Context) (downloadclient.Config, error) {
	return f.getCfg, f.getErr
}

func (f *fullDownloadClientServicer) Update(context.Context, downloadclient.Input) (downloadclient.Config, error) {
	return f.updateCfg, f.updateErr
}

func (f *fullDownloadClientServicer) Test(context.Context) (downloadclient.TestResult, error) {
	return downloadclient.TestResult{}, nil
}

func newFullDownloadClientClient(t *testing.T, svc transport.DownloadClientServicer) omnibusv1connect.DownloadClientServiceClient {
	t.Helper()
	handler := transport.NewDownloadClientHandler(svc)
	path, h := omnibusv1connect.NewDownloadClientServiceHandler(handler)

	mux := http.NewServeMux()
	mux.Handle(path, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return omnibusv1connect.NewDownloadClientServiceClient(srv.Client(), srv.URL)
}

func TestGetDownloadClientConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("maps masked config", func(t *testing.T) {
		t.Parallel()
		client := newFullDownloadClientClient(t, &fullDownloadClientServicer{getCfg: downloadclient.Config{
			URL: "http://sab.test", Category: "comics", Configured: true,
		}})
		resp, err := client.GetDownloadClientConfig(ctx, connect.NewRequest(&omnibusv1.GetDownloadClientConfigRequest{}))
		require.NoError(t, err)
		require.Equal(t, "http://sab.test", resp.Msg.GetConfig().GetUrl())
		require.Equal(t, "comics", resp.Msg.GetConfig().GetCategory())
		require.True(t, resp.Msg.GetConfig().GetConfigured())
	})

	t.Run("service error", func(t *testing.T) {
		t.Parallel()
		client := newFullDownloadClientClient(t, &fullDownloadClientServicer{getErr: errors.New("db down")})
		_, err := client.GetDownloadClientConfig(ctx, connect.NewRequest(&omnibusv1.GetDownloadClientConfigRequest{}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	})
}

func TestUpdateDownloadClientConfig(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("maps updated config", func(t *testing.T) {
		t.Parallel()
		client := newFullDownloadClientClient(t, &fullDownloadClientServicer{updateCfg: downloadclient.Config{
			URL: "http://sab.test", Category: "comics", Configured: true,
		}})
		resp, err := client.UpdateDownloadClientConfig(ctx, connect.NewRequest(&omnibusv1.UpdateDownloadClientConfigRequest{
			Url: "http://sab.test", ApiKey: "secret", Category: "comics",
		}))
		require.NoError(t, err)
		require.Equal(t, "http://sab.test", resp.Msg.GetConfig().GetUrl())
		require.True(t, resp.Msg.GetConfig().GetConfigured())
	})

	t.Run("empty url rejected at edge", func(t *testing.T) {
		t.Parallel()
		client := newFullDownloadClientClient(t, &fullDownloadClientServicer{})
		_, err := client.UpdateDownloadClientConfig(ctx, connect.NewRequest(&omnibusv1.UpdateDownloadClientConfigRequest{
			Url: "",
		}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("validation error maps to invalid argument", func(t *testing.T) {
		t.Parallel()
		client := newFullDownloadClientClient(t, &fullDownloadClientServicer{updateErr: downloadclient.ErrMissingField})
		_, err := client.UpdateDownloadClientConfig(ctx, connect.NewRequest(&omnibusv1.UpdateDownloadClientConfigRequest{
			Url: "http://sab.test",
		}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("internal error", func(t *testing.T) {
		t.Parallel()
		client := newFullDownloadClientClient(t, &fullDownloadClientServicer{updateErr: errors.New("db down")})
		_, err := client.UpdateDownloadClientConfig(ctx, connect.NewRequest(&omnibusv1.UpdateDownloadClientConfigRequest{
			Url: "http://sab.test",
		}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	})
}
