package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/repository"
)

// seedSeriesForIssues inserts a series and returns its id, so issue rows have a valid FK.
func seedSeriesForIssues(t *testing.T, repos *repository.Repositories) int64 {
	t.Helper()
	s, err := repos.Series.Upsert(context.Background(), repository.SeriesUpsert{
		ComicvineVolumeID: 4050, Name: "Saga", Status: "Active", CreatedAt: ts,
	})
	require.NoError(t, err)
	return s.ID
}

func TestListWantedForAutoSearchOrdersAndCaps(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repos := repository.NewRepositories(d)
	ctx := context.Background()
	seriesID := seedSeriesForIssues(t, repos)

	// Three Wanted issues with increasing search_attempts; one cold (at the cap).
	mk := func(cvID int64, raw string, sort float64, created string, attempts int32) int64 {
		iss, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
			SeriesID: seriesID, ComicvineIssueID: cvID, IssueNumberRaw: raw,
			IssueNumberSort: sort, Status: "Wanted", CreatedAt: created,
		})
		require.NoError(t, err)
		if attempts > 0 {
			require.NoError(t, repos.Issue.UpdateStatus(ctx, iss.ID, "Wanted", attempts))
		}
		return iss.ID
	}

	fewest := mk(1, "1", 1, "2026-01-01T00:00:00Z", 0)
	mid := mk(2, "2", 2, "2026-01-02T00:00:00Z", 1)
	mk(3, "3", 3, "2026-01-03T00:00:00Z", 5) // cold: at the cap, excluded
	// A non-Wanted issue is excluded.
	snatched, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: seriesID, ComicvineIssueID: 4, IssueNumberRaw: "4", IssueNumberSort: 4,
		Status: "Snatched", CreatedAt: "2026-01-04T00:00:00Z",
	})
	require.NoError(t, err)

	batch, err := repos.Issue.ListWantedForAutoSearch(ctx, 5, 10)
	require.NoError(t, err)

	// Cold issue (attempts==cap) and the Snatched issue are excluded.
	require.Len(t, batch, 2)
	require.Equal(t, fewest, batch[0].ID, "fewest-attempts first")
	require.Equal(t, mid, batch[1].ID)
	for _, b := range batch {
		require.NotEqual(t, snatched.ID, b.ID)
		require.Less(t, b.SearchAttempts, int64(5))
	}

	// The batch is capped.
	limited, err := repos.Issue.ListWantedForAutoSearch(ctx, 5, 1)
	require.NoError(t, err)
	require.Len(t, limited, 1)
	require.Equal(t, fewest, limited[0].ID)
}

func TestIncrementSearchAttempts(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repos := repository.NewRepositories(d)
	ctx := context.Background()
	seriesID := seedSeriesForIssues(t, repos)

	iss, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: seriesID, ComicvineIssueID: 1, IssueNumberRaw: "1", IssueNumberSort: 1,
		Status: "Wanted", CreatedAt: ts,
	})
	require.NoError(t, err)

	require.NoError(t, repos.Issue.IncrementSearchAttempts(ctx, iss.ID))
	require.NoError(t, repos.Issue.IncrementSearchAttempts(ctx, iss.ID))

	got, err := repos.Issue.GetByID(ctx, iss.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), got.SearchAttempts)
}

func TestListWantedForMatch(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repos := repository.NewRepositories(d)
	ctx := context.Background()
	seriesID := seedSeriesForIssues(t, repos)

	_, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: seriesID, ComicvineIssueID: 1, IssueNumberRaw: "1", IssueNumberSort: 1,
		Status: "Wanted", CreatedAt: ts,
	})
	require.NoError(t, err)
	_, err = repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: seriesID, ComicvineIssueID: 2, IssueNumberRaw: "2", IssueNumberSort: 2,
		Status: "Snatched", CreatedAt: ts,
	})
	require.NoError(t, err)

	wanted, err := repos.Issue.ListWantedForMatch(ctx)
	require.NoError(t, err)
	require.Len(t, wanted, 1)
	require.Equal(t, "Wanted", wanted[0].Status)
}
