package search_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/indexer"
	"github.com/vizvim/omnibus/internal/search"
)

func scoredCand(releaseKey, format string, size int64, pack bool) indexer.Candidate {
	return indexer.Candidate{
		Provider:    "newznab",
		ReleaseKey:  releaseKey,
		Format:      format,
		SizeBytes:   size,
		IsPack:      pack,
		DownloadURL: "http://x/" + releaseKey,
	}
}

func TestScoreQualityOrdering(t *testing.T) {
	t.Parallel()
	opts := search.DefaultScoreOpts()
	size := int64(30 * 1024 * 1024)

	cbz := search.Score(scoredCand("a", "cbz", size, false), opts)
	cbr := search.Score(scoredCand("a", "cbr", size, false), opts)
	pdf := search.Score(scoredCand("a", "pdf", size, false), opts)

	require.Greater(t, cbz, cbr)
	require.Greater(t, cbr, pdf)
}

func TestRankOrdersByScoreDescending(t *testing.T) {
	t.Parallel()
	size := int64(30 * 1024 * 1024)
	ranked := search.Rank([]indexer.Candidate{
		scoredCand("pdf", "pdf", size, false),
		scoredCand("cbz", "cbz", size, false),
		scoredCand("cbr", "cbr", size, false),
	}, search.DefaultScoreOpts())

	require.Len(t, ranked, 3)
	require.Equal(t, "cbz", ranked[0].Candidate.Format)
	require.Equal(t, "cbr", ranked[1].Candidate.Format)
	require.Equal(t, "pdf", ranked[2].Candidate.Format)
}

func TestRankTieBreakOnReleaseKey(t *testing.T) {
	t.Parallel()
	// Two otherwise-identical candidates differ only by release_key — the tie-break
	// must order them deterministically (ascending release_key) across runs.
	size := int64(30 * 1024 * 1024)
	in := []indexer.Candidate{
		scoredCand("zzz", "cbz", size, false),
		scoredCand("aaa", "cbz", size, false),
	}

	first := search.Rank(in, search.DefaultScoreOpts())
	second := search.Rank(in, search.DefaultScoreOpts())

	require.Equal(t, "aaa", first[0].Candidate.ReleaseKey)
	require.Equal(t, "zzz", first[1].Candidate.ReleaseKey)
	// Stable across runs.
	require.Equal(t, first[0].Candidate.ReleaseKey, second[0].Candidate.ReleaseKey)
	require.Equal(t, first[1].Candidate.ReleaseKey, second[1].Candidate.ReleaseKey)
}

func TestScoreSingleBeatsPack(t *testing.T) {
	t.Parallel()
	opts := search.DefaultScoreOpts()
	size := int64(30 * 1024 * 1024)

	single := search.Score(scoredCand("a", "cbz", size, false), opts)
	pack := search.Score(scoredCand("a", "cbz", size, true), opts)
	require.Greater(t, single, pack)
}

func TestScoreUnknownSizeIsNeutral(t *testing.T) {
	t.Parallel()
	opts := search.DefaultScoreOpts()

	// Unknown size scores neutral (half size-fit weight), so it neither dominates nor
	// is rejected on size — a known ideal-size candidate of the same format outranks it.
	unknown := search.Score(scoredCand("a", "cbz", 0, false), opts)
	ideal := search.Score(scoredCand("a", "cbz", opts.IdealSizeBytes, false), opts)
	require.Greater(t, ideal, unknown)
}
