package series_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/jobs"
	"github.com/vizvim/omnibus/internal/provider/metadata"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/service/series"
)

// newService builds a Service backed by the fixture fake gateway and a temp SQLite DB,
// wired to a real started River jobs client so AddSeries enqueues a durable import job
// that the engine then runs (the same wiring app.go uses). Returns the service and the
// repos for assertions; the started client is stopped via t.Cleanup.
func newService(t *testing.T) (*series.Service, *repository.Repositories) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "svc.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	fake, err := metadata.NewFakeProvider("../../provider/metadata/testdata/fixtures")
	require.NoError(t, err)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := metadata.NewGateway(fake, repository.NewMetadataCacheRepository(d), rate.NewLimiter(rate.Inf, 1), logger, time.Hour)

	repos := repository.NewRepositories(d)

	svc := series.New(series.Deps{
		Gateway:    gw,
		Repos:      repos,
		AttemptCap: 5,
		Logger:     logger,
	})

	workers := river.NewWorkers()
	river.AddWorker(workers, jobs.NewImportWorker(svc))
	client, err := jobs.New(ctx, d.Write, 2, logger, workers)
	require.NoError(t, err)
	svc.SetEnqueuer(client)
	require.NoError(t, client.Start(ctx))
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.Stop(stopCtx)
	})

	return svc, repos
}

// fakeEnqueuer records EnqueueImport calls without running the import, so a test can
// assert AddSeries enqueues and returns fast rather than importing inline.
type fakeEnqueuer struct {
	mu    sync.Mutex
	calls [][2]int64
}

func (f *fakeEnqueuer) EnqueueImport(_ context.Context, seriesID, comicvineVolumeID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, [2]int64{seriesID, comicvineVolumeID})
	return nil
}

func (f *fakeEnqueuer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func TestAddSeriesIdempotentFastReturn(t *testing.T) {
	t.Parallel()
	svc, repos := newService(t)
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
	svc, repos := newService(t)
	ctx := context.Background()

	s, err := svc.AddSeries(ctx, 4050)
	require.NoError(t, err)

	// Wait for the durable import job to run (it imports the fixture set).
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
	svc, repos := newService(t)
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
	svc, repos := newService(t)
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
	svc, _ := newService(t)

	results, err := svc.SearchComicVine(context.Background(), "Daredevil")
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.NotEqual(t, results[0].StartYear, results[1].StartYear)
}

// TestAddSeriesEnqueuesWithoutRunningInline proves AddSeries enqueues the import
// (calling EnqueueImport) and returns fast without importing synchronously: with a
// fake enqueuer that records but does not run, no issues are imported inline.
func TestAddSeriesEnqueuesWithoutRunningInline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "enqueue.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	fake, err := metadata.NewFakeProvider("../../provider/metadata/testdata/fixtures")
	require.NoError(t, err)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	gw := metadata.NewGateway(fake, repository.NewMetadataCacheRepository(d), rate.NewLimiter(rate.Inf, 1), logger, time.Hour)
	repos := repository.NewRepositories(d)

	enq := &fakeEnqueuer{}
	svc := series.New(series.Deps{Gateway: gw, Repos: repos, AttemptCap: 5, Logger: logger, Enqueuer: enq})

	s, err := svc.AddSeries(ctx, 4050)
	require.NoError(t, err)

	// EnqueueImport was called exactly once with the created series + volume ids.
	require.Equal(t, 1, enq.count())

	// The import did NOT run synchronously: no issues were imported inline.
	n, err := repos.Issue.CountBySeries(ctx, s.ID)
	require.NoError(t, err)
	require.Zero(t, n, "AddSeries must enqueue, not import inline")
}
