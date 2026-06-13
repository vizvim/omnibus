package indexer_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"github.com/vizvim/omnibus/internal/indexer"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestGatewayAggregatesAcrossProviders(t *testing.T) {
	t.Parallel()
	gw := indexer.NewGateway(quietLogger(), rate.Inf)

	a := indexer.NewFakeProvider("newznab", []indexer.Candidate{{ReleaseKey: "a", Provider: "newznab"}})
	b := indexer.NewFakeProvider("getcomics", []indexer.Candidate{{ReleaseKey: "b", Provider: "getcomics"}})

	got, err := gw.Search(context.Background(), []indexer.IndexerProvider{a, b}, "q")
	require.NoError(t, err)
	require.Len(t, got, 2)
}

func TestGatewaySkipsFailingProvider(t *testing.T) {
	t.Parallel()
	gw := indexer.NewGateway(quietLogger(), rate.Inf)

	ok := indexer.NewFakeProvider("newznab", []indexer.Candidate{{ReleaseKey: "ok"}})
	bad := indexer.NewFailingFakeProvider("getcomics", errors.New("boom"))

	// A failing provider is logged + skipped; the other provider's candidates still return.
	got, err := gw.Search(context.Background(), []indexer.IndexerProvider{bad, ok}, "q")
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "ok", got[0].ReleaseKey)
}

func TestGatewayPacesPerHostBeforeFetch(t *testing.T) {
	t.Parallel()
	// One provider, a slow per-host limiter (2 req/sec). The first Wait passes the
	// initial burst token immediately; the second call to the SAME host must block
	// ~500ms, proving Wait is enforced before each fetch.
	gw := indexer.NewGateway(quietLogger(), rate.Limit(2))
	p := indexer.NewFakeProvider("newznab", []indexer.Candidate{{ReleaseKey: "x"}})

	ctx := context.Background()
	_, err := gw.Search(ctx, []indexer.IndexerProvider{p}, "q") // consumes burst token
	require.NoError(t, err)

	start := time.Now()
	_, err = gw.Search(ctx, []indexer.IndexerProvider{p}, "q") // must wait for a token
	require.NoError(t, err)
	require.GreaterOrEqual(t, time.Since(start), 300*time.Millisecond)
}

func TestGatewayWaitRespectsContextCancellation(t *testing.T) {
	t.Parallel()
	gw := indexer.NewGateway(quietLogger(), rate.Limit(1))
	p := indexer.NewFakeProvider("newznab", []indexer.Candidate{{ReleaseKey: "x"}})

	_, err := gw.Search(context.Background(), []indexer.IndexerProvider{p}, "q") // burst token
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled before the next Wait can acquire a token
	_, err = gw.Search(ctx, []indexer.IndexerProvider{p}, "q")
	require.Error(t, err)
}
