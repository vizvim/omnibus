package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/repository"
)

const blTS = "2026-01-01T00:00:00Z"

func TestBlacklistAddAndListRoundTrip(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	ctx := context.Background()
	repos := repository.NewRepositories(d)

	s, err := repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 4050, Name: "Saga", Status: "Active", CreatedAt: blTS,
	})
	require.NoError(t, err)
	iss, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: s.ID, ComicvineIssueID: 9001, IssueNumberRaw: "7", IssueNumberSort: 7.0,
		Status: "Wanted", CreatedAt: blTS,
	})
	require.NoError(t, err)

	require.NoError(t, repos.Blacklist.Add(ctx, repository.BlacklistAdd{
		IssueID: iss.ID, Provider: "newznab", ReleaseKey: "rk-bad", Reason: "corrupt",
	}, blTS))

	keys, err := repos.Blacklist.ListForIssue(ctx, iss.ID)
	require.NoError(t, err)
	require.Len(t, keys, 1)
	require.Equal(t, repository.BlacklistKey{Provider: "newznab", ReleaseKey: "rk-bad"}, keys[0])
}

func TestBlacklistAddDuplicateIsNoOp(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	ctx := context.Background()
	repos := repository.NewRepositories(d)

	s, err := repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 4050, Name: "Saga", Status: "Active", CreatedAt: blTS,
	})
	require.NoError(t, err)
	iss, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: s.ID, ComicvineIssueID: 9001, IssueNumberRaw: "7", IssueNumberSort: 7.0,
		Status: "Wanted", CreatedAt: blTS,
	})
	require.NoError(t, err)

	add := repository.BlacklistAdd{IssueID: iss.ID, Provider: "newznab", ReleaseKey: "rk-bad"}
	require.NoError(t, repos.Blacklist.Add(ctx, add, blTS))
	// Duplicate insert of the same release for the same issue must not error or duplicate.
	require.NoError(t, repos.Blacklist.Add(ctx, add, blTS))

	keys, err := repos.Blacklist.ListForIssue(ctx, iss.ID)
	require.NoError(t, err)
	require.Len(t, keys, 1, "a duplicate blacklist is a no-op (UNIQUE constraint)")
}

func TestBlacklistIsPerIssueScoped(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	ctx := context.Background()
	repos := repository.NewRepositories(d)

	s, err := repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 4050, Name: "Saga", Status: "Active", CreatedAt: blTS,
	})
	require.NoError(t, err)
	issA, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: s.ID, ComicvineIssueID: 9001, IssueNumberRaw: "7", IssueNumberSort: 7.0,
		Status: "Wanted", CreatedAt: blTS,
	})
	require.NoError(t, err)
	issB, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: s.ID, ComicvineIssueID: 9002, IssueNumberRaw: "8", IssueNumberSort: 8.0,
		Status: "Wanted", CreatedAt: blTS,
	})
	require.NoError(t, err)

	// Blacklist a release for issue A only.
	require.NoError(t, repos.Blacklist.Add(ctx, repository.BlacklistAdd{
		IssueID: issA.ID, Provider: "newznab", ReleaseKey: "rk-bad",
	}, blTS))

	aKeys, err := repos.Blacklist.ListForIssue(ctx, issA.ID)
	require.NoError(t, err)
	require.Len(t, aKeys, 1, "issue A sees its blacklist")

	bKeys, err := repos.Blacklist.ListForIssue(ctx, issB.ID)
	require.NoError(t, err)
	require.Empty(t, bKeys, "issue B does not see issue A's blacklist (per-issue scope, D-11)")
}
