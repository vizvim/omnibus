package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/repository"
)

// seedIssueForCredits inserts a series + issue and returns the issue id for credit FKs.
func seedIssueForCredits(t *testing.T, repos *repository.Repositories) int64 {
	t.Helper()
	ctx := context.Background()
	seriesID := seedSeriesForIssues(t, repos)
	iss, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: seriesID, ComicvineIssueID: 99, IssueNumberRaw: "1",
		IssueNumberSort: 1, Status: "Wanted", CreatedAt: ts,
	})
	require.NoError(t, err)
	return iss.ID
}

func TestIssueCreditsReplaceAndList(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repos := repository.NewRepositories(d)
	ctx := context.Background()
	issueID := seedIssueForCredits(t, repos)

	// Empty list before any Replace.
	got, err := repos.IssueCredits.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Empty(t, got)

	// Replace inserts the supplied set, ordered by (role, name).
	err = repos.IssueCredits.Replace(ctx, issueID, []repository.IssueCredit{
		{IssueID: issueID, Role: "writer", Name: "Brian K. Vaughan", CVPersonID: 1},
		{IssueID: issueID, Role: "penciller", Name: "Fiona Staples", CVPersonID: 2},
		{IssueID: issueID, Role: "cover", Name: "Fiona Staples", CVPersonID: 2},
	})
	require.NoError(t, err)

	got, err = repos.IssueCredits.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, "cover", got[0].Role)
	require.Equal(t, "penciller", got[1].Role)
	require.Equal(t, "writer", got[2].Role)
	require.Equal(t, int64(1), got[2].CVPersonID)
}

func TestIssueCreditsReplaceIsIdempotent(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repos := repository.NewRepositories(d)
	ctx := context.Background()
	issueID := seedIssueForCredits(t, repos)

	set := []repository.IssueCredit{
		{IssueID: issueID, Role: "writer", Name: "A", CVPersonID: 1},
		{IssueID: issueID, Role: "inker", Name: "B", CVPersonID: 2},
	}
	require.NoError(t, repos.IssueCredits.Replace(ctx, issueID, set))
	require.NoError(t, repos.IssueCredits.Replace(ctx, issueID, set))

	got, err := repos.IssueCredits.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, got, 2, "re-running Replace with the same set must not duplicate rows")

	// A subsequent Replace fully swaps the set (delete-all then insert).
	require.NoError(t, repos.IssueCredits.Replace(ctx, issueID, []repository.IssueCredit{
		{IssueID: issueID, Role: "editor", Name: "C", CVPersonID: 3},
	}))
	got, err = repos.IssueCredits.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "editor", got[0].Role)

	// Replace with an empty set clears all credits.
	require.NoError(t, repos.IssueCredits.Replace(ctx, issueID, nil))
	got, err = repos.IssueCredits.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Empty(t, got)
}

// TestIssueUpsertParityFields verifies the new mylar-parity columns round-trip,
// including the empty-string -> NULL / "" -> "standard" / 0 -> NULL defaulting.
func TestIssueUpsertParityFields(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repos := repository.NewRepositories(d)
	ctx := context.Background()
	seriesID := seedSeriesForIssues(t, repos)

	// Full field set.
	full, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: seriesID, ComicvineIssueID: 1, IssueNumberRaw: "1", IssueNumberSort: 1,
		Description: "A summary", ImageURL: "https://x/y.jpg", CVLastUpdated: "2026-01-01",
		IssueType: "annual", AltIssueNumber: "1A", PageCount: 32,
		Status: "Wanted", CreatedAt: ts,
	})
	require.NoError(t, err)
	require.Equal(t, "A summary", full.Description.String)
	require.Equal(t, "https://x/y.jpg", full.ImageUrl.String)
	require.Equal(t, "2026-01-01", full.CvLastUpdated.String)
	require.Equal(t, "annual", full.IssueType)
	require.Equal(t, "1A", full.AltIssueNumber.String)
	require.True(t, full.PageCount.Valid)
	require.Equal(t, int64(32), full.PageCount.Int64)

	// Defaulting: empty type -> "standard"; empty strings -> NULL; 0 page_count -> NULL.
	defd, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: seriesID, ComicvineIssueID: 2, IssueNumberRaw: "2", IssueNumberSort: 2,
		Status: "Wanted", CreatedAt: ts,
	})
	require.NoError(t, err)
	require.Equal(t, "standard", defd.IssueType)
	require.False(t, defd.Description.Valid)
	require.False(t, defd.ImageUrl.Valid)
	require.False(t, defd.CvLastUpdated.Valid)
	require.False(t, defd.AltIssueNumber.Valid)
	require.False(t, defd.PageCount.Valid)
}
