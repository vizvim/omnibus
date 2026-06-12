package series_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/service/series"
)

func TestCanTransitionLegal(t *testing.T) {
	t.Parallel()
	const attemptCap = 5
	legal := []struct{ from, to series.IssueStatus }{
		{series.StatusWanted, series.StatusSnatched},
		{series.StatusWanted, series.StatusSkipped},
		{series.StatusWanted, series.StatusIgnored},
		{series.StatusSnatched, series.StatusDownloaded},
		{series.StatusSnatched, series.StatusFailed},
		{series.StatusDownloaded, series.StatusArchived},
		{series.StatusDownloaded, series.StatusFailed},
		{series.StatusFailed, series.StatusIgnored},
		{series.StatusSkipped, series.StatusWanted},
		{series.StatusIgnored, series.StatusWanted},
		{series.StatusArchived, series.StatusWanted},
	}
	for _, tc := range legal {
		require.True(t, series.CanTransition(tc.from, tc.to, 0, attemptCap), "%s -> %s should be legal", tc.from, tc.to)
	}
}

func TestCanTransitionIllegal(t *testing.T) {
	t.Parallel()
	const attemptCap = 5
	illegal := []struct{ from, to series.IssueStatus }{
		{series.StatusWanted, series.StatusDownloaded},
		{series.StatusWanted, series.StatusArchived},
		{series.StatusArchived, series.StatusSnatched},
		{series.StatusDownloaded, series.StatusWanted},
		{series.StatusSnatched, series.StatusWanted},
	}
	for _, tc := range illegal {
		require.False(t, series.CanTransition(tc.from, tc.to, 0, attemptCap), "%s -> %s should be illegal", tc.from, tc.to)
	}
}

func TestFailedToWantedAttemptCap(t *testing.T) {
	t.Parallel()
	const attemptCap = 3
	// Below the cap: Failed -> Wanted allowed.
	require.True(t, series.CanTransition(series.StatusFailed, series.StatusWanted, 2, attemptCap))
	// At/above the cap: blocked (loop prevention).
	require.False(t, series.CanTransition(series.StatusFailed, series.StatusWanted, 3, attemptCap))
	require.False(t, series.CanTransition(series.StatusFailed, series.StatusWanted, 4, attemptCap))
}

func TestTransitionReturnsError(t *testing.T) {
	t.Parallel()
	const attemptCap = 5
	_, err := series.Transition(series.StatusWanted, series.StatusDownloaded, 0, attemptCap)
	require.Error(t, err)

	next, err := series.Transition(series.StatusWanted, series.StatusSnatched, 0, attemptCap)
	require.NoError(t, err)
	require.Equal(t, series.StatusSnatched, next)
}

func TestDownloadTransition(t *testing.T) {
	t.Parallel()

	legal := []struct{ from, to series.DownloadStatus }{
		{series.DownloadQueued, series.DownloadDownloading},
		{series.DownloadQueued, series.DownloadCompleted},
		{series.DownloadQueued, series.DownloadFailed},
		{series.DownloadQueued, series.DownloadBlacklisted},
		{series.DownloadDownloading, series.DownloadCompleted},
		{series.DownloadDownloading, series.DownloadFailed},
		{series.DownloadDownloading, series.DownloadBlacklisted},
		{series.DownloadFailed, series.DownloadBlacklisted},
	}
	for _, tc := range legal {
		require.True(t, series.CanTransitionDownload(tc.from, tc.to), "%s -> %s should be legal", tc.from, tc.to)
		got, err := series.TransitionDownload(tc.from, tc.to)
		require.NoError(t, err)
		require.Equal(t, tc.to, got)
	}

	illegal := []struct{ from, to series.DownloadStatus }{
		{series.DownloadCompleted, series.DownloadDownloading},
		{series.DownloadCompleted, series.DownloadFailed},
		{series.DownloadFailed, series.DownloadDownloading},
		{series.DownloadFailed, series.DownloadCompleted},
		{series.DownloadBlacklisted, series.DownloadQueued},
		{series.DownloadDownloading, series.DownloadQueued},
	}
	for _, tc := range illegal {
		require.False(t, series.CanTransitionDownload(tc.from, tc.to), "%s -> %s should be illegal", tc.from, tc.to)
		_, err := series.TransitionDownload(tc.from, tc.to)
		require.Error(t, err)
	}
}
