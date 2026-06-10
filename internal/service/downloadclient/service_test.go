package downloadclient_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/service/downloadclient"
)

func newService(t *testing.T) (*downloadclient.Service, *repository.Repositories) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "svc.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	repos := repository.NewRepositories(d)
	return downloadclient.New(downloadclient.Deps{Repos: repos}), repos
}

func TestUpdateRejectsEmptyURL(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)

	_, err := svc.Update(context.Background(), downloadclient.Input{URL: "  ", APIKey: "k", Category: "comics"})
	require.ErrorIs(t, err, downloadclient.ErrMissingField)
}

func TestUpdateDefaultsCategory(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)

	got, err := svc.Update(context.Background(), downloadclient.Input{
		URL:      "http://sab.test",
		APIKey:   "k",
		Category: "",
	})
	require.NoError(t, err)
	require.Equal(t, "comics", got.Category)
}

func TestUpdateEmptyAPIKeyRetainsStoredKey(t *testing.T) {
	t.Parallel()
	svc, repos := newService(t)
	ctx := context.Background()

	_, err := svc.Update(ctx, downloadclient.Input{
		URL:      "http://sab.test",
		APIKey:   "secret-key",
		Category: "comics",
	})
	require.NoError(t, err)

	// Update with an empty api_key must NOT blank the stored key.
	_, err = svc.Update(ctx, downloadclient.Input{
		URL:      "http://sab2.test",
		APIKey:   "",
		Category: "comics",
	})
	require.NoError(t, err)

	row, err := repos.DownloadClient.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, "secret-key", row.APIKey, "stored api_key must be retained on empty-key update")
	require.Equal(t, "http://sab2.test", row.URL)
}

func TestUpdateReplacesAPIKeyWhenProvided(t *testing.T) {
	t.Parallel()
	svc, repos := newService(t)
	ctx := context.Background()

	_, err := svc.Update(ctx, downloadclient.Input{URL: "http://sab.test", APIKey: "old", Category: "comics"})
	require.NoError(t, err)

	_, err = svc.Update(ctx, downloadclient.Input{URL: "http://sab.test", APIKey: "new", Category: "comics"})
	require.NoError(t, err)

	row, err := repos.DownloadClient.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, "new", row.APIKey)
}

func TestGetEmptyRowIsNotConfigured(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)

	got, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.False(t, got.Configured)
	require.Empty(t, got.URL)
}

func TestGetAfterUpdateIsConfiguredAndMasked(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)
	ctx := context.Background()

	_, err := svc.Update(ctx, downloadclient.Input{URL: "http://sab.test", APIKey: "secret", Category: "movies"})
	require.NoError(t, err)

	got, err := svc.Get(ctx)
	require.NoError(t, err)
	require.True(t, got.Configured)
	require.Equal(t, "http://sab.test", got.URL)
	require.Equal(t, "movies", got.Category)
}
