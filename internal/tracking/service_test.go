package tracking

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository"
)

const failTS = "2026-01-01T00:00:00Z"

// failFixture builds a temp-SQLite-backed tracking Service with a Downloading download row so a
// terminal failure can be driven through onFailed and the issue's download_attempts inspected.
type failFixture struct {
	svc        *Service
	repos      *repository.Repositories
	issueID    int64
	downloadID int64
	row        repository.DownloadRow
}

func newFailFixture(t *testing.T) *failFixture {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tracking.db")
	require.NoError(t, db.Migrate(ctx, path))
	d, err := db.Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	repos := repository.NewRepositories(d)

	s, err := repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 4050, Name: "Saga", Status: "Active", CreatedAt: failTS,
	})
	require.NoError(t, err)
	iss, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: s.ID, ComicvineIssueID: 9001, IssueNumberRaw: "7", IssueNumberSort: 7.0,
		Status: "Snatched", CreatedAt: failTS,
	})
	require.NoError(t, err)
	row, err := repos.Downloads.Upsert(ctx, repository.DownloadUpsert{
		IssueID: iss.ID, Provider: "sabnzbd", ReleaseKey: "rk-1",
		ReleaseTitle: "Saga #7", SizeBytes: 30 * 1024 * 1024, Status: "Downloading",
		ClientRef: "nzo-1",
	}, failTS)
	require.NoError(t, err)

	svc := New(Deps{Repos: repos})

	return &failFixture{
		svc: svc, repos: repos, issueID: iss.ID, downloadID: row.ID, row: row,
	}
}

func TestOnFailedIncrementsDownloadAttempts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fx := newFailFixture(t)

	before, err := fx.repos.Issue.GetByID(ctx, fx.issueID)
	require.NoError(t, err)

	require.NoError(t, fx.svc.onFailed(ctx, fx.row, "client failure"))

	after, err := fx.repos.Issue.GetByID(ctx, fx.issueID)
	require.NoError(t, err)
	require.Equal(t, before.DownloadAttempts+1, after.DownloadAttempts,
		"a terminal download failure must count toward the replacement cap")

	row, err := fx.repos.Downloads.Get(ctx, fx.downloadID)
	require.NoError(t, err)
	require.Equal(t, "Failed", row.Status)
}

func TestStallReapIncrementsDownloadAttempts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fx := newFailFixture(t)

	// A stall is a terminal download failure: drive it through maybeStall (force the
	// row past the stall window) and assert the same single increment.
	stalled := fx.row
	stalled.UpdatedAt = "2000-01-01T00:00:00Z" // far in the past → past any stall window

	before, err := fx.repos.Issue.GetByID(ctx, fx.issueID)
	require.NoError(t, err)

	require.NoError(t, fx.svc.maybeStall(ctx, stalled))

	after, err := fx.repos.Issue.GetByID(ctx, fx.issueID)
	require.NoError(t, err)
	require.Equal(t, before.DownloadAttempts+1, after.DownloadAttempts,
		"a stall reap must count toward the replacement cap")
}
