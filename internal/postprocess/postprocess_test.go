package postprocess_test

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/download"
	"github.com/vizvim/omnibus/internal/postprocess"
	"github.com/vizvim/omnibus/internal/renameconfig"
	"github.com/vizvim/omnibus/internal/repository"
)

const ts = "2026-01-01T00:00:00Z"

// fakeReplacementEnqueuer records EnqueueReplacementSearch calls (the corrupt-archive seam).
type fakeReplacementEnqueuer struct{ issueIDs []int64 }

func (f *fakeReplacementEnqueuer) EnqueueReplacementSearch(_ context.Context, issueID int64) error {
	f.issueIDs = append(f.issueIDs, issueID)
	return nil
}

// fixture builds a temp-SQLite-backed post-process Service with a Snatched issue and a
// Completed download whose history carries storagePath, plus a fake provider for D-05 removal.
type fixture struct {
	svc        *postprocess.Service
	repos      *repository.Repositories
	fake       *download.FakeProvider
	enqueuer   *fakeReplacementEnqueuer
	issueID    int64
	downloadID int64
	libraryDir string
}

func newFixture(t *testing.T, storagePath string) *fixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "postprocess.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	repos := repository.NewRepositories(d)
	ctx := context.Background()

	pub, err := repos.Publisher.Upsert(ctx, repository.PublisherUpsert{Name: "Image Comics", CreatedAt: ts})
	require.NoError(t, err)
	pubID := pub.ID
	startYear := int32(2012)
	s, err := repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 4050, PublisherID: &pubID, Name: "Saga", StartYear: &startYear,
		Status: "Active", CreatedAt: ts,
	})
	require.NoError(t, err)

	iss, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: s.ID, ComicvineIssueID: 9001, IssueNumberRaw: "7", IssueNumberSort: 7.0,
		Title: "The Will", CoverDate: "2012-06-01", Status: "Snatched", CreatedAt: ts,
	})
	require.NoError(t, err)

	// A Completed download row + its completed-history storage path (what the poll loop wrote).
	row, err := repos.Downloads.Upsert(ctx, repository.DownloadUpsert{
		IssueID: iss.ID, Provider: "sabnzbd", ReleaseKey: "rk-1", ReleaseTitle: "Saga #7",
		SizeBytes: 30 * 1024 * 1024, Status: "Completed", ClientRef: "nzo_abc",
	}, ts)
	require.NoError(t, err)
	_, err = repos.Downloads.InsertHistory(ctx, repository.DownloadHistoryInsert{
		IssueID: iss.ID, Provider: "sabnzbd", ReleaseKey: "rk-1",
		Result: "completed", Detail: storagePath, OccurredAt: ts,
	})
	require.NoError(t, err)

	libraryDir := filepath.Join(t.TempDir(), "library")
	require.NoError(t, os.MkdirAll(libraryDir, 0o750))

	fake := download.NewFakeProvider("sabnzbd", "nzo_abc")
	enqueuer := &fakeReplacementEnqueuer{}

	svc := postprocess.New(postprocess.Deps{
		Repos:        repos,
		Providers:    map[string]download.DownloadProvider{"sabnzbd": fake},
		RenameConfig: renameConfigGetter{},
		LibraryPath:  libraryDir,
		AttemptCap:   5,
	})
	svc.SetEnqueuer(enqueuer)

	return &fixture{
		svc: svc, repos: repos, fake: fake, enqueuer: enqueuer,
		issueID: iss.ID, downloadID: row.ID, libraryDir: libraryDir,
	}
}

// renameConfigGetter returns the Mylar defaults (the production fresh-DB config).
type renameConfigGetter struct{}

func (renameConfigGetter) Get(_ context.Context) (renameconfig.Config, error) {
	return renameconfig.DefaultRenameConfig(), nil
}

// writeValidCBZ writes a minimal valid CBZ (a zip with one entry) to dir and returns its path.
func writeValidCBZ(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "saga-7.cbz")
	f, err := os.Create(p)
	require.NoError(t, err)
	zw := zip.NewWriter(f)
	w, err := zw.Create("001.jpg")
	require.NoError(t, err)
	_, err = w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}) // a few bytes; not validated as an image
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
	return p
}

func TestProcessHappyPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	srcDir := t.TempDir()
	src := writeValidCBZ(t, srcDir)
	fx := newFixture(t, src)

	require.NoError(t, fx.svc.Process(ctx, fx.downloadID))

	// The file is filed under LibraryPath at the rendered folder/file path.
	// Folder: "Image Comics/Saga (2012)", file: "Saga (2012) #007 (June 2012).cbz".
	expected := filepath.Join(fx.libraryDir, "Image Comics", "Saga (2012)", "Saga (2012) #007 (June 2012).cbz")
	_, statErr := os.Stat(expected)
	require.NoError(t, statErr, "imported file should exist at %s", expected)

	// Issue flipped Snatched -> Downloaded.
	iss, err := fx.repos.Issue.GetByID(ctx, fx.issueID)
	require.NoError(t, err)
	require.Equal(t, "Downloaded", iss.Status)

	// downloaded + processed timeline events written.
	events, err := fx.repos.IssueEvents.ListByIssue(ctx, fx.issueID)
	require.NoError(t, err)
	types := map[string]bool{}
	for _, e := range events {
		types[e.EventType] = true
	}
	require.True(t, types["downloaded"], "downloaded event missing")
	require.True(t, types["processed"], "processed event missing")

	// RemoveFromHistory was called for the client ref (D-05).
	require.Equal(t, []string{"nzo_abc"}, fx.fake.Removed)

	// No replacement was enqueued on the happy path.
	require.Empty(t, fx.enqueuer.issueIDs)
}

func TestProcessIdempotentReRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	srcDir := t.TempDir()
	src := writeValidCBZ(t, srcDir)
	fx := newFixture(t, src)

	require.NoError(t, fx.svc.Process(ctx, fx.downloadID))
	eventsAfterFirst, err := fx.repos.IssueEvents.ListByIssue(ctx, fx.issueID)
	require.NoError(t, err)

	// A second run on the now-Downloaded issue is a safe no-op: no new events, no extra remove.
	require.NoError(t, fx.svc.Process(ctx, fx.downloadID))
	eventsAfterSecond, err := fx.repos.IssueEvents.ListByIssue(ctx, fx.issueID)
	require.NoError(t, err)
	require.Len(t, eventsAfterSecond, len(eventsAfterFirst), "re-run must not write new events")
	require.Equal(t, []string{"nzo_abc"}, fx.fake.Removed, "re-run must not remove again")
}

func TestProcessCorruptArchiveRoutesToReplacement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// A file with a zip signature but truncated body fails structural validation.
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "corrupt.cbz")
	require.NoError(t, os.WriteFile(src, []byte("PK\x03\x04 truncated garbage"), 0o600))
	fx := newFixture(t, src)

	// A corrupt archive is a handled failure, not a job error.
	require.NoError(t, fx.svc.Process(ctx, fx.downloadID))

	// Issue flipped Snatched -> Failed.
	iss, err := fx.repos.Issue.GetByID(ctx, fx.issueID)
	require.NoError(t, err)
	require.Equal(t, "Failed", iss.Status)

	// download_attempts bumped (D-13).
	require.EqualValues(t, 1, iss.DownloadAttempts)

	// A replacement search was enqueued for the issue (D-16 shared failure path).
	require.Equal(t, []int64{fx.issueID}, fx.enqueuer.issueIDs)

	// A failed timeline event with reason=corrupt was written.
	events, err := fx.repos.IssueEvents.ListByIssue(ctx, fx.issueID)
	require.NoError(t, err)
	var sawCorrupt bool
	for _, e := range events {
		if e.EventType == "failed" {
			require.Contains(t, e.PayloadJSON, "corrupt")
			sawCorrupt = true
		}
	}
	require.True(t, sawCorrupt, "failed(corrupt) event missing")

	// No file was filed and the item was NOT removed from the client (T-05-11).
	require.Empty(t, fx.fake.Removed)
}
