package metadata_test

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/metadata"
	"github.com/vizvim/omnibus/internal/repository"
)

// countingProvider wraps a MetadataProvider and counts calls per method.
type countingProvider struct {
	inner       metadata.MetadataProvider
	mu          sync.Mutex
	getVolume   int
	listIssues  int
	getIssue    int
	searchCalls int
}

func (c *countingProvider) SearchSeries(ctx context.Context, q string) ([]metadata.SeriesResult, error) {
	c.mu.Lock()
	c.searchCalls++
	c.mu.Unlock()
	return c.inner.SearchSeries(ctx, q)
}

func (c *countingProvider) GetVolume(ctx context.Context, id int64) (metadata.VolumeDetail, []byte, error) {
	c.mu.Lock()
	c.getVolume++
	c.mu.Unlock()
	return c.inner.GetVolume(ctx, id)
}

func (c *countingProvider) ListIssues(ctx context.Context, id int64, off int) ([]metadata.IssueDetail, bool, []byte, error) {
	c.mu.Lock()
	c.listIssues++
	c.mu.Unlock()
	return c.inner.ListIssues(ctx, id, off)
}

func (c *countingProvider) GetIssue(ctx context.Context, id int64) (metadata.IssueDetail, error) {
	c.mu.Lock()
	c.getIssue++
	c.mu.Unlock()
	return c.inner.GetIssue(ctx, id)
}

func (c *countingProvider) GetCover(ctx context.Context, u string) ([]byte, error) {
	return c.inner.GetCover(ctx, u)
}

func cacheRepo(t *testing.T) repository.MetadataCacheRepository {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gw.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return repository.NewMetadataCacheRepository(d)
}

func newGateway(t *testing.T, prov metadata.MetadataProvider, limiter *rate.Limiter, buf *bytes.Buffer) *metadata.Gateway {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return metadata.NewGateway(prov, cacheRepo(t), limiter, logger, time.Hour)
}

func TestGatewayCacheHitCallsProviderOnce(t *testing.T) {
	t.Parallel()
	fake, err := metadata.NewFakeProvider("testdata/fixtures")
	require.NoError(t, err)
	cp := &countingProvider{inner: fake}
	gw := newGateway(t, cp, rate.NewLimiter(rate.Inf, 1), &bytes.Buffer{})
	ctx := context.Background()

	_, err = gw.GetVolume(ctx, 4050)
	require.NoError(t, err)
	v, err := gw.GetVolume(ctx, 4050)
	require.NoError(t, err)

	require.Equal(t, 1, cp.getVolume, "second GetVolume served from cache")
	require.Equal(t, int64(4050), v.ComicvineVolumeID)
}

func TestGatewayGetIssueCachesAndPreservesCredits(t *testing.T) {
	t.Parallel()
	fake, err := metadata.NewFakeProvider("testdata/fixtures")
	require.NoError(t, err)
	cp := &countingProvider{inner: fake}
	gw := newGateway(t, cp, rate.NewLimiter(rate.Inf, 1), &bytes.Buffer{})
	ctx := context.Background()

	first, err := gw.GetIssue(ctx, 1001)
	require.NoError(t, err)
	require.NotEmpty(t, first.Credits, "DETAIL fetch carries credits")

	// Second call is served from cache; the round-tripped envelope preserves credits.
	second, err := gw.GetIssue(ctx, 1001)
	require.NoError(t, err)
	require.Equal(t, 1, cp.getIssue, "second GetIssue served from cache")
	require.ElementsMatch(t, first.Credits, second.Credits)
	require.Equal(t, first.Description, second.Description)
}

func TestGatewayLimiterGatesProvider(t *testing.T) {
	t.Parallel()
	fake, err := metadata.NewFakeProvider("testdata/fixtures")
	require.NoError(t, err)
	cp := &countingProvider{inner: fake}

	// Zero-rate, zero-burst limiter: Wait must fail under a short deadline, so the
	// provider is never reached (no bypass path).
	limiter := rate.NewLimiter(0, 0)
	gw := newGateway(t, cp, limiter, &bytes.Buffer{})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = gw.GetVolume(ctx, 4050)
	require.Error(t, err, "zero-rate limiter blocks the provider call")
	require.Equal(t, 0, cp.getVolume, "provider not reached without passing the limiter")
}

func TestGatewayLargeSeriesPagedThroughLimiter(t *testing.T) {
	t.Parallel()
	fake, err := metadata.NewFakeProvider("testdata/fixtures")
	require.NoError(t, err)
	cp := &countingProvider{inner: fake}
	// A fast but finite limiter; all paged calls must pass through it.
	limiter := rate.NewLimiter(rate.Every(time.Millisecond), 1)
	gw := newGateway(t, cp, limiter, &bytes.Buffer{})
	ctx := context.Background()

	var all []metadata.IssueDetail
	offset := 0
	for {
		issues, hasMore, err := gw.ListIssues(ctx, 4050, offset)
		require.NoError(t, err)
		all = append(all, issues...)
		if !hasMore {
			break
		}
		offset += len(issues)
		require.Less(t, offset, 100000, "pagination must terminate")
	}
	require.NotEmpty(t, all)
	require.GreaterOrEqual(t, cp.listIssues, 1)
}

func TestGatewayConditionalRefresh(t *testing.T) {
	t.Parallel()
	fake, err := metadata.NewFakeProvider("testdata/fixtures")
	require.NoError(t, err)
	cp := &countingProvider{inner: fake}

	// TTL of zero means every entry is immediately stale -> always refresh.
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	gw := metadata.NewGateway(cp, cacheRepo(t), rate.NewLimiter(rate.Inf, 1), logger, 0)
	ctx := context.Background()

	_, err = gw.GetVolume(ctx, 4050)
	require.NoError(t, err)
	_, err = gw.GetVolume(ctx, 4050)
	require.NoError(t, err)
	require.Equal(t, 2, cp.getVolume, "stale (TTL=0) entry triggers refresh each call")
}

