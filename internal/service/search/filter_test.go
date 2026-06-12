package search_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/provider/indexer"
	"github.com/vizvim/omnibus/internal/service/search"
	"github.com/vizvim/omnibus/internal/service/series"
)

// target7 is the IssueMatch for issue "7" (sort 7.0, no qualifier).
func target7() search.IssueMatch {
	s, q := series.Normalize("7")
	return search.IssueMatch{Sort: s, Qual: q}
}

func cand(title, issueRaw, releaseKey, format string, size int64) indexer.Candidate {
	return indexer.Candidate{
		Provider:       "newznab",
		ReleaseKey:     releaseKey,
		Title:          title,
		IssueNumberRaw: issueRaw,
		SizeBytes:      size,
		Format:         format,
	}
}

func TestHardFilterIssueMatchRejectsQualifierMismatch(t *testing.T) {
	t.Parallel()
	opts := search.DefaultFilterOpts()

	// A 7.INH candidate must be rejected for a "7" target (pitfall #3).
	results := search.HardFilter(
		[]indexer.Candidate{cand("Saga #7.INH", "7.INH", "k1", "cbz", 10*1024*1024)},
		target7(), nil, opts,
	)
	require.Len(t, results, 1)
	require.True(t, results[0].Rejected)
	require.Equal(t, search.ReasonWrongIssue, results[0].Reason)

	// And a plain "7" candidate passes the issue-match gate.
	results = search.HardFilter(
		[]indexer.Candidate{cand("Saga #7", "7", "k2", "cbz", 10*1024*1024)},
		target7(), nil, opts,
	)
	require.False(t, results[0].Rejected)
}

func TestHardFilterRejectsForTargetWithQualifier(t *testing.T) {
	t.Parallel()
	s, q := series.Normalize("7.INH")
	targetINH := search.IssueMatch{Sort: s, Qual: q}

	// A plain "7" candidate is rejected for a "7.INH" target (vice-versa).
	results := search.HardFilter(
		[]indexer.Candidate{cand("Saga #7", "7", "k", "cbz", 10*1024*1024)},
		targetINH, nil, search.DefaultFilterOpts(),
	)
	require.True(t, results[0].Rejected)
	require.Equal(t, search.ReasonWrongIssue, results[0].Reason)
}

func TestHardFilterBlacklist(t *testing.T) {
	t.Parallel()
	bl := search.NewBlacklistSet([2]string{"newznab", "bad-key"})

	results := search.HardFilter(
		[]indexer.Candidate{cand("Saga #7", "7", "bad-key", "cbz", 10*1024*1024)},
		target7(), bl, search.DefaultFilterOpts(),
	)
	require.True(t, results[0].Rejected)
	require.Equal(t, search.ReasonBlacklisted, results[0].Reason)
}

func TestHardFilterIgnoreWord(t *testing.T) {
	t.Parallel()
	results := search.HardFilter(
		[]indexer.Candidate{cand("Saga #7 sample (cbz)", "7", "k", "cbz", 10*1024*1024)},
		target7(), nil, search.DefaultFilterOpts(),
	)
	require.True(t, results[0].Rejected)
	require.Equal(t, search.ReasonIgnoreWord, results[0].Reason)
}

func TestHardFilterSizeBounds(t *testing.T) {
	t.Parallel()
	opts := search.DefaultFilterOpts()

	// Too small (below 1MB).
	tooSmall := search.HardFilter(
		[]indexer.Candidate{cand("Saga #7", "7", "k", "cbz", 100)},
		target7(), nil, opts,
	)
	require.True(t, tooSmall[0].Rejected)
	require.Equal(t, search.ReasonSize, tooSmall[0].Reason)

	// Too large (above 500MB).
	tooBig := search.HardFilter(
		[]indexer.Candidate{cand("Saga #7", "7", "k", "cbz", 600*1024*1024)},
		target7(), nil, opts,
	)
	require.True(t, tooBig[0].Rejected)
	require.Equal(t, search.ReasonSize, tooBig[0].Reason)
}

func TestHardFilterUnknownSizePasses(t *testing.T) {
	t.Parallel()
	// Unknown size (0) must NOT be rejected (D-07).
	results := search.HardFilter(
		[]indexer.Candidate{cand("Saga #7", "7", "k", "cbz", 0)},
		target7(), nil, search.DefaultFilterOpts(),
	)
	require.False(t, results[0].Rejected)
}

func TestHardFilterFormat(t *testing.T) {
	t.Parallel()
	results := search.HardFilter(
		[]indexer.Candidate{cand("Saga #7", "7", "k", "exe", 10*1024*1024)},
		target7(), nil, search.DefaultFilterOpts(),
	)
	require.True(t, results[0].Rejected)
	require.Equal(t, search.ReasonFormat, results[0].Reason)
}

func TestSurvivorsKeepsOnlyAccepted(t *testing.T) {
	t.Parallel()
	results := search.HardFilter(
		[]indexer.Candidate{
			cand("Saga #7", "7", "good", "cbz", 10*1024*1024),
			cand("Saga #8", "8", "wrong", "cbz", 10*1024*1024),
		},
		target7(), nil, search.DefaultFilterOpts(),
	)
	survivors := search.Survivors(results)
	require.Len(t, survivors, 1)
	require.Equal(t, "good", survivors[0].ReleaseKey)
}
