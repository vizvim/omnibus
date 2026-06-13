package ddlconfig_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/ddlconfig"
	"github.com/vizvim/omnibus/internal/repository"
)

func newService(t *testing.T) *ddlconfig.Service {
	t.Helper()
	path := filepath.Join(t.TempDir(), "svc.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	repos := repository.NewRepositories(d)
	return ddlconfig.New(ddlconfig.Deps{Repos: repos})
}

func TestGetFreshDBDefaultsDisabled(t *testing.T) {
	t.Parallel()
	svc := newService(t)

	got, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.False(t, got.Enabled, "DDL is opt-in: a fresh DB defaults to disabled")
}

func TestDefaultDDLConfigMatchesGetDefaults(t *testing.T) {
	t.Parallel()
	svc := newService(t)

	got, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.Equal(t, ddlconfig.DefaultDDLConfig(), got)
}

func TestUpdatePersistsAndRoundTrips(t *testing.T) {
	t.Parallel()
	svc := newService(t)
	ctx := context.Background()

	saved, err := svc.Update(ctx, ddlconfig.Input{Enabled: true})
	require.NoError(t, err)
	require.True(t, saved.Enabled)

	// A subsequent Get returns the saved value (runtime persistence, no restart).
	got, err := svc.Get(ctx)
	require.NoError(t, err)
	require.True(t, got.Enabled)

	// Toggling back off round-trips too.
	_, err = svc.Update(ctx, ddlconfig.Input{Enabled: false})
	require.NoError(t, err)
	got, err = svc.Get(ctx)
	require.NoError(t, err)
	require.False(t, got.Enabled)
}
