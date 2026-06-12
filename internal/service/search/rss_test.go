package search_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/provider/download"
	"github.com/vizvim/omnibus/internal/provider/indexer"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/service/search"
)

// newRSSService builds a search service whose fake gateway returns the given feed
// candidates from Feed(), plus an RSS-enabled indexer and a single Wanted issue (#7).
func newRSSService(t *testing.T, feedCands []indexer.Candidate) (*search.Service, *repository.Repositories, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rss.db")
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

	_, err = repos.Indexers.Create(ctx, repository.IndexerUpsert{
		Name: "nzb", Kind: "newznab", BaseURL: "http://nzb.test", Enabled: true,
		Categories: "7030", UseForRSS: true,
	}, ts)
	require.NoError(t, err)

	svc := search.New(search.Deps{
		Gateway:           &fakeGateway{feed: feedCands},
		Repos:             repos,
		DownloadProviders: map[string]download.DownloadProvider{"sabnzbd": download.NewFakeProvider("sabnzbd", "nzo_rss")},
		AttemptCap:        5,
	})
	return svc, repos, iss.ID
}

func TestRSSPollGrabsMatchingFeedItem(t *testing.T) {
	t.Parallel()
	// A cbz feed item for issue #7 matches the Wanted issue and clears the floor.
	svc, repos, issueID := newRSSService(t, []indexer.Candidate{
		feedCand("newznab", "rk-cbz", "7", "cbz", 30*1024*1024),
	})
	ctx := context.Background()

	require.NoError(t, svc.RunRSSPoll(ctx))

	iss, err := repos.Issue.GetByID(ctx, issueID)
	require.NoError(t, err)
	require.Equal(t, "Snatched", iss.Status, "matching feed item auto-grabbed")

	dls, err := repos.Downloads.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, dls, 1)
	require.Equal(t, "rk-cbz", dls[0].ReleaseKey)
}

func TestRSSPollDedupsAlreadyGrabbedRelease(t *testing.T) {
	t.Parallel()
	svc, repos, issueID := newRSSService(t, []indexer.Candidate{
		feedCand("newznab", "rk-dup", "7", "cbz", 30*1024*1024),
	})
	ctx := context.Background()

	// Pre-seed a downloads row for the same (provider, release_key) — the dedup target.
	_, err := repos.Downloads.Upsert(ctx, repository.DownloadUpsert{
		IssueID: issueID, Provider: "newznab", ReleaseKey: "rk-dup",
		ReleaseTitle: "Saga #7", Status: "Queued", ClientRef: "pre",
	}, ts)
	require.NoError(t, err)

	require.NoError(t, svc.RunRSSPoll(ctx))

	// Still exactly one downloads row — no re-grab.
	dls, err := repos.Downloads.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Len(t, dls, 1)
	require.Equal(t, "pre", dls[0].ClientRef, "existing row untouched, no re-grab")

	// Issue not transitioned by the dedup-skipped grab.
	iss, err := repos.Issue.GetByID(ctx, issueID)
	require.NoError(t, err)
	require.Equal(t, "Wanted", iss.Status)
}

func TestRSSPollIgnoresNonMatchingItem(t *testing.T) {
	t.Parallel()
	// Feed item for issue #99 does not match the Wanted #7 issue.
	svc, repos, issueID := newRSSService(t, []indexer.Candidate{
		feedCand("newznab", "rk-99", "99", "cbz", 30*1024*1024),
	})
	ctx := context.Background()

	require.NoError(t, svc.RunRSSPoll(ctx))

	iss, err := repos.Issue.GetByID(ctx, issueID)
	require.NoError(t, err)
	require.Equal(t, "Wanted", iss.Status, "non-matching feed item ignored")

	events, err := repos.IssueEvents.ListByIssue(ctx, issueID)
	require.NoError(t, err)
	require.Empty(t, events, "no events for an unmatched issue")
}

func feedCand(provider, releaseKey, num, format string, size int64) indexer.Candidate {
	return indexer.Candidate{
		Provider:       provider,
		ReleaseKey:     releaseKey,
		Title:          "Saga #" + num + " (" + format + ")",
		IssueNumberRaw: num,
		Format:         format,
		SizeBytes:      size,
		DownloadURL:    "http://nzb/" + releaseKey + ".nzb",
	}
}
