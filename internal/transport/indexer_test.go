package transport_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/service/indexer"
	"github.com/vizvim/omnibus/internal/transport"
)

func newIndexerClient(t *testing.T) omnibusv1connect.IndexerServiceClient {
	t.Helper()
	path := filepath.Join(t.TempDir(), "idx.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	repos := repository.NewRepositories(d)
	svc := indexer.New(indexer.Deps{Repos: repos})
	handler := transport.NewIndexerHandler(svc)
	path2, h := omnibusv1connect.NewIndexerServiceHandler(handler)

	mux := http.NewServeMux()
	mux.Handle(path2, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return omnibusv1connect.NewIndexerServiceClient(srv.Client(), srv.URL)
}

func TestListIndexersMasksAPIKey(t *testing.T) {
	t.Parallel()
	client := newIndexerClient(t)
	ctx := context.Background()

	_, err := client.CreateIndexer(ctx, connect.NewRequest(&omnibusv1.CreateIndexerRequest{
		Name:    "nzb",
		Kind:    "newznab",
		BaseUrl: "http://nzb.test",
		ApiKey:  "super-secret",
	}))
	require.NoError(t, err)

	resp, err := client.ListIndexers(ctx, connect.NewRequest(&omnibusv1.ListIndexersRequest{}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetIndexers(), 1)

	// Serialize the full response to JSON and assert the secret never appears — the
	// proto Indexer has no api_key field, so masking is structural (T-4-01).
	raw, err := protojson.Marshal(resp.Msg)
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(string(raw)), "api_key")
	require.NotContains(t, string(raw), "super-secret")
}

func TestCreateIndexerRejectsEmptyName(t *testing.T) {
	t.Parallel()
	client := newIndexerClient(t)

	_, err := client.CreateIndexer(context.Background(), connect.NewRequest(&omnibusv1.CreateIndexerRequest{
		Name:    "",
		Kind:    "newznab",
		BaseUrl: "http://nzb.test",
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestCreateIndexerRejectsInvalidKind(t *testing.T) {
	t.Parallel()
	client := newIndexerClient(t)

	_, err := client.CreateIndexer(context.Background(), connect.NewRequest(&omnibusv1.CreateIndexerRequest{
		Name:    "x",
		Kind:    "bogus",
		BaseUrl: "http://nzb.test",
	}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}
