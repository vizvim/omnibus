package transport_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	omnibusv1 "github.com/vizvim/omnibus/gen/go/omnibus/v1"
	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/provider/metadata"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/service/series"
	"github.com/vizvim/omnibus/internal/transport"
)

func newClient(t *testing.T) (omnibusv1connect.SeriesServiceClient, *series.Service) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	fake, err := metadata.NewFakeProvider("../provider/metadata/testdata/fixtures")
	require.NoError(t, err)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := metadata.NewGateway(fake, repository.NewMetadataCacheRepository(d), rate.NewLimiter(rate.Inf, 1), logger, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	t.Cleanup(func() { cancel(); wg.Wait() })

	svc := series.New(series.Deps{
		Gateway: gw, Repos: repository.NewRepositories(d),
		AttemptCap: 5, Logger: logger, LifeCtx: ctx, WaitGroup: &wg,
	})

	handler := transport.NewSeriesHandler(svc)
	mux := http.NewServeMux()
	p, h := omnibusv1connect.NewSeriesServiceHandler(handler)
	mux.Handle(p, h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := omnibusv1connect.NewSeriesServiceClient(srv.Client(), srv.URL)
	return client, svc
}

func TestRPCListAndAddAndGet(t *testing.T) {
	t.Parallel()
	client, _ := newClient(t)
	ctx := context.Background()

	// ListSeries on an empty DB round-trips.
	listResp, err := client.ListSeries(ctx, connect.NewRequest(&omnibusv1.ListSeriesRequest{}))
	require.NoError(t, err)
	require.Empty(t, listResp.Msg.GetSeries())

	// AddSeries returns a Series (Success Criterion 1/2).
	addResp, err := client.AddSeries(ctx, connect.NewRequest(&omnibusv1.AddSeriesRequest{ComicvineVolumeId: 4050}))
	require.NoError(t, err)
	require.Equal(t, int64(4050), addResp.Msg.GetSeries().GetComicvineVolumeId())
	seriesID := addResp.Msg.GetSeries().GetId()

	// GetSeries returns the series + issues shape once import lands.
	require.Eventually(t, func() bool {
		gr, gerr := client.GetSeries(ctx, connect.NewRequest(&omnibusv1.GetSeriesRequest{SeriesId: seriesID}))
		return gerr == nil && len(gr.Msg.GetIssues()) >= 4
	}, 5*time.Second, 25*time.Millisecond)

	getResp, err := client.GetSeries(ctx, connect.NewRequest(&omnibusv1.GetSeriesRequest{SeriesId: seriesID}))
	require.NoError(t, err)
	require.Equal(t, "Marvel", getResp.Msg.GetPublisher())
	// cover_url, when present, points at the backend /covers route, never comicvine.com.
	for _, iss := range getResp.Msg.GetIssues() {
		if iss.GetCoverUrl() != "" {
			require.Contains(t, iss.GetCoverUrl(), "/covers/")
			require.NotContains(t, iss.GetCoverUrl(), "comicvine")
		}
	}
}

func TestRPCInputValidation(t *testing.T) {
	t.Parallel()
	client, _ := newClient(t)
	ctx := context.Background()

	_, err := client.AddSeries(ctx, connect.NewRequest(&omnibusv1.AddSeriesRequest{ComicvineVolumeId: 0}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, err = client.SearchComicVine(ctx, connect.NewRequest(&omnibusv1.SearchComicVineRequest{Query: ""}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	_, err = client.GetSeries(ctx, connect.NewRequest(&omnibusv1.GetSeriesRequest{SeriesId: -1}))
	require.Error(t, err)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}
