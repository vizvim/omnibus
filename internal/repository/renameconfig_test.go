package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/repository"
)

func TestRenameConfigFreshDBNotPresent(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repo := repository.NewRenameConfigRepository(d)
	ctx := context.Background()

	row, err := repo.Get(ctx)
	require.NoError(t, err)
	require.False(t, row.Present, "a fresh DB has no renaming keys written")
}

func TestRenameConfigUpsertRoundTrip(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repo := repository.NewRenameConfigRepository(d)
	ctx := context.Background()

	in := repository.RenameConfigUpsert{
		FolderFormat:        "$Publisher/$Series ($Year)",
		FileFormat:          "$Series #$Issue",
		RenameEnabled:       true,
		IssuePaddingEnabled: true,
		PaddingFormat:       "00x",
		ReplaceSpaces:       false,
		Lowercase:           true,
	}
	saved, err := repo.Upsert(ctx, in)
	require.NoError(t, err)
	require.True(t, saved.Present)

	got, err := repo.Get(ctx)
	require.NoError(t, err)
	require.True(t, got.Present)
	require.Equal(t, "$Publisher/$Series ($Year)", got.FolderFormat)
	require.Equal(t, "$Series #$Issue", got.FileFormat)
	require.True(t, got.RenameEnabled)
	require.True(t, got.IssuePaddingEnabled)
	require.Equal(t, "00x", got.PaddingFormat)
	require.False(t, got.ReplaceSpaces)
	require.True(t, got.Lowercase)
}

func TestRenameConfigUpsertOverwrites(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repo := repository.NewRenameConfigRepository(d)
	ctx := context.Background()

	_, err := repo.Upsert(ctx, repository.RenameConfigUpsert{
		FolderFormat:  "$Series",
		FileFormat:    "$Series",
		RenameEnabled: true,
		PaddingFormat: "00x",
	})
	require.NoError(t, err)

	_, err = repo.Upsert(ctx, repository.RenameConfigUpsert{
		FolderFormat:  "$Publisher/$Series",
		FileFormat:    "$Series #$Issue",
		RenameEnabled: false,
		PaddingFormat: "0x",
	})
	require.NoError(t, err)

	got, err := repo.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, "$Publisher/$Series", got.FolderFormat)
	require.False(t, got.RenameEnabled)
	require.Equal(t, "0x", got.PaddingFormat)
}
