package tracking_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/provider/download"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/service/tracking"
)

const ddlTS = "2026-01-01T00:00:00Z"

// fakeDDLFetcher returns a fixed path or a fixed error, recording the clientRef it was asked
// to fetch. It satisfies tracking.DDLFetcher.
type fakeDDLFetcher struct {
	path      string
	err       error
	clientRef string
}

func (f *fakeDDLFetcher) Fetch(_ context.Context, clientRef string, progress func(done, total int64)) (string, error) {
	f.clientRef = clientRef
	if progress != nil {
		progress(50, 100) // simulate mid-stream progress so the progress branch is exercised
	}
	if f.err != nil {
		return "", f.err
	}
	return f.path, nil
}

// fakeDDLEnqueuer records which downstream jobs the DDL runner fans out to.
type fakeDDLEnqueuer struct {
	postProcessed []int64
	replacements  []int64
}

func (f *fakeDDLEnqueuer) EnqueuePostProcess(_ context.Context, downloadID int64) error {
	f.postProcessed = append(f.postProcessed, downloadID)
	return nil
}

func (f *fakeDDLEnqueuer) EnqueueReplacementSearch(_ context.Context, issueID int64) error {
	f.replacements = append(f.replacements, issueID)
	return nil
}

// ddlFixture builds a temp-SQLite-backed tracking Service with a Queued getcomics download and
// a DDL fetcher/enqueuer the test controls.
type ddlFixture struct {
	svc        *tracking.Service
	repos      *repository.Repositories
	fetcher    *fakeDDLFetcher
	enqueuer   *fakeDDLEnqueuer
	issueID    int64
	downloadID int64
}

func newDDLFixture(t *testing.T, fetcher *fakeDDLFetcher) *ddlFixture {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tracking.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	repos := repository.NewRepositories(d)

	s, err := repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 4050, Name: "Saga", Status: "Active", CreatedAt: ddlTS,
	})
	require.NoError(t, err)
	iss, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: s.ID, ComicvineIssueID: 9001, IssueNumberRaw: "7", IssueNumberSort: 7.0,
		Status: "Snatched", CreatedAt: ddlTS,
	})
	require.NoError(t, err)
	// A Queued getcomics download whose clientRef is the post URL (the DDL handoff).
	row, err := repos.Downloads.Upsert(ctx, repository.DownloadUpsert{
		IssueID: iss.ID, Provider: "getcomics", ReleaseKey: "https://getcomics.org/post/1",
		ReleaseTitle: "Saga #7", SizeBytes: 30 * 1024 * 1024, Status: "Queued",
		ClientRef: "https://getcomics.org/post/1",
	}, ddlTS)
	require.NoError(t, err)

	enqueuer := &fakeDDLEnqueuer{}
	svc := tracking.New(tracking.Deps{
		Repos:       repos,
		DDLFetchers: map[string]tracking.DDLFetcher{"getcomics": fetcher},
	})
	svc.SetEnqueuer(enqueuer)

	return &ddlFixture{
		svc: svc, repos: repos, fetcher: fetcher, enqueuer: enqueuer,
		issueID: iss.ID, downloadID: row.ID,
	}
}

// TestRunDDLFetchSuccessFeedsPostProcess asserts a successful fetch flips the download to
// Completed (carrying the streamed path on history) and enqueues post-process — the SAME path
// a SAB download takes (D-03).
func TestRunDDLFetchSuccessFeedsPostProcess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	streamed := filepath.Join(t.TempDir(), "incomplete", "saga-7.cbz")
	fx := newDDLFixture(t, &fakeDDLFetcher{path: streamed})

	require.NoError(t, fx.svc.RunDDLFetch(ctx, fx.downloadID))

	// The fetcher was asked to resolve the download's clientRef (the post URL).
	require.Equal(t, "https://getcomics.org/post/1", fx.fetcher.clientRef)

	// The download flipped to Completed and its completed-history carries the streamed path so
	// GetCompletedStorage (read by post-process) resolves it.
	row, err := fx.repos.Downloads.Get(ctx, fx.downloadID)
	require.NoError(t, err)
	require.Equal(t, "Completed", row.Status)
	storage, ok, err := fx.repos.Downloads.GetCompletedStorage(ctx, fx.issueID, "getcomics", "https://getcomics.org/post/1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, streamed, storage)

	// Post-process was enqueued; no replacement.
	require.Equal(t, []int64{fx.downloadID}, fx.enqueuer.postProcessed)
	require.Empty(t, fx.enqueuer.replacements)
}

// TestRunDDLFetchExhaustionFeedsReplacement asserts mirror exhaustion routes the download into
// the shared failure path (Failed + replacement search, D-04 -> D-16) and returns nil (a handled
// failure, not a job error River should retry).
func TestRunDDLFetchExhaustionFeedsReplacement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fx := newDDLFixture(t, &fakeDDLFetcher{err: download.ErrMirrorExhaustion})

	require.NoError(t, fx.svc.RunDDLFetch(ctx, fx.downloadID))

	row, err := fx.repos.Downloads.Get(ctx, fx.downloadID)
	require.NoError(t, err)
	require.Equal(t, "Failed", row.Status)

	// Replacement was enqueued for the issue; no post-process.
	require.Equal(t, []int64{fx.issueID}, fx.enqueuer.replacements)
	require.Empty(t, fx.enqueuer.postProcessed)
}

// TestRunDDLFetchTerminalIsNoOp asserts a re-run on an already-terminal download is a safe
// no-op (idempotent re-enqueue), neither re-fetching nor fanning out.
func TestRunDDLFetchTerminalIsNoOp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fx := newDDLFixture(t, &fakeDDLFetcher{path: "unused"})

	// Drive the download to Completed first.
	_, err := fx.repos.Downloads.UpdateStatus(ctx, fx.downloadID, "Completed", ddlTS)
	require.NoError(t, err)

	require.NoError(t, fx.svc.RunDDLFetch(ctx, fx.downloadID))

	require.Empty(t, fx.fetcher.clientRef, "a terminal download must not be re-fetched")
	require.Empty(t, fx.enqueuer.postProcessed)
	require.Empty(t, fx.enqueuer.replacements)
}
