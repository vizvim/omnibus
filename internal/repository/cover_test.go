package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/repository"
)

func TestCoverRepository(t *testing.T) {
	t.Parallel()
	d := newTestDB(t)
	repo := repository.NewCoverRepository(d)
	ctx := context.Background()

	// (a) Get on an absent cover returns found=false, err=nil.
	img, ct, found, err := repo.Get(ctx, "series", 1)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, img)
	require.Empty(t, ct)

	// (d) Exists is false before upsert.
	exists, err := repo.Exists(ctx, "series", 1)
	require.NoError(t, err)
	require.False(t, exists)

	// (b) Upsert then Get round-trips the exact bytes and content_type.
	original := []byte("\xff\xd8\xffORIGINAL")
	require.NoError(t, repo.Upsert(ctx, "series", 1, original, "image/jpeg"))

	img, ct, found, err = repo.Get(ctx, "series", 1)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, original, img)
	require.Equal(t, "image/jpeg", ct)

	// (d) Exists is true after upsert.
	exists, err = repo.Exists(ctx, "series", 1)
	require.NoError(t, err)
	require.True(t, exists)

	// (c) A second upsert for the same (entity_type, id) replaces image + content_type.
	replacement := []byte("\x89PNGREPLACED")
	require.NoError(t, repo.Upsert(ctx, "series", 1, replacement, "image/png"))

	img, ct, found, err = repo.Get(ctx, "series", 1)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, replacement, img)
	require.Equal(t, "image/png", ct)

	// A different (entity_type, id) is independent and still absent.
	_, _, found, err = repo.Get(ctx, "issues", 1)
	require.NoError(t, err)
	require.False(t, found)
}
