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

// TestCreateRejectsGetComicsKind asserts GetComics is no longer a creatable indexer kind
// (05-07): only newznab is accepted. GetComics is a built-in static source toggled by the
// enable_ddl config, never an indexer row.
func TestCreateRejectsGetComicsKind(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)

	_, err := svc.Create(context.Background(), indexer.Input{
		Name:    "gc",
		Kind:    "getcomics",
		BaseURL: "https://getcomics.org",
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

func TestListAndDelete(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, indexer.Input{
		Name: "nzb", Kind: indexer.KindNewznab, BaseURL: "http://nzb.test", Enabled: true,
	})
	require.NoError(t, err)

	list, err := svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "nzb", list[0].Name)

	require.NoError(t, svc.Delete(ctx, created.ID))

	list, err = svc.List(ctx)
	require.NoError(t, err)
	require.Empty(t, list)
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

// fakeProber records the row it was handed and returns a canned result.
type fakeProber struct {
	result   indexer.TestResult
	gotRow   repository.IndexerRow
	gotCalls int
}

func (f *fakeProber) Probe(_ context.Context, row repository.IndexerRow) indexer.TestResult {
	f.gotCalls++
	f.gotRow = row
	return f.result
}

func newServiceWithProber(t *testing.T, prober indexer.IndexerProber) (*indexer.Service, *repository.Repositories) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "svc.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	repos := repository.NewRepositories(d)
	return indexer.New(indexer.Deps{Repos: repos, Prober: prober}), repos
}

func TestTestForwardsProberResultAndPassesAPIKey(t *testing.T) {
	t.Parallel()
	fake := &fakeProber{result: indexer.TestResult{OK: true, Detail: "connected"}}
	svc, _ := newServiceWithProber(t, fake)
	ctx := context.Background()

	created, err := svc.Create(ctx, indexer.Input{
		Name: "nzb", Kind: indexer.KindNewznab, BaseURL: "http://nzb.test", APIKey: "secret-key", Enabled: true,
	})
	require.NoError(t, err)

	res, err := svc.Test(ctx, created.ID)
	require.NoError(t, err)
	require.True(t, res.OK)
	require.Equal(t, "connected", res.Detail)
	require.Equal(t, 1, fake.gotCalls)
	require.Equal(t, "secret-key", fake.gotRow.APIKey, "the probe must receive the stored api_key")
}

func TestTestForwardsProberFailureWithoutError(t *testing.T) {
	t.Parallel()
	fake := &fakeProber{result: indexer.TestResult{OK: false, Detail: "unauthorized (check API key)"}}
	svc, _ := newServiceWithProber(t, fake)
	ctx := context.Background()

	created, err := svc.Create(ctx, indexer.Input{
		Name: "nzb", Kind: indexer.KindNewznab, BaseURL: "http://nzb.test", Enabled: true,
	})
	require.NoError(t, err)

	res, err := svc.Test(ctx, created.ID)
	require.NoError(t, err, "a failing probe is a normal result, not an error")
	require.False(t, res.OK)
	require.Equal(t, "unauthorized (check API key)", res.Detail)
}

func TestTestMissingIDReturnsError(t *testing.T) {
	t.Parallel()
	fake := &fakeProber{result: indexer.TestResult{OK: true}}
	svc, _ := newServiceWithProber(t, fake)

	_, err := svc.Test(context.Background(), 9999)
	require.Error(t, err, "a load failure is a genuine internal fault, not a probe ok=false")
	require.Equal(t, 0, fake.gotCalls, "the prober must not run when the row cannot be loaded")
}
