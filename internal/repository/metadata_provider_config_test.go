package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/repository"
)

func TestMetadataProviderConfigFreshDBNotSet(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repo := repository.NewMetadataProviderConfigRepository(d)
	ctx := context.Background()

	_, err := repo.Get(ctx, "comicvine")
	require.ErrorIs(t, err, repository.ErrNoMetadataProviderConfig)
}

func TestMetadataProviderConfigUpsertRoundTrip(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repo := repository.NewMetadataProviderConfigRepository(d)
	ctx := context.Background()

	saved, err := repo.Upsert(ctx, repository.MetadataProviderConfigUpsert{
		Provider: "comicvine",
		APIKey:   "secret",
	}, ts)
	require.NoError(t, err)
	require.Equal(t, "comicvine", saved.Provider)
	require.Equal(t, "secret", saved.APIKey)

	got, err := repo.Get(ctx, "comicvine")
	require.NoError(t, err)
	require.Equal(t, "secret", got.APIKey)
}

func TestMetadataProviderConfigUpsertPreservesCreatedAt(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repo := repository.NewMetadataProviderConfigRepository(d)
	ctx := context.Background()

	_, err := repo.Upsert(ctx, repository.MetadataProviderConfigUpsert{Provider: "comicvine", APIKey: "k1"}, ts)
	require.NoError(t, err)

	const later = "2027-02-02T00:00:00Z"
	updated, err := repo.Upsert(ctx, repository.MetadataProviderConfigUpsert{Provider: "comicvine", APIKey: "k2"}, later)
	require.NoError(t, err)
	require.Equal(t, "k2", updated.APIKey)
	require.Equal(t, ts, updated.CreatedAt, "created_at is preserved across updates")
	require.Equal(t, later, updated.UpdatedAt, "updated_at advances on update")
}

func TestMetadataProviderConfigEmptyKeyStoredAsEmpty(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repo := repository.NewMetadataProviderConfigRepository(d)
	ctx := context.Background()

	row, err := repo.Upsert(ctx, repository.MetadataProviderConfigUpsert{Provider: "comicvine", APIKey: ""}, ts)
	require.NoError(t, err)
	require.Empty(t, row.APIKey, "a NULL api_key column maps back to an empty string")
}

// Vendor-agnostic schema: each provider keeps an independent row keyed by provider id.
func TestMetadataProviderConfigProvidersAreIndependent(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repo := repository.NewMetadataProviderConfigRepository(d)
	ctx := context.Background()

	_, err := repo.Upsert(ctx, repository.MetadataProviderConfigUpsert{Provider: "comicvine", APIKey: "cv-key"}, ts)
	require.NoError(t, err)
	_, err = repo.Upsert(ctx, repository.MetadataProviderConfigUpsert{Provider: "metron", APIKey: "metron-key"}, ts)
	require.NoError(t, err)

	cv, err := repo.Get(ctx, "comicvine")
	require.NoError(t, err)
	require.Equal(t, "cv-key", cv.APIKey)

	metron, err := repo.Get(ctx, "metron")
	require.NoError(t, err)
	require.Equal(t, "metron-key", metron.APIKey)
}
