package series_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/metadata"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/series"
)

// stubGateway is a fully controllable series.Gateway for the conditional-refresh
// branch tests: it lets a test set the volume's DateLastUpdated and the cached
// source_updated_at independently, and counts ListIssues calls to prove the skip path
// does not re-import.
type stubGateway struct {
	volume          metadata.VolumeDetail
	cachedMarker    string
	hasCachedMarker bool
	issues          []metadata.IssueDetail
	listIssuesCalls atomic.Int32
	// issueDetail / issueErr control GetIssue; getIssueCalls counts it so a test can
	// prove the lazy credit fetch is a one-time, cache-gated operation.
	issueDetail   metadata.IssueDetail
	issueErr      error
	getIssueCalls atomic.Int32
}

func (g *stubGateway) SearchSeries(context.Context, string) ([]metadata.SeriesResult, error) {
	return nil, nil
}

func (g *stubGateway) GetVolume(context.Context, int64) (metadata.VolumeDetail, error) {
	return g.volume, nil
}

func (g *stubGateway) ListIssues(_ context.Context, _ int64, offset int) ([]metadata.IssueDetail, bool, error) {
	g.listIssuesCalls.Add(1)
	if offset > 0 {
		return nil, false, nil
	}
	return g.issues, false, nil
}

func (g *stubGateway) GetIssue(context.Context, int64) (metadata.IssueDetail, error) {
	g.getIssueCalls.Add(1)
	if g.issueErr != nil {
		return metadata.IssueDetail{}, g.issueErr
	}
	return g.issueDetail, nil
}

func (g *stubGateway) GetCover(context.Context, string) ([]byte, error) { return nil, nil }

func (g *stubGateway) CachedSourceUpdatedAt(context.Context, int64) (string, bool) {
	return g.cachedMarker, g.hasCachedMarker
}

// newRefreshService builds a series.Service over the stub gateway and a temp DB, and
// seeds one series row to refresh. Returns the service, the stub, the repos, and the
// seeded series id.
func newRefreshService(t *testing.T, gw *stubGateway) (*series.Service, *repository.Repositories, int64) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "refresh.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	repos := repository.NewRepositories(d)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := series.New(series.Deps{Gateway: gw, Repos: repos, AttemptCap: 5, Logger: logger})

	created, err := repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 4050,
		Name:              "Daredevil",
		Status:            "Active",
		TotalIssues:       2,
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
	})
	require.NoError(t, err)
	return svc, repos, created.ID
}

// TestRunRefreshSkipsWhenUnchanged: marker equal to cached -> no ListIssues, but
// last_refreshed_at is still bumped (D-05 cheap no-op).
func TestRunRefreshSkipsWhenUnchanged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := &stubGateway{
		volume:          metadata.VolumeDetail{ComicvineVolumeID: 4050, Name: "Daredevil", CountOfIssues: 2, DateLastUpdated: "2026-01-01 00:00:00"},
		cachedMarker:    "2026-01-01 00:00:00",
		hasCachedMarker: true,
	}
	svc, repos, id := newRefreshService(t, gw)

	require.NoError(t, svc.RunRefresh(ctx, id, 4050))

	require.Zero(t, gw.listIssuesCalls.Load(), "unchanged refresh must not call ListIssues")
	row, err := repos.Series.GetByID(ctx, id)
	require.NoError(t, err)
	require.True(t, row.LastRefreshedAt.Valid)
	require.NotEmpty(t, row.LastRefreshedAt.String, "last_refreshed_at is bumped even on a skip")
}

