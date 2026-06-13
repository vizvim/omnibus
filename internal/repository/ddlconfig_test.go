package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/repository"
)

func TestDDLConfigFreshDBNotPresent(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repo := repository.NewDDLConfigRepository(d)
	ctx := context.Background()

	row, err := repo.Get(ctx)
	require.NoError(t, err)
	require.False(t, row.Present, "a fresh DB has no enable_ddl key written")
	require.False(t, row.Enabled, "the default (no key) coerces to disabled")
}

func TestDDLConfigUpsertRoundTrip(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repo := repository.NewDDLConfigRepository(d)
	ctx := context.Background()

	saved, err := repo.Upsert(ctx, true)
	require.NoError(t, err)
	require.True(t, saved.Present)
	require.True(t, saved.Enabled)

	got, err := repo.Get(ctx)
	require.NoError(t, err)
	require.True(t, got.Present)
	require.True(t, got.Enabled)
}

func TestDDLConfigUpsertOverwrites(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repo := repository.NewDDLConfigRepository(d)
	ctx := context.Background()

	_, err := repo.Upsert(ctx, true)
	require.NoError(t, err)

	_, err = repo.Upsert(ctx, false)
	require.NoError(t, err)

	got, err := repo.Get(ctx)
	require.NoError(t, err)
	require.True(t, got.Present)
	require.False(t, got.Enabled, "a later upsert overwrites the stored value")
}
