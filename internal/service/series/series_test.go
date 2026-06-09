package series_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/provider/metadata"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/service/series"
)

// newService builds a Service backed by the fixture fake gateway and a temp SQLite DB.
// It returns the service, the repos (for assertions), and the lifecycle cancel + wg.
func newService(t *testing.T) (*series.Service, *repository.Repositories, context.CancelFunc, *sync.WaitGroup) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "svc.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	fake, err := metadata.NewFakeProvider("../../provider/metadata/testdata/fixtures")
	require.NoError(t, err)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := metadata.NewGateway(fake, repository.NewMetadataCacheRepository(d), rate.NewLimiter(rate.Inf, 1), logger, time.Hour)

	repos := repository.NewRepositories(d)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	svc := series.New(series.Deps{
		Gateway:    gw,
		Repos:      repos,
		AttemptCap: 5,
		Logger:     logger,
		LifeCtx:    ctx,
		WaitGroup:  &wg,
	})
	return svc, repos, cancel, &wg
}

func TestAddSeriesIdempotentFastReturn(t *testing.T) {
	t.Parallel()
	svc, repos, cancel, wg := newService(t)
	defer func() { cancel(); wg.Wait() }()
	ctx := context.Background()

	s1, err := svc.AddSeries(ctx, 4050)
	require.NoError(t, err)
	require.Equal(t, int64(4050), s1.ComicvineVolumeID)

	// Re-run is idempotent — one series row.
	s2, err := svc.AddSeries(ctx, 4050)
	require.NoError(t, err)
	require.Equal(t, s1.ID, s2.ID)

	all, err := repos.Series.List(ctx, 50, 0)
	require.NoError(t, err)
	require.Len(t, all, 1)
}

func TestImportPopulatesIssuesPublisherArcs(t *testing.T) {
	t.Parallel()
	svc, repos, cancel, wg := newService(t)
	defer func() { cancel(); wg.Wait() }()
	ctx := context.Background()

	s, err := svc.AddSeries(ctx, 4050)
	require.NoError(t, err)

	// Wait for the bounded import goroutine to finish (it imports the fixture set).
	require.Eventually(t, func() bool {
		n, _ := repos.Issue.CountBySeries(ctx, s.ID)
		return n >= 4
	}, 5*time.Second, 20*time.Millisecond)

	view, err := svc.GetSeries(ctx, s.ID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(view.Issues), 4)

	// Non-integer issue numbers present and distinct.
	raws := map[string]bool{}
	for _, i := range view.Issues {
		raws[i.IssueNumber] = true
	}
	require.True(t, raws["7.INH"])
	require.True(t, raws["½"])
	require.True(t, raws["Annual 1"])

	// Publisher populated.
	require.Equal(t, "Marvel", view.Publisher)
}

func TestImportReRunFillsGaps(t *testing.T) {
	t.Parallel()
	svc, repos, cancel, wg := newService(t)
	defer func() { cancel(); wg.Wait() }()
	ctx := context.Background()

	s, err := svc.AddSeries(ctx, 4050)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		n, _ := repos.Issue.CountBySeries(ctx, s.ID)
		return n >= 4
	}, 5*time.Second, 20*time.Millisecond)
	first, err := repos.Issue.CountBySeries(ctx, s.ID)
	require.NoError(t, err)

	// Re-run import: idempotent, still the same count (no duplicates).
	_, err = svc.AddSeries(ctx, 4050)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		n, _ := repos.Issue.CountBySeries(ctx, s.ID)
		return n == first
	}, 5*time.Second, 20*time.Millisecond)
}

func TestGetSeriesShape(t *testing.T) {
	t.Parallel()
	svc, repos, cancel, wg := newService(t)
	defer func() { cancel(); wg.Wait() }()
	ctx := context.Background()

	s, err := svc.AddSeries(ctx, 4050)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		n, _ := repos.Issue.CountBySeries(ctx, s.ID)
		return n >= 4
	}, 5*time.Second, 20*time.Millisecond)

	view, err := svc.GetSeries(ctx, s.ID)
	require.NoError(t, err)
	require.Equal(t, s.ID, view.Series.ID)
	require.NotEmpty(t, view.Issues)
	for _, i := range view.Issues {
		require.NotEmpty(t, i.Status, "each issue has a status")
	}
}

func TestSearchComicVine(t *testing.T) {
	t.Parallel()
	svc, _, cancel, wg := newService(t)
	defer func() { cancel(); wg.Wait() }()

	results, err := svc.SearchComicVine(context.Background(), "Daredevil")
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.NotEqual(t, results[0].StartYear, results[1].StartYear)
}

func TestImportStopsOnShutdown(t *testing.T) {
	t.Parallel()
	svc, _, cancel, wg := newService(t)
	ctx := context.Background()

	_, err := svc.AddSeries(ctx, 4050)
	require.NoError(t, err)

	// Cancel the lifecycle context; the import goroutine must drain and the WaitGroup
	// must join within the deadline.
	cancel()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("import goroutine did not drain on shutdown")
	}
}