// TestRunRefreshReimportsWhenChanged: marker differs from cached -> import runs (issues
// land) and last_refreshed_at is bumped.
func TestRunRefreshReimportsWhenChanged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := &stubGateway{
		volume:          metadata.VolumeDetail{ComicvineVolumeID: 4050, Name: "Daredevil", CountOfIssues: 2, DateLastUpdated: "2026-02-02 00:00:00"},
		cachedMarker:    "2026-01-01 00:00:00",
		hasCachedMarker: true,
		issues: []metadata.IssueDetail{
			{ComicvineIssueID: 1, IssueNumber: "1", Title: "One"},
			{ComicvineIssueID: 2, IssueNumber: "2", Title: "Two"},
		},
	}
	svc, repos, id := newRefreshService(t, gw)

	require.NoError(t, svc.RunRefresh(ctx, id, 4050))

	require.Positive(t, gw.listIssuesCalls.Load(), "changed refresh must re-import (ListIssues called)")
	n, err := repos.Issue.CountBySeries(ctx, id)
	require.NoError(t, err)
	require.Equal(t, int64(2), n, "changed refresh imports the issues")
	row, err := repos.Series.GetByID(ctx, id)
	require.NoError(t, err)
	require.True(t, row.LastRefreshedAt.Valid)
}

// TestImportEnqueuesSearchForWantedIssues: a re-import (changed refresh) enqueues a
// one-off auto-search for each newly-Wanted issue (D-10 immediate enqueue).
func TestImportEnqueuesSearchForWantedIssues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := &stubGateway{
		volume:          metadata.VolumeDetail{ComicvineVolumeID: 4050, Name: "Daredevil", CountOfIssues: 2, DateLastUpdated: "2026-02-02 00:00:00"},
		cachedMarker:    "2026-01-01 00:00:00",
		hasCachedMarker: true,
		issues: []metadata.IssueDetail{
			{ComicvineIssueID: 1, IssueNumber: "1", Title: "One"},
			{ComicvineIssueID: 2, IssueNumber: "2", Title: "Two"},
		},
	}

	path := filepath.Join(t.TempDir(), "enqueue-search.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	repos := repository.NewRepositories(d)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	enq := &fakeEnqueuer{}
	svc := series.New(series.Deps{Gateway: gw, Repos: repos, AttemptCap: 5, Logger: logger, Enqueuer: enq})

	created, err := repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 4050, Name: "Daredevil", Status: "Active", CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	require.NoError(t, err)

	require.NoError(t, svc.RunRefresh(ctx, created.ID, 4050))

	enq.mu.Lock()
	defer enq.mu.Unlock()
	require.Len(t, enq.searches, 2, "one auto-search enqueued per newly-Wanted issue (D-10)")
}

// TestImportPersistsParityFieldsAndCredits: a changed refresh stores the new
// mylar-parity issue fields (description, image_url, cv_last_updated), derives the
// issue_type, and persists normalized creator credits via IssueCredits.Replace.
func TestImportPersistsParityFieldsAndCredits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := &stubGateway{
		volume:          metadata.VolumeDetail{ComicvineVolumeID: 4050, Name: "Daredevil", CountOfIssues: 2, DateLastUpdated: "2026-02-02 00:00:00"},
		cachedMarker:    "2026-01-01 00:00:00",
		hasCachedMarker: true,
		issues: []metadata.IssueDetail{
			{
				ComicvineIssueID: 1, IssueNumber: "1", Title: "One",
				Description: "Summary one", CoverURL: "https://comicvine.gamespot.com/x.jpg",
				CVLastUpdated: "2026-02-02 00:00:00",
				Credits: []metadata.Credit{
					{Role: "writer", Name: "Wendy Writer", ComicvinePersonID: 10},
					// "penciler" normalizes to "penciller"; comma roles already split upstream.
					{Role: "penciler", Name: "Penny Penciller", ComicvinePersonID: 11},
					{Role: "cover", Name: "Penny Penciller", ComicvinePersonID: 11},
				},
			},
			{ComicvineIssueID: 2, IssueNumber: "Annual 1", Title: "Annual One"},
		},
	}
	svc, repos, id := newRefreshService(t, gw)

	require.NoError(t, svc.RunRefresh(ctx, id, 4050))

	stored, err := repos.Issue.ListBySeries(ctx, id)
	require.NoError(t, err)
	require.Len(t, stored, 2)

	byCV := map[int64]repository.Issue{}
	for _, iss := range stored {
		byCV[iss.ComicvineIssueID] = iss
	}

	one := byCV[1]
	require.Equal(t, "Summary one", one.Description.String)
	require.Equal(t, "https://comicvine.gamespot.com/x.jpg", one.ImageUrl.String)
	require.Equal(t, "2026-02-02 00:00:00", one.CvLastUpdated.String)
	require.Equal(t, "standard", one.IssueType)

	// "Annual 1" derives the annual issue_type.
	require.Equal(t, "annual", byCV[2].IssueType)

	credits, err := repos.IssueCredits.ListByIssue(ctx, one.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []repository.IssueCredit{
		{IssueID: one.ID, Role: "writer", Name: "Wendy Writer", CVPersonID: 10},
		{IssueID: one.ID, Role: "penciller", Name: "Penny Penciller", CVPersonID: 11},
		{IssueID: one.ID, Role: "cover", Name: "Penny Penciller", CVPersonID: 11},
	}, credits)

	// Re-import is idempotent: credits are not duplicated.
	require.NoError(t, svc.RunRefresh(ctx, id, 4050))
	credits, err = repos.IssueCredits.ListByIssue(ctx, one.ID)
	require.NoError(t, err)
	require.Len(t, credits, 3)
}

