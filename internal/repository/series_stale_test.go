package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/repository"
)

// TestListStaleSelectsOnlyStaleActiveSeries seeds Active series that are never-refreshed,
// recently-refreshed, and old, plus a stale Paused and stale Ended series, then asserts
// ListStale returns only the stale Active ones (NULL + old), oldest first, bounded.
func TestListStaleSelectsOnlyStaleActiveSeries(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repo := repository.NewSeriesRepository(d)
	ctx := context.Background()

	now := time.Now().UTC()
	recent := now.Add(-1 * time.Hour).Format(time.RFC3339)
	old := now.Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	cutoff := now.Add(-7 * 24 * time.Hour).Format(time.RFC3339)

	mk := func(volID int64, name, status, lastRefreshed string) {
		_, err := repo.Upsert(ctx, repository.SeriesUpsert{
			ComicvineVolumeID: volID,
			Name:              name,
			Status:            status,
			LastRefreshedAt:   lastRefreshed,
			CreatedAt:         "2026-01-01T00:00:00Z",
		})
		require.NoError(t, err)
	}

	mk(1, "NeverRefreshed", "Active", "") // NULL last_refreshed_at -> stale
	mk(2, "Old", "Active", old)           // older than cutoff -> stale
	mk(3, "Recent", "Active", recent)     // recent -> NOT stale
	mk(4, "PausedOld", "Paused", old)     // stale but not Active -> excluded
	mk(5, "EndedOld", "Ended", old)       // stale but not Active -> excluded

	stale, err := repo.ListStale(ctx, cutoff, 50)
	require.NoError(t, err)

	names := make([]string, 0, len(stale))
	for _, s := range stale {
		names = append(names, s.Name)
	}
	require.ElementsMatch(t, []string{"NeverRefreshed", "Old"}, names,
		"only stale Active series are returned; recent + non-Active are excluded")
	// NULLs lead (oldest-first ordering): NeverRefreshed comes before Old.
	require.Equal(t, "NeverRefreshed", stale[0].Name)
}

// TestListStaleHonorsLimit asserts the LIMIT bound.
func TestListStaleHonorsLimit(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repo := repository.NewSeriesRepository(d)
	ctx := context.Background()

	for i := int64(1); i <= 5; i++ {
		_, err := repo.Upsert(ctx, repository.SeriesUpsert{
			ComicvineVolumeID: i,
			Name:              "S",
			Status:            "Active",
			CreatedAt:         "2026-01-01T00:00:00Z",
		})
		require.NoError(t, err)
	}

	cutoff := time.Now().UTC().Format(time.RFC3339)
	stale, err := repo.ListStale(ctx, cutoff, 3)
	require.NoError(t, err)
	require.Len(t, stale, 3)
}