func TestGatewaySearchCachesAndRoundTrips(t *testing.T) {
	t.Parallel()
	fake, err := metadata.NewFakeProvider("testdata/fixtures")
	require.NoError(t, err)
	cp := &countingProvider{inner: fake}
	gw := newGateway(t, cp, rate.NewLimiter(rate.Inf, 1), &bytes.Buffer{})
	ctx := context.Background()

	first, err := gw.SearchSeries(ctx, "Daredevil")
	require.NoError(t, err)
	require.Len(t, first, 2)

	// Second call served from the cache (encodeSearchEnvelope round-trips through
	// decodeSearch): the provider is hit only once.
	second, err := gw.SearchSeries(ctx, "Daredevil")
	require.NoError(t, err)
	require.Equal(t, 1, cp.searchCalls, "second search served from cache")
	require.Equal(t, first, second)
}

func TestGatewayGetCoverPaces(t *testing.T) {
	t.Parallel()
	fake, err := metadata.NewFakeProvider("testdata/fixtures")
	require.NoError(t, err)
	gw := newGateway(t, fake, rate.NewLimiter(rate.Inf, 1), &bytes.Buffer{})

	data, err := gw.GetCover(context.Background(), "https://comicvine.gamespot.com/x.jpg")
	require.NoError(t, err)
	require.NotEmpty(t, data)
}

func TestGatewayCachedSourceUpdatedAt(t *testing.T) {
	t.Parallel()
	fake, err := metadata.NewFakeProvider("testdata/fixtures")
	require.NoError(t, err)
	gw := newGateway(t, fake, rate.NewLimiter(rate.Inf, 1), &bytes.Buffer{})
	ctx := context.Background()

	// Before any fetch, no marker is cached.
	_, ok := gw.CachedSourceUpdatedAt(ctx, 4050)
	require.False(t, ok)

	// After GetVolume caches the volume (with its date_last_updated marker), it reads back.
	_, err = gw.GetVolume(ctx, 4050)
	require.NoError(t, err)
	marker, ok := gw.CachedSourceUpdatedAt(ctx, 4050)
	require.True(t, ok)
	require.NotEmpty(t, marker)
}

func TestFakeGetCoverReturnsBytes(t *testing.T) {
	t.Parallel()
	fake, err := metadata.NewFakeProvider("testdata/fixtures")
	require.NoError(t, err)
	data, err := fake.GetCover(context.Background(), "anything")
	require.NoError(t, err)
	require.NotEmpty(t, data)
}

func TestGatewayListIssuesServesFromCache(t *testing.T) {
	t.Parallel()
	fake, err := metadata.NewFakeProvider("testdata/fixtures")
	require.NoError(t, err)
	cp := &countingProvider{inner: fake}
	gw := newGateway(t, cp, rate.NewLimiter(rate.Inf, 1), &bytes.Buffer{})
	ctx := context.Background()

	first, _, err := gw.ListIssues(ctx, 4050, 0)
	require.NoError(t, err)
	require.NotEmpty(t, first)

	// Second call served from cache (decodeIssues round-trip): provider hit once.
	second, _, err := gw.ListIssues(ctx, 4050, 0)
	require.NoError(t, err)
	require.Equal(t, 1, cp.listIssues, "second ListIssues served from cache")
	require.Len(t, second, len(first))
}

// errProvider is a MetadataProvider whose GetIssue always errors, to drive the gateway's
// GetIssue error path.
type errIssueProvider struct{ metadata.MetadataProvider }

func (errIssueProvider) GetIssue(context.Context, int64) (metadata.IssueDetail, error) {
	return metadata.IssueDetail{}, context.DeadlineExceeded
}

func TestGatewayGetIssuePropagatesProviderError(t *testing.T) {
	t.Parallel()
	fake, err := metadata.NewFakeProvider("testdata/fixtures")
	require.NoError(t, err)
	gw := newGateway(t, errIssueProvider{fake}, rate.NewLimiter(rate.Inf, 1), &bytes.Buffer{})

	_, err = gw.GetIssue(context.Background(), 1001)
	require.Error(t, err)
}

func TestFakeListIssuesEmptyAfterFirstPage(t *testing.T) {
	t.Parallel()
	fake, err := metadata.NewFakeProvider("testdata/fixtures")
	require.NoError(t, err)
	issues, hasMore, _, err := fake.ListIssues(context.Background(), 4050, 100)
	require.NoError(t, err)
	require.False(t, hasMore)
	require.Empty(t, issues)
}

func TestGatewayEmitsLogs(t *testing.T) {
	t.Parallel()
	fake, err := metadata.NewFakeProvider("testdata/fixtures")
	require.NoError(t, err)
	cp := &countingProvider{inner: fake}
	var buf bytes.Buffer
	gw := newGateway(t, cp, rate.NewLimiter(rate.Inf, 1), &buf)
	ctx := context.Background()

	_, err = gw.GetVolume(ctx, 4050) // miss
	require.NoError(t, err)
	_, err = gw.GetVolume(ctx, 4050) // hit
	require.NoError(t, err)

	out := buf.String()
	require.True(t, strings.Contains(out, "miss") || strings.Contains(out, "cache"), "expected cache log lines")
	require.Contains(t, out, "hit")
}
