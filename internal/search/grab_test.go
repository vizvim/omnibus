package search_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/download"
	"github.com/vizvim/omnibus/internal/indexer"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/search"
)

const ts = "2026-01-01T00:00:00Z"

func newGrabFixture(t *testing.T) (*search.Grabber, *repository.Repositories, *download.FakeProvider, int64, *db.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "grab.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	repos := repository.NewRepositories(d)
	ctx := context.Background()
	s, err := repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: 4050, Name: "Saga", Status: "Active", CreatedAt: ts,
	})
	require.NoError(t, err)
	iss, err := repos.Issue.Upsert(ctx, repository.IssueUpsert{
		SeriesID: s.ID, ComicvineIssueID: 9001, IssueNumberRaw: "7", IssueNumberSort: 7.0,
		Status: "Wanted", CreatedAt: ts,
	})
	require.NoError(t, err)

	fake := download.NewFakeProvider("sabnzbd", "nzo_abc")
	grabber := search.NewGrabber(search.GrabDeps{
		Providers:  map[string]search.Submitter{"sabnzbd": fake},
		Repos:      repos,
		AttemptCap: 5,
	})
	return grabber, repos, fake, iss.ID, d
}

func candFor(releaseKey string) indexer.Candidate {
	return indexer.Candidate{
		Provider:    "newznab",
		ReleaseKey:  releaseKey,
		Title:       "Saga #7 (cbz)",
		Format:      "cbz",
		SizeBytes:   30 * 1024 * 1024,
		DownloadURL: "http://nzb/" + releaseKey + ".nzb",
	}
}

func TestGrabWritesDownloadSnatchedAndEvent(t *testing.T) {
	t.Parallel()
	grabber, repos, fake, issueID, _ := newGrabFixture(t)
	ctx := context.Background()

	cand := candFor("rk-1")
	res, err := grabber.Grab(ctx, issueID, "sabnzbd", "rk-1", cand)
	require.NoError(t, err)
	require.Equal(t, "nzo_abc", res.ClientRef)
	require.Equal(t, "Queued", res.Status)
	require.Equal(t, "http://nzb/rk-1.nzb", fake.LastRequest.DownloadURL)

	// Download row exists with the client ref.
	dls, err := repos.Downloads.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, dls, 1)
	require.Equal(t, "nzo_abc", dls[0].ClientRef)
	require.Equal(t, "sabnzbd", dls[0].Provider)

	// Issue flipped Wanted→Snatched.
	iss, err := repos.Issue.GetByID(ctx, issueID)
	require.NoError(t, err)
	require.Equal(t, "Snatched", iss.Status)

	// A snatched timeline event was written with the release_key payload.
	events, err := repos.IssueEvents.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "snatched", events[0].EventType)
	require.Contains(t, events[0].PayloadJSON, "rk-1")
}

func TestGrabIsIdempotent(t *testing.T) {
	t.Parallel()
	grabber, repos, _, issueID, _ := newGrabFixture(t)
	ctx := context.Background()
	cand := candFor("rk-1")

	_, err := grabber.Grab(ctx, issueID, "sabnzbd", "rk-1", cand)
	require.NoError(t, err)
	// Second grab of the same (provider, release_key, issue) — single download row.
	_, err = grabber.Grab(ctx, issueID, "sabnzbd", "rk-1", cand)
	require.NoError(t, err)

	dls, err := repos.Downloads.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, dls, 1, "duplicate grab must not create a second download row")
}

func TestGrabRejectsCrossIssueRelease(t *testing.T) {
	t.Parallel()
	grabber, _, _, issueID, _ := newGrabFixture(t)
	ctx := context.Background()

	// releaseKey arg does not match the candidate's ReleaseKey (T-4-02).
	_, err := grabber.Grab(ctx, issueID, "sabnzbd", "rk-mismatch", candFor("rk-1"))
	require.ErrorIs(t, err, search.ErrCrossIssueGrab)
}

func TestGrabUnknownProviderErrors(t *testing.T) {
	t.Parallel()
	grabber, _, _, issueID, _ := newGrabFixture(t)
	ctx := context.Background()

	_, err := grabber.Grab(ctx, issueID, "nope", "rk-1", candFor("rk-1"))
	require.ErrorIs(t, err, search.ErrProviderNotFound)
}

func TestGrabWritesNoDownloadHistory(t *testing.T) {
	t.Parallel()
	grabber, _, _, issueID, d := newGrabFixture(t)
	ctx := context.Background()

	_, err := grabber.Grab(ctx, issueID, "sabnzbd", "rk-1", candFor("rk-1"))
	require.NoError(t, err)

	// Phase-5 boundary: grab writes NO download_history rows.
	var count int
	row := d.Read.QueryRowContext(ctx, "SELECT count(*) FROM download_history WHERE issue_id = ?", issueID)
	require.NoError(t, row.Scan(&count))
	require.Equal(t, 0, count, "grab must not write download_history (Phase 5 boundary)")
}
