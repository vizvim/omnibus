package series_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/metadata"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/series"
)

// newServiceWithFakeEnqueuer builds a Service backed by a temp SQLite DB and the fixture
// fake gateway, wired to a recording fakeEnqueuer rather than a real River client. This
// lets the ListSeries / UpdateSeriesSettings / RefreshSeries paths be exercised
// synchronously (no durable import job runs), and returns the repos for direct seeding.
func newServiceWithFakeEnqueuer(t *testing.T) (*series.Service, *repository.Repositories, *fakeEnqueuer) {
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
	enq := &fakeEnqueuer{}
	svc := series.New(series.Deps{Gateway: gw, Repos: repos, AttemptCap: 5, Logger: logger, Enqueuer: enq})
	return svc, repos, enq
}

// seedSeries upserts a minimal Active series row and returns it.
func seedSeries(t *testing.T, repos *repository.Repositories, volumeID int64, name string) repository.Series {
	t.Helper()
	ser, err := repos.Series.Upsert(context.Background(), repository.SeriesUpsert{
		ComicvineVolumeID: volumeID,
		Name:              name,
		Status:            "Active",
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
	})
	require.NoError(t, err)
	return ser
}

func TestListSeriesPaging(t *testing.T) {
	t.Parallel()
	svc, repos, _ := newServiceWithFakeEnqueuer(t)
	ctx := context.Background()

	// Empty DB → empty (non-nil) slice.
	rows, err := svc.ListSeries(ctx, 0)
	require.NoError(t, err)
	require.Empty(t, rows)

	seedSeries(t, repos, 1001, "Alpha")
	seedSeries(t, repos, 1002, "Beta")

	rows, err = svc.ListSeries(ctx, 0)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	// A negative page is clamped to 0 (covers the page<0 branch) and still returns rows.
	rows, err = svc.ListSeries(ctx, -5)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	// A page far past the data returns an empty slice (offset beyond the rows).
	rows, err = svc.ListSeries(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestUpdateSeriesSettings(t *testing.T) {
	t.Parallel()
	svc, repos, _ := newServiceWithFakeEnqueuer(t)
	ctx := context.Background()
	ser := seedSeries(t, repos, 2001, "Gamma")

	cases := []struct {
		name    string
		status  string
		wantErr bool
	}{
		{"active", "Active", false},
		{"paused", "Paused", false},
		{"ended", "Ended", false},
		{"invalid", "Bogus", true},
		{"empty", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := svc.UpdateSeriesSettings(ctx, ser.ID, tc.status)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.status, got.Status)
			require.Equal(t, ser.ID, got.ID)
		})
	}
}

func TestRefreshSeriesEnqueuesAndReturnsCurrent(t *testing.T) {
	t.Parallel()
	svc, repos, enq := newServiceWithFakeEnqueuer(t)
	ctx := context.Background()

	// Seed a series with a publisher so the publisher-resolution branch in RefreshSeries
	// is exercised.
	pub, err := repos.Publisher.Upsert(ctx, repository.PublisherUpsert{
		ComicvinePublisherID: nil,
		Name:                 "Image",
		CreatedAt:            time.Now().UTC().Format(time.RFC3339),
	})
	require.NoError(t, err)
	ser, err := repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 3001,
		PublisherID:       &pub.ID,
		Name:              "Saga",
		Status:            "Active",
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
	})
	require.NoError(t, err)

	got, err := svc.RefreshSeries(ctx, ser.ID)
	require.NoError(t, err)
	require.Equal(t, ser.ID, got.ID)
	require.Equal(t, "Image", got.Publisher)
	require.Equal(t, 1, enq.refreshCount())
}

func TestRefreshSeriesNotFound(t *testing.T) {
	t.Parallel()
	svc, _, enq := newServiceWithFakeEnqueuer(t)

	_, err := svc.RefreshSeries(context.Background(), 999999)
	require.Error(t, err)
	require.Zero(t, enq.refreshCount())
}

// TestGetSeriesWithArc exercises arcFromRow + the arc-listing branch of GetSeries by
// seeding a series with an issue, an arc, and a link between them.
func TestGetSeriesWithArc(t *testing.T) {
	t.Parallel()
	svc, repos, _ := newServiceWithFakeEnqueuer(t)
	ctx := context.Background()
	ts := time.Now().UTC().Format(time.RFC3339)

	ser := seedSeries(t, repos, 4001, "Arc Series")
	iss, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID:         ser.ID,
		ComicvineIssueID: 8001,
		IssueNumberRaw:   "1",
		IssueNumberSort:  1,
		Status:           "Wanted",
		CreatedAt:        ts,
	})
	require.NoError(t, err)
	arc, err := repos.Arc.Upsert(ctx, repository.ArcUpsert{ComicvineArcID: 777, Name: "The Big Arc", CreatedAt: ts})
	require.NoError(t, err)
	require.NoError(t, repos.Arc.LinkIssue(ctx, arc.ID, iss.ID, 0))

	view, err := svc.GetSeries(ctx, ser.ID)
	require.NoError(t, err)
	require.Len(t, view.StoryArcs, 1)
	require.Equal(t, "The Big Arc", view.StoryArcs[0].Name)
	require.Equal(t, int64(777), view.StoryArcs[0].ComicvineArcID)
	require.Equal(t, arc.ID, view.StoryArcs[0].ID)
}

func TestGetSeriesNotFound(t *testing.T) {
	t.Parallel()
	svc, _, _ := newServiceWithFakeEnqueuer(t)

	_, err := svc.GetSeries(context.Background(), 424242)
	require.Error(t, err)
}
