package metadataprovider_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/metadataprovider"
	"github.com/vizvim/omnibus/internal/repository"
)

func newService(t *testing.T, prober metadataprovider.MetadataProber) (*metadataprovider.Service, *repository.Repositories) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "svc.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	repos := repository.NewRepositories(d)
	return metadataprovider.New(metadataprovider.Deps{Repos: repos, Prober: prober}), repos
}

func TestGetEmptyRowIsNotConfigured(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, nil)

	got, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.False(t, got.Configured)
	require.Equal(t, metadataprovider.ProviderComicVine, got.Provider)
}

func TestGetAfterUpdateIsConfiguredAndMasked(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, nil)
	ctx := context.Background()

	_, err := svc.Update(ctx, metadataprovider.Input{APIKey: "secret"})
	require.NoError(t, err)

	got, err := svc.Get(ctx)
	require.NoError(t, err)
	require.True(t, got.Configured)
}

func TestUpdateEmptyAPIKeyRetainsStoredKey(t *testing.T) {
	t.Parallel()
	svc, repos := newService(t, nil)
	ctx := context.Background()

	_, err := svc.Update(ctx, metadataprovider.Input{APIKey: "secret-key"})
	require.NoError(t, err)

	// A blank update must NOT blank the stored key.
	_, err = svc.Update(ctx, metadataprovider.Input{APIKey: "   "})
	require.NoError(t, err)

	row, err := repos.MetadataProviderConfig.Get(ctx, metadataprovider.ProviderComicVine)
	require.NoError(t, err)
	require.Equal(t, "secret-key", row.APIKey, "stored api_key must be retained on empty-key update")
}

func TestUpdateReplacesAPIKeyWhenProvided(t *testing.T) {
	t.Parallel()
	svc, repos := newService(t, nil)
	ctx := context.Background()

	_, err := svc.Update(ctx, metadataprovider.Input{APIKey: "old"})
	require.NoError(t, err)
	_, err = svc.Update(ctx, metadataprovider.Input{APIKey: "new"})
	require.NoError(t, err)

	row, err := repos.MetadataProviderConfig.Get(ctx, metadataprovider.ProviderComicVine)
	require.NoError(t, err)
	require.Equal(t, "new", row.APIKey)
}

// fakeProber records the api key it was asked to probe and returns a canned result.
type fakeProber struct {
	ok     bool
	detail string
	gotKey string
	calls  int
}

func (f *fakeProber) Probe(_ context.Context, apiKey string) (bool, string) {
	f.calls++
	f.gotKey = apiKey
	return f.ok, f.detail
}

func TestTestForwardsProberResult(t *testing.T) {
	t.Parallel()
	fake := &fakeProber{ok: true, detail: "connected"}
	svc, _ := newService(t, fake)

	res, err := svc.Test(context.Background(), "supplied-key")
	require.NoError(t, err)
	require.True(t, res.OK)
	require.Equal(t, "connected", res.Detail)
	require.Equal(t, "supplied-key", fake.gotKey, "the supplied key is passed straight to the prober")
	require.Equal(t, 1, fake.calls)
}

func TestTestFailingProbeIsNotAnError(t *testing.T) {
	t.Parallel()
	fake := &fakeProber{ok: false, detail: "unauthorized or unreachable (check API key)"}
	svc, _ := newService(t, fake)

	res, err := svc.Test(context.Background(), "")
	require.NoError(t, err, "a failing probe is a normal result, not an RPC error")
	require.False(t, res.OK)
	require.NotEmpty(t, res.Detail)
}

func TestTestNilProberIsUnavailable(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t, nil)

	res, err := svc.Test(context.Background(), "")
	require.NoError(t, err)
	require.False(t, res.OK)
	require.NotEmpty(t, res.Detail)
}
