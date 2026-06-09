package repository_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository"
)

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "repo.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return d
}

const ts = "2026-01-01T00:00:00Z"

func TestUpsertSeriesIdempotentOnVolumeID(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repo := repository.NewSeriesRepository(d)
	ctx := context.Background()

	first, err := repo.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 4050,
		Name:              "Saga",
		CreatedAt:         ts,
		Status:            "Active",
	})
	require.NoError(t, err)

	// Same volume_id updates in place (no duplicate row) — META-06 idempotency.
	second, err := repo.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 4050,
		Name:              "Saga (renamed)",
		CreatedAt:         ts,
		Status:            "Active",
	})
	require.NoError(t, err)

	require.Equal(t, first.ID, second.ID)
	require.Equal(t, "Saga (renamed)", second.Name)

	all, err := repo.List(ctx, 50, 0)
	require.NoError(t, err)
	require.Len(t, all, 1)
}

func TestUpsertIssueDistinctByQualifier(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	seriesRepo := repository.NewSeriesRepository(d)
	issueRepo := repository.NewIssueRepository(d)
	ctx := context.Background()

	s, err := seriesRepo.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 4050, Name: "S", CreatedAt: ts, Status: "Active",
	})
	require.NoError(t, err)

	_, err = issueRepo.Upsert(ctx, repository.IssueUpsert{
		SeriesID: s.ID, ComicvineIssueID: 1, IssueNumberRaw: "7",
		IssueNumberSort: 7.0, Status: "Wanted", CreatedAt: ts,
	})
	require.NoError(t, err)

	_, err = issueRepo.Upsert(ctx, repository.IssueUpsert{
		SeriesID: s.ID, ComicvineIssueID: 2, IssueNumberRaw: "7.INH",
		IssueNumberSort: 7.0, IssueNumberQual: "INH", Status: "Wanted", CreatedAt: ts,
	})
	require.NoError(t, err)

	issues, err := issueRepo.ListBySeries(ctx, s.ID)
	require.NoError(t, err)
	require.Len(t, issues, 2, "7 and 7.INH are distinct rows")
}

func TestUpsertIssueIdempotentOnCVID(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	seriesRepo := repository.NewSeriesRepository(d)
	issueRepo := repository.NewIssueRepository(d)
	ctx := context.Background()

	s, err := seriesRepo.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 4050, Name: "S", CreatedAt: ts, Status: "Active",
	})
	require.NoError(t, err)

	_, err = issueRepo.Upsert(ctx, repository.IssueUpsert{
		SeriesID: s.ID, ComicvineIssueID: 99, IssueNumberRaw: "1",
		IssueNumberSort: 1.0, Title: "One", Status: "Wanted", CreatedAt: ts,
	})
	require.NoError(t, err)
	_, err = issueRepo.Upsert(ctx, repository.IssueUpsert{
		SeriesID: s.ID, ComicvineIssueID: 99, IssueNumberRaw: "1",
		IssueNumberSort: 1.0, Title: "One (updated)", Status: "Wanted", CreatedAt: ts,
	})
	require.NoError(t, err)

	issues, err := issueRepo.ListBySeries(ctx, s.ID)
	require.NoError(t, err)
	require.Len(t, issues, 1)
	require.Equal(t, "One (updated)", issues[0].Title.String)
}

func TestUpsertPublisherAndArcIdempotent(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	pubRepo := repository.NewPublisherRepository(d)
	arcRepo := repository.NewArcRepository(d)
	ctx := context.Background()

	p1, err := pubRepo.Upsert(ctx, repository.PublisherUpsert{Name: "Image", CreatedAt: ts})
	require.NoError(t, err)
	p2, err := pubRepo.Upsert(ctx, repository.PublisherUpsert{Name: "Image", CreatedAt: ts})
	require.NoError(t, err)
	require.Equal(t, p1.ID, p2.ID)

	a1, err := arcRepo.Upsert(ctx, repository.ArcUpsert{ComicvineArcID: 555, Name: "Arc", CreatedAt: ts})
	require.NoError(t, err)
	a2, err := arcRepo.Upsert(ctx, repository.ArcUpsert{ComicvineArcID: 555, Name: "Arc", CreatedAt: ts})
	require.NoError(t, err)
	require.Equal(t, a1.ID, a2.ID)
}

func TestUserConfigRoundTrip(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repo := repository.NewUserConfigRepository(d)
	ctx := context.Background()

	require.NoError(t, repo.Set(ctx, "auth.enabled", "true"))
	v, err := repo.Get(ctx, "auth.enabled")
	require.NoError(t, err)
	require.Equal(t, "true", v)

	// Overwrite round-trips.
	require.NoError(t, repo.Set(ctx, "auth.enabled", "false"))
	v, err = repo.Get(ctx, "auth.enabled")
	require.NoError(t, err)
	require.Equal(t, "false", v)

	_, err = repo.Get(ctx, "missing")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestMetadataCacheRoundTrip(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repo := repository.NewMetadataCacheRepository(d)
	ctx := context.Background()

	require.NoError(t, repo.Put(ctx, repository.MetadataCacheEntry{
		CacheKey: "volume:4050-1", Payload: `{"x":1}`, FetchedAt: ts,
	}))
	got, err := repo.Get(ctx, "volume:4050-1")
	require.NoError(t, err)
	require.Equal(t, `{"x":1}`, got.Payload)
}
