package search_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/indexer"
	"github.com/vizvim/omnibus/internal/search"
	"github.com/vizvim/omnibus/internal/series"
)

func pipelineTarget(raw string) search.IssueMatch {
	s, q := series.Normalize(raw)
	return search.IssueMatch{Sort: s, Qual: q}
}

func pCand(releaseKey, issueRaw, format string, size int64, pack bool) indexer.Candidate {
	return indexer.Candidate{
		Provider:       "newznab",
		ReleaseKey:     releaseKey,
		Title:          "Saga " + issueRaw + " (" + format + ")",
		IssueNumberRaw: issueRaw,
		Format:         format,
		SizeBytes:      size,
		IsPack:         pack,
		DownloadURL:    "http://x/" + releaseKey,
	}
}

func TestPipelinePicksTopAcceptable(t *testing.T) {
	t.Parallel()
	size := int64(30 * 1024 * 1024)
	res := search.Pipeline(
		[]indexer.Candidate{
			pCand("pdf7", "7", "pdf", size, false),
			pCand("cbz7", "7", "cbz", size, false),
		},
		pipelineTarget("7"), nil,
		search.DefaultFilterOpts(), search.DefaultScoreOpts(), search.DefaultAcceptanceFloor,
	)

	require.True(t, res.Acceptable)
	require.NotNil(t, res.Pick)
	require.Equal(t, "cbz7", res.Pick.Candidate.ReleaseKey)
	require.Len(t, res.Ranked, 2) // both survive the filter
}

// TestPipelineUntaggedSingleIssueClearsFloor pins the calibration fix: most indexer
// release titles carry no container-format token, so inferFormat yields "" for legitimate
// single issues. With the old unknown-format weight (20) such a release scored 20 + ~24 +
// 10 = 54 and was wrongly gated out below the 60 floor (the Absolute Batman #1 symptom).
// An untagged single issue at a realistic size must now be acceptable.
func TestPipelineUntaggedSingleIssueClearsFloor(t *testing.T) {
	t.Parallel()
	size := int64(36 * 1024 * 1024) // ~36MB, a plausible single-issue size (not the 30MB ideal)
	res := search.Pipeline(
		[]indexer.Candidate{pCand("untagged7", "7", "", size, false)},
		pipelineTarget("7"), nil,
		search.DefaultFilterOpts(), search.DefaultScoreOpts(), search.DefaultAcceptanceFloor,
	)

	require.True(t, res.Acceptable)
	require.NotNil(t, res.Pick)
	require.Equal(t, "untagged7", res.Pick.Candidate.ReleaseKey)
}

func TestPipelineRankedPopulatedEvenWhenNotAcceptable(t *testing.T) {
	t.Parallel()
	// A lone pdf pack of unknown size scores below the default floor (40 + 15 + 0 = 55).
	res := search.Pipeline(
		[]indexer.Candidate{pCand("weak", "7", "pdf", 0, true)},
		pipelineTarget("7"), nil,
		search.DefaultFilterOpts(), search.DefaultScoreOpts(), search.DefaultAcceptanceFloor,
	)

	require.False(t, res.Acceptable)
	require.Nil(t, res.Pick)
	require.NotEmpty(t, res.FloorReason)
	require.Contains(t, res.FloorReason, "floor")
	// The manual UI still sees the ranked list even when nothing is acceptable.
	require.Len(t, res.Ranked, 1)
}

func TestPipelineNoSurvivorsHasReason(t *testing.T) {
	t.Parallel()
	// Wrong issue number → rejected by the hard filter → no survivors.
	res := search.Pipeline(
		[]indexer.Candidate{pCand("wrong", "8", "cbz", 30*1024*1024, false)},
		pipelineTarget("7"), nil,
		search.DefaultFilterOpts(), search.DefaultScoreOpts(), search.DefaultAcceptanceFloor,
	)

	require.False(t, res.Acceptable)
	require.Nil(t, res.Pick)
	require.Empty(t, res.Ranked)
	require.Equal(t, "no candidate passed the hard filter", res.FloorReason)
	require.Len(t, res.Rejections, 1)
	require.Equal(t, search.ReasonWrongIssue, res.Rejections[0].Reason)
}

func TestPipelineIsDeterministic(t *testing.T) {
	t.Parallel()
	size := int64(30 * 1024 * 1024)
	in := []indexer.Candidate{
		pCand("b", "7", "cbz", size, false),
		pCand("a", "7", "cbz", size, false),
		pCand("c", "7", "cbr", size, false),
	}
	first := search.Pipeline(in, pipelineTarget("7"), nil,
		search.DefaultFilterOpts(), search.DefaultScoreOpts(), search.DefaultAcceptanceFloor)
	second := search.Pipeline(in, pipelineTarget("7"), nil,
		search.DefaultFilterOpts(), search.DefaultScoreOpts(), search.DefaultAcceptanceFloor)

	require.Equal(t, first.Pick.Candidate.ReleaseKey, second.Pick.Candidate.ReleaseKey)
	require.Equal(t, "a", first.Pick.Candidate.ReleaseKey) // tie-break: lowest release_key among top cbz
	require.Len(t, first.Ranked, 3)
	for i := range first.Ranked {
		require.Equal(t, first.Ranked[i].Candidate.ReleaseKey, second.Ranked[i].Candidate.ReleaseKey)
	}
}