// TestRunSweepEnqueuesOnlyStaleSeries: with two stale Active series seeded and a fake
// enqueuer, RunSweep enqueues exactly one refresh per stale series and zero when none
// are stale.
func TestRunSweepEnqueuesOnlyStaleSeries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sweep.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	repos := repository.NewRepositories(d)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	enq := &fakeEnqueuer{}
	// Short staleness so "recent" series are excluded and never-refreshed ones included.
	svc := series.New(series.Deps{
		Gateway: &stubGateway{}, Repos: repos, AttemptCap: 5, Logger: logger,
		Enqueuer: enq, StalenessThreshold: time.Hour,
	})

	// Two stale Active (never refreshed) + one recently refreshed + one Paused.
	mk := func(volID int64, status, lastRefreshed string) {
		_, e := repos.Series.Upsert(ctx, repository.SeriesUpsert{
			ComicvineVolumeID: volID, Name: "S", Status: status,
			LastRefreshedAt: lastRefreshed, CreatedAt: "2026-01-01T00:00:00Z",
		})
		require.NoError(t, e)
	}
	mk(1, "Active", "")
	mk(2, "Active", "")
	mk(3, "Active", time.Now().UTC().Format(time.RFC3339)) // fresh -> excluded
	mk(4, "Paused", "")                                    // not Active -> excluded

	require.NoError(t, svc.RunSweep(ctx))
	require.Equal(t, 2, enq.refreshCount(), "one refresh enqueued per stale Active series")
}

// TestRunSweepEnqueuesNothingWhenAllFresh: no stale series -> no enqueues.
func TestRunSweepEnqueuesNothingWhenAllFresh(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sweep-fresh.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	repos := repository.NewRepositories(d)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	enq := &fakeEnqueuer{}
	svc := series.New(series.Deps{
		Gateway: &stubGateway{}, Repos: repos, AttemptCap: 5, Logger: logger,
		Enqueuer: enq, StalenessThreshold: 7 * 24 * time.Hour,
	})

	_, err = repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 1, Name: "Fresh", Status: "Active",
		LastRefreshedAt: time.Now().UTC().Format(time.RFC3339), CreatedAt: "2026-01-01T00:00:00Z",
	})
	require.NoError(t, err)

	require.NoError(t, svc.RunSweep(ctx))
	require.Zero(t, enq.refreshCount(), "no stale series => no refresh enqueued")
}

// TestRefreshSeriesEnqueuesFast: RefreshSeries enqueues a refresh job (fake enqueuer)
// and returns the current series without running the refresh inline.
func TestRefreshSeriesEnqueuesFast(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := &stubGateway{volume: metadata.VolumeDetail{ComicvineVolumeID: 4050, Name: "Daredevil"}}
	svc, _, id := newRefreshService(t, gw)
	enq := &fakeEnqueuer{}
	svc.SetEnqueuer(enq)

	s, err := svc.RefreshSeries(ctx, id)
	require.NoError(t, err)
	require.Equal(t, id, s.ID)
	require.Equal(t, 1, enq.refreshCount(), "RefreshSeries enqueues exactly one refresh")
	require.Zero(t, gw.listIssuesCalls.Load(), "RefreshSeries does not import inline")
}
