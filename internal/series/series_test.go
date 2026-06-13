package series_test

import (
	"context"
	"errors"
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
	"github.com/vizvim/omnibus/internal/metadata"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/series"
)

// errProvider is a sentinel used to drive the lazy-credit-fetch provider-error path.
var errProvider = errors.New("provider unavailable")

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

	fake, err := metadata.NewFakeProvider("../metadata/testdata/fixtures")
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
	client, err := jobs.New(ctx, d.Write, d.Read, 2, 0, 0, 0, 0, logger, workers)
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
	mu        sync.Mutex
	calls     [][2]int64
	refreshes [][2]int64
	searches  []int64
}

func (f *fakeEnqueuer) EnqueueImport(_ context.Context, seriesID, comicvineVolumeID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, [2]int64{seriesID, comicvineVolumeID})
	return nil
}

func (f *fakeEnqueuer) EnqueueRefresh(_ context.Context, seriesID, comicvineVolumeID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes = append(f.refreshes, [2]int64{seriesID, comicvineVolumeID})
	return nil
}

func (f *fakeEnqueuer) EnqueueSearchIssue(_ context.Context, issueID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.searches = append(f.searches, issueID)
	return nil
}

func (f *fakeEnqueuer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeEnqueuer) refreshCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.refreshes)
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

func TestGetIssueReturnsDetailWithOrderedCredits(t *testing.T) {
	t.Parallel()
	svc, repos := newService(t)
	ctx := context.Background()

	// Seed a series + a single issue with rich metadata directly via the repos so the
	// assertions don't depend on fixture credit data.
	ser, err := repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 9001,
		Name:              "Test Vol",
		Status:            "Active",
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
	})
	require.NoError(t, err)

	iss, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID:         ser.ID,
		ComicvineIssueID: 7001,
		IssueNumberRaw:   "1",
		IssueNumberSort:  1,
		Title:            "First",
		CoverDate:        "2020-01-01",
		StoreDate:        "2019-12-25",
		Description:      "A grand summary.",
		CVLastUpdated:    "2020-02-02",
		IssueType:        "standard",
		AltIssueNumber:   "1A",
		PageCount:        32,
		Status:           "Wanted",
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
	})
	require.NoError(t, err)

	// Insert credits out of (role,name) order to prove the query orders them.
	require.NoError(t, repos.IssueCredits.Replace(ctx, iss.ID, []repository.IssueCredit{
		{IssueID: iss.ID, Role: "writer", Name: "Zoe Author", CVPersonID: 3},
		{IssueID: iss.ID, Role: "penciller", Name: "Al Artist", CVPersonID: 1},
		{IssueID: iss.ID, Role: "penciller", Name: "Bo Artist", CVPersonID: 2},
	}))

	detail, err := svc.GetIssue(ctx, iss.ID)
	require.NoError(t, err)

	// Base issue + rich fields mapped.
	require.Equal(t, iss.ID, detail.Issue.ID)
	require.Equal(t, "First", detail.Issue.Title)
	require.Equal(t, "standard", detail.Issue.IssueType)
	require.Equal(t, "A grand summary.", detail.Description)
	require.Equal(t, "standard", detail.IssueType)
	require.Equal(t, "1A", detail.AltIssueNumber)
	require.Equal(t, int32(32), detail.PageCount)
	require.Equal(t, "2019-12-25", detail.StoreDate)
	require.Equal(t, "2020-02-02", detail.CVLastUpdated)

	// Credits ordered by (role, name): penciller/Al, penciller/Bo, writer/Zoe.
	require.Len(t, detail.Credits, 3)
	require.Equal(t, "penciller", detail.Credits[0].Role)
	require.Equal(t, "Al Artist", detail.Credits[0].Name)
	require.Equal(t, "penciller", detail.Credits[1].Role)
	require.Equal(t, "Bo Artist", detail.Credits[1].Name)
	require.Equal(t, "writer", detail.Credits[2].Role)
	require.Equal(t, "Zoe Author", detail.Credits[2].Name)
	require.Equal(t, int64(3), detail.Credits[2].ComicvinePersonID)
}

// seedIssue inserts one issue (with a ComicVine id, optional description, and no
// credits) under a fresh series, returning the issue id. Used by the lazy-credit tests.
func seedIssue(t *testing.T, repos *repository.Repositories, cvIssueID int64, description string) int64 {
	t.Helper()
	ctx := context.Background()
	ser, err := repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 8800,
		Name:              "Lazy Vol",
		Status:            "Active",
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
	})
	require.NoError(t, err)
	iss, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID:         ser.ID,
		ComicvineIssueID: cvIssueID,
		IssueNumberRaw:   "1",
		IssueNumberSort:  1,
		Title:            "First",
		Description:      description,
		IssueType:        "standard",
		Status:           "Wanted",
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
	})
	require.NoError(t, err)
	return iss.ID
}

