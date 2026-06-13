package renameconfig_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/renameconfig"
	"github.com/vizvim/omnibus/internal/repository"
)

func newService(t *testing.T) (*renameconfig.Service, *repository.Repositories) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "svc.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	repos := repository.NewRepositories(d)
	return renameconfig.New(renameconfig.Deps{Repos: repos}), repos
}

func TestGetFreshDBReturnsMylarDefaults(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)

	got, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.Equal(t, "$Publisher/$Series ($Year)", got.FolderFormat)
	require.Equal(t, "$Series $VolumeY $Annual #$Issue ($monthname $Year)", got.FileFormat)
	require.True(t, got.RenameEnabled)
	require.True(t, got.IssuePaddingEnabled)
	require.Equal(t, "00x", got.PaddingFormat)
	require.False(t, got.ReplaceSpaces)
	require.False(t, got.Lowercase)
}

func TestDefaultRenameConfigMatchesGetDefaults(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)

	got, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.Equal(t, renameconfig.DefaultRenameConfig(), got)
}

func TestUpdatePersistsAndRoundTrips(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)
	ctx := context.Background()

	in := renameconfig.Input{
		FolderFormat:        "$Series/$Year",
		FileFormat:          "$Series #$Issue",
		RenameEnabled:       true,
		IssuePaddingEnabled: false,
		PaddingFormat:       "0x",
		ReplaceSpaces:       true,
		Lowercase:           true,
	}
	saved, err := svc.Update(ctx, in)
	require.NoError(t, err)
	require.Equal(t, "$Series/$Year", saved.FolderFormat)
	require.True(t, saved.ReplaceSpaces)

	// A subsequent Get returns the saved values (runtime persistence, no restart).
	got, err := svc.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, "$Series/$Year", got.FolderFormat)
	require.Equal(t, "$Series #$Issue", got.FileFormat)
	require.True(t, got.RenameEnabled)
	require.False(t, got.IssuePaddingEnabled)
	require.Equal(t, "0x", got.PaddingFormat)
	require.True(t, got.ReplaceSpaces)
	require.True(t, got.Lowercase)
}

func TestUpdateRejectsEmptyFormatsWhenRenameEnabled(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)

	_, err := svc.Update(context.Background(), renameconfig.Input{
		FolderFormat:  "  ",
		FileFormat:    "$Series",
		RenameEnabled: true,
		PaddingFormat: "00x",
	})
	require.ErrorIs(t, err, renameconfig.ErrMissingField)

	_, err = svc.Update(context.Background(), renameconfig.Input{
		FolderFormat:  "$Series",
		FileFormat:    "",
		RenameEnabled: true,
		PaddingFormat: "00x",
	})
	require.ErrorIs(t, err, renameconfig.ErrMissingField)
}

func TestUpdateAllowsEmptyFormatsWhenRenameDisabled(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)

	got, err := svc.Update(context.Background(), renameconfig.Input{
		FolderFormat:  "",
		FileFormat:    "",
		RenameEnabled: false,
		PaddingFormat: "00x",
	})
	require.NoError(t, err, "empty formats are allowed when rename is off")
	require.False(t, got.RenameEnabled)
}

func TestUpdateDefaultsUnknownPaddingFormat(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)

	got, err := svc.Update(context.Background(), renameconfig.Input{
		FolderFormat:  "$Series",
		FileFormat:    "$Series #$Issue",
		RenameEnabled: true,
		PaddingFormat: "garbage",
	})
	require.NoError(t, err)
	require.Equal(t, "00x", got.PaddingFormat, "an unknown padding format falls back to the 00x default")
}
