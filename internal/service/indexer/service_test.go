package indexer_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/service/indexer"
)

func newService(t *testing.T) (*indexer.Service, *repository.Repositories) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "svc.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	repos := repository.NewRepositories(d)
	return indexer.New(indexer.Deps{Repos: repos}), repos
}

func TestCreateRejectsInvalidKind(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)

	_, err := svc.Create(context.Background(), indexer.Input{
		Name:    "bad",
		Kind:    "foo",
		BaseURL: "http://example.test",
	})
	require.ErrorIs(t, err, indexer.ErrInvalidKind)
}

func TestCreateRejectsMissingFields(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)

	_, err := svc.Create(context.Background(), indexer.Input{Kind: indexer.KindNewznab, BaseURL: ""})
	require.ErrorIs(t, err, indexer.ErrMissingField)
}

func TestCreateDefaultsNewznabCategories(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)

	got, err := svc.Create(context.Background(), indexer.Input{
		Name:    "nzb",
		Kind:    indexer.KindNewznab,
		BaseURL: "http://nzb.test",
		Enabled: true,
	})
	require.NoError(t, err)
	require.Equal(t, "7030", got.Categories)
}

func TestUpdateEmptyAPIKeyRetainsStoredKey(t *testing.T) {
	t.Parallel()
	svc, repos := newService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, indexer.Input{
		Name:    "nzb",
		Kind:    indexer.KindNewznab,
		BaseURL: "http://nzb.test",
		APIKey:  "secret-key",
		Enabled: true,
	})
	require.NoError(t, err)

	// Update with an empty api_key must NOT blank the stored key.
	_, err = svc.Update(ctx, created.ID, indexer.Input{
		Name:    "nzb-renamed",
		Kind:    indexer.KindNewznab,
		BaseURL: "http://nzb.test",
		APIKey:  "",
		Enabled: true,
	})
	require.NoError(t, err)

	row, err := repos.Indexers.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "secret-key", row.APIKey, "stored api_key must be retained on empty-key update")
	require.Equal(t, "nzb-renamed", row.Name)
}

func TestUpdateReplacesAPIKeyWhenProvided(t *testing.T) {
	t.Parallel()
	svc, repos := newService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, indexer.Input{
		Name: "nzb", Kind: indexer.KindNewznab, BaseURL: "http://nzb.test", APIKey: "old", Enabled: true,
	})
	require.NoError(t, err)

	_, err = svc.Update(ctx, created.ID, indexer.Input{
		Name: "nzb", Kind: indexer.KindNewznab, BaseURL: "http://nzb.test", APIKey: "new", Enabled: true,
	})
	require.NoError(t, err)

	row, err := repos.Indexers.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "new", row.APIKey)
}

func TestCreateNormalizesSchemeLessBaseURL(t *testing.T) {
	t.Parallel()
	svc, repos := newService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, indexer.Input{
		Name:    "nzb",
		Kind:    indexer.KindNewznab,
		BaseURL: "192.168.69:5076",
		Enabled: true,
	})
	require.NoError(t, err)

	row, err := repos.Indexers.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "http://192.168.69:5076", row.BaseURL)
	require.Equal(t, "http://192.168.69:5076", created.BaseURL)
}

func TestCreatePreservesHTTPSBaseURL(t *testing.T) {
	t.Parallel()
	svc, repos := newService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, indexer.Input{
		Name:    "nzb",
		Kind:    indexer.KindNewznab,
		BaseURL: "https://nzb.test/",
		Enabled: true,
	})
	require.NoError(t, err)

	row, err := repos.Indexers.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "https://nzb.test", row.BaseURL, "https URL must not be double-prefixed; only trailing slash trimmed")
}

func TestUpdateNormalizesSchemeLessBaseURL(t *testing.T) {
	t.Parallel()
	svc, repos := newService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, indexer.Input{
		Name: "nzb", Kind: indexer.KindNewznab, BaseURL: "http://nzb.test", APIKey: "k", Enabled: true,
	})
	require.NoError(t, err)

	_, err = svc.Update(ctx, created.ID, indexer.Input{
		Name: "nzb", Kind: indexer.KindNewznab, BaseURL: "192.168.69:5076", APIKey: "k", Enabled: true,
	})
	require.NoError(t, err)

	row, err := repos.Indexers.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "http://192.168.69:5076", row.BaseURL)
}

func TestTestReturnsNotImplementedStub(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, indexer.Input{
		Name: "gc", Kind: indexer.KindGetComics, BaseURL: "http://gc.test", Enabled: true,
	})
	require.NoError(t, err)

	res, err := svc.Test(ctx, created.ID)
	require.NoError(t, err)
	require.False(t, res.OK)
	require.Equal(t, "not implemented", res.Detail)
}