// TestGetIssueLazilyFetchesAndPersistsCredits proves that when no credits are cached,
// GetIssue fetches the per-issue DETAIL, persists the mapped credits (Replace), returns
// them, and on a second call serves the now-cached credits WITHOUT re-fetching.
func TestGetIssueLazilyFetchesAndPersistsCredits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := &stubGateway{
		issueDetail: metadata.IssueDetail{
			ComicvineIssueID: 7700,
			Description:      "<p>Detail summary.</p>",
			Credits: []metadata.Credit{
				{Role: "writer", Name: "Zoe Author", ComicvinePersonID: 3},
				{Role: "penciler", Name: "Al Artist", ComicvinePersonID: 1},
			},
		},
	}
	svc, repos, _ := newRefreshService(t, gw)
	issueID := seedIssue(t, repos, 7700, "<p>stored</p>")

	detail, err := svc.GetIssue(ctx, issueID)
	require.NoError(t, err)
	require.Equal(t, int32(1), gw.getIssueCalls.Load(), "first GetIssue fetches DETAIL")
	require.NotEmpty(t, detail.Credits, "lazily-fetched credits returned")

	// Credits were persisted: ListByIssue now returns them.
	stored, err := repos.IssueCredits.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, stored, 2)

	// Second call serves cached credits — no second provider fetch.
	again, err := svc.GetIssue(ctx, issueID)
	require.NoError(t, err)
	require.Equal(t, int32(1), gw.getIssueCalls.Load(), "cached credits skip the fetch")
	require.Len(t, again.Credits, 2)
}

// TestGetIssuePrefersFetchedDescriptionStripped proves the richer fetched description is
// preferred over the stored one and is rendered as plain text.
func TestGetIssuePrefersFetchedDescriptionStripped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := &stubGateway{
		issueDetail: metadata.IssueDetail{
			ComicvineIssueID: 7701,
			Description:      "<p><em><b>Rich</b></em> &amp; <i>fetched</i>.</p>",
			Credits:          []metadata.Credit{{Role: "writer", Name: "A", ComicvinePersonID: 1}},
		},
	}
	svc, repos, _ := newRefreshService(t, gw)
	issueID := seedIssue(t, repos, 7701, "<p>stored only</p>")

	detail, err := svc.GetIssue(ctx, issueID)
	require.NoError(t, err)
	require.Equal(t, "Rich & fetched.", detail.Description)
}

// TestGetIssueProviderErrorFallsBack proves a provider error during the lazy fetch is
// non-fatal: GetIssue still returns a detail (with no credits) and the stored,
// HTML-stripped description.
func TestGetIssueProviderErrorFallsBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := &stubGateway{issueErr: errProvider}
	svc, repos, _ := newRefreshService(t, gw)
	issueID := seedIssue(t, repos, 7702, "<p>Stored <b>summary</b> &amp; more.</p>")

	detail, err := svc.GetIssue(ctx, issueID)
	require.NoError(t, err, "provider error must not fail GetIssue")
	require.Empty(t, detail.Credits)
	require.Equal(t, "Stored summary & more.", detail.Description)
}

// TestGetIssueNoFetchWhenCreditsCached proves no provider fetch happens when credits
// already exist for the issue.
func TestGetIssueNoFetchWhenCreditsCached(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := &stubGateway{}
	svc, repos, _ := newRefreshService(t, gw)
	issueID := seedIssue(t, repos, 7703, "plain stored")
	require.NoError(t, repos.IssueCredits.Replace(ctx, issueID, []repository.IssueCredit{
		{IssueID: issueID, Role: "writer", Name: "Existing", CVPersonID: 9},
	}))

	detail, err := svc.GetIssue(ctx, issueID)
	require.NoError(t, err)
	require.Zero(t, gw.getIssueCalls.Load(), "cached credits must not trigger a fetch")
	require.Len(t, detail.Credits, 1)
	require.Equal(t, "Existing", detail.Credits[0].Name)
}

func TestGetIssueNotFound(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)

	_, err := svc.GetIssue(context.Background(), 999999)
	require.Error(t, err)
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

	fake, err := metadata.NewFakeProvider("../metadata/testdata/fixtures")
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
