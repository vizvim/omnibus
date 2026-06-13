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
	"github.com/vizvim/omnibus/internal/downloadclient"
	"github.com/vizvim/omnibus/internal/transport"
)

// fakeDownloadClientServicer is a hand-rolled stub of the servicer the handler depends on.
type fakeDownloadClientServicer struct {
	testResult downloadclient.TestResult
	testErr    error
}

func (f *fakeDownloadClientServicer) Get(context.Context) (downloadclient.Config, error) {
	return downloadclient.Config{}, nil
}

func (f *fakeDownloadClientServicer) Update(context.Context, downloadclient.Input) (downloadclient.Config, error) {
	return downloadclient.Config{}, nil
}

func (f *fakeDownloadClientServicer) Test(context.Context) (downloadclient.TestResult, error) {
	return f.testResult, f.testErr
}

func newDownloadClientTestClient(t *testing.T, svc transport.DownloadClientServicer) omnibusv1connect.DownloadClientServiceClient {
	t.Helper()
	handler := transport.NewDownloadClientHandler(svc)
	path, h := omnibusv1connect.NewDownloadClientServiceHandler(handler)

	mux := http.NewServeMux()
	mux.Handle(path, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return omnibusv1connect.NewDownloadClientServiceClient(srv.Client(), srv.URL)
}

func TestTestDownloadClientConfigReturnsOKInBody(t *testing.T) {
	t.Parallel()
	client := newDownloadClientTestClient(t, &fakeDownloadClientServicer{
		testResult: downloadclient.TestResult{OK: true, Detail: "connected"},
	})

	resp, err := client.TestDownloadClientConfig(context.Background(),
		connect.NewRequest(&omnibusv1.TestDownloadClientConfigRequest{}))
	require.NoError(t, err)
	require.True(t, resp.Msg.GetOk())
	require.Equal(t, "connected", resp.Msg.GetDetail())
}

func TestTestDownloadClientConfigFailedProbeIsNotConnectError(t *testing.T) {
	t.Parallel()
	client := newDownloadClientTestClient(t, &fakeDownloadClientServicer{
		testResult: downloadclient.TestResult{OK: false, Detail: "not configured"},
	})

	resp, err := client.TestDownloadClientConfig(context.Background(),
		connect.NewRequest(&omnibusv1.TestDownloadClientConfigRequest{}))
	require.NoError(t, err, "an ok=false probe must be a normal response, not a connect error")
	require.False(t, resp.Msg.GetOk())
	require.Equal(t, "not configured", resp.Msg.GetDetail())
}

func TestTestDownloadClientConfigInternalFaultIsConnectError(t *testing.T) {
	t.Parallel()
	client := newDownloadClientTestClient(t, &fakeDownloadClientServicer{
		testErr: errors.New("boom"),
	})

	_, err := client.TestDownloadClientConfig(context.Background(),
		connect.NewRequest(&omnibusv1.TestDownloadClientConfigRequest{}))
	require.Error(t, err, "a genuine internal fault must surface as a connect error")
	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}
