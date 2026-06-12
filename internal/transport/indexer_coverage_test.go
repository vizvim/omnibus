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
	"github.com/vizvim/omnibus/internal/service/indexer"
	"github.com/vizvim/omnibus/internal/transport"
)

// fakeIndexerServicer is a hand-rolled stub of the IndexerServicer the handler depends on,
// exercising the Update/Delete/Test handler branches (including the masked mapping and
// error translation) deterministically without a DB or real HTTP prober.
type fakeIndexerServicer struct {
	list    []indexer.Indexer
	listErr error

	created    indexer.Indexer
	createErr  error
	updated    indexer.Indexer
	updateErr  error
	deleteErr  error
	testResult indexer.TestResult
	testErr    error
}

func (f *fakeIndexerServicer) List(context.Context) ([]indexer.Indexer, error) {
	return f.list, f.listErr
}

func (f *fakeIndexerServicer) Create(context.Context, indexer.Input) (indexer.Indexer, error) {
	return f.created, f.createErr
}

func (f *fakeIndexerServicer) Update(context.Context, int64, indexer.Input) (indexer.Indexer, error) {
	return f.updated, f.updateErr
}

func (f *fakeIndexerServicer) Delete(context.Context, int64) error { return f.deleteErr }

func (f *fakeIndexerServicer) Test(context.Context, int64) (indexer.TestResult, error) {
	return f.testResult, f.testErr
}

func newFakeIndexerClient(t *testing.T, svc transport.IndexerServicer) omnibusv1connect.IndexerServiceClient {
	t.Helper()
	handler := transport.NewIndexerHandler(svc)
	path, h := omnibusv1connect.NewIndexerServiceHandler(handler)

	mux := http.NewServeMux()
	mux.Handle(path, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return omnibusv1connect.NewIndexerServiceClient(srv.Client(), srv.URL)
}

func TestIndexerHandlerUpdate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("maps updated indexer", func(t *testing.T) {
		t.Parallel()
		client := newFakeIndexerClient(t, &fakeIndexerServicer{updated: indexer.Indexer{
			ID: 1, Name: "nzb", Kind: "newznab", BaseURL: "http://nzb.test", Enabled: true, Priority: 5,
		}})
		resp, err := client.UpdateIndexer(ctx, connect.NewRequest(&omnibusv1.UpdateIndexerRequest{
			Id: 1, Name: "nzb", Kind: "newznab", BaseUrl: "http://nzb.test", Enabled: true,
		}))
		require.NoError(t, err)
		require.Equal(t, "nzb", resp.Msg.GetIndexer().GetName())
		require.Equal(t, int32(5), resp.Msg.GetIndexer().GetPriority())
	})

	t.Run("invalid id", func(t *testing.T) {
		t.Parallel()
		client := newFakeIndexerClient(t, &fakeIndexerServicer{})
		_, err := client.UpdateIndexer(ctx, connect.NewRequest(&omnibusv1.UpdateIndexerRequest{
			Id: 0, Name: "nzb", BaseUrl: "http://nzb.test",
		}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("empty name", func(t *testing.T) {
		t.Parallel()
		client := newFakeIndexerClient(t, &fakeIndexerServicer{})
		_, err := client.UpdateIndexer(ctx, connect.NewRequest(&omnibusv1.UpdateIndexerRequest{
			Id: 1, Name: "", BaseUrl: "http://nzb.test",
		}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("validation error maps to invalid argument", func(t *testing.T) {
		t.Parallel()
		client := newFakeIndexerClient(t, &fakeIndexerServicer{updateErr: indexer.ErrInvalidKind})
		_, err := client.UpdateIndexer(ctx, connect.NewRequest(&omnibusv1.UpdateIndexerRequest{
			Id: 1, Name: "x", Kind: "bogus", BaseUrl: "http://nzb.test",
		}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("internal error", func(t *testing.T) {
		t.Parallel()
		client := newFakeIndexerClient(t, &fakeIndexerServicer{updateErr: errors.New("db down")})
		_, err := client.UpdateIndexer(ctx, connect.NewRequest(&omnibusv1.UpdateIndexerRequest{
			Id: 1, Name: "x", Kind: "newznab", BaseUrl: "http://nzb.test",
		}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	})
}

func TestIndexerHandlerDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("deletes", func(t *testing.T) {
		t.Parallel()
		client := newFakeIndexerClient(t, &fakeIndexerServicer{})
		_, err := client.DeleteIndexer(ctx, connect.NewRequest(&omnibusv1.DeleteIndexerRequest{Id: 1}))
		require.NoError(t, err)
	})

	t.Run("invalid id", func(t *testing.T) {
		t.Parallel()
		client := newFakeIndexerClient(t, &fakeIndexerServicer{})
		_, err := client.DeleteIndexer(ctx, connect.NewRequest(&omnibusv1.DeleteIndexerRequest{Id: 0}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("service error", func(t *testing.T) {
		t.Parallel()
		client := newFakeIndexerClient(t, &fakeIndexerServicer{deleteErr: errors.New("boom")})
		_, err := client.DeleteIndexer(ctx, connect.NewRequest(&omnibusv1.DeleteIndexerRequest{Id: 1}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	})
}

func TestIndexerHandlerTest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("ok probe in body", func(t *testing.T) {
		t.Parallel()
		client := newFakeIndexerClient(t, &fakeIndexerServicer{testResult: indexer.TestResult{OK: true, Detail: "reachable"}})
		resp, err := client.TestIndexer(ctx, connect.NewRequest(&omnibusv1.TestIndexerRequest{Id: 1}))
		require.NoError(t, err)
		require.True(t, resp.Msg.GetOk())
		require.Equal(t, "reachable", resp.Msg.GetDetail())
	})

	t.Run("failed probe is not a connect error", func(t *testing.T) {
		t.Parallel()
		client := newFakeIndexerClient(t, &fakeIndexerServicer{testResult: indexer.TestResult{OK: false, Detail: "401"}})
		resp, err := client.TestIndexer(ctx, connect.NewRequest(&omnibusv1.TestIndexerRequest{Id: 1}))
		require.NoError(t, err)
		require.False(t, resp.Msg.GetOk())
	})

	t.Run("invalid id", func(t *testing.T) {
		t.Parallel()
		client := newFakeIndexerClient(t, &fakeIndexerServicer{})
		_, err := client.TestIndexer(ctx, connect.NewRequest(&omnibusv1.TestIndexerRequest{Id: 0}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("internal fault", func(t *testing.T) {
		t.Parallel()
		client := newFakeIndexerClient(t, &fakeIndexerServicer{testErr: errors.New("boom")})
		_, err := client.TestIndexer(ctx, connect.NewRequest(&omnibusv1.TestIndexerRequest{Id: 1}))
		require.Error(t, err)
		require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
	})
}

func TestIndexerHandlerListServiceError(t *testing.T) {
	t.Parallel()
	client := newFakeIndexerClient(t, &fakeIndexerServicer{listErr: errors.New("db down")})
	_, err := client.ListIndexers(context.Background(), connect.NewRequest(&omnibusv1.ListIndexersRequest{}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}

func TestIndexerHandlerCreateInternalError(t *testing.T) {
	t.Parallel()
	client := newFakeIndexerClient(t, &fakeIndexerServicer{createErr: errors.New("db down")})
	_, err := client.CreateIndexer(context.Background(), connect.NewRequest(&omnibusv1.CreateIndexerRequest{
		Name: "x", Kind: "newznab", BaseUrl: "http://nzb.test",
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}
