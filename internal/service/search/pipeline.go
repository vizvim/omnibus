package search

import (
	"fmt"

	"github.com/vizvim/omnibus/internal/provider/indexer"
)

// DefaultAcceptanceFloor is the fixed minimum score the top survivor must clear to be an
// acceptable pick (D-02/D-05). Calibrated against DefaultScoreOpts: a cbz at neutral
// size with completeness clears it; a lone pdf of unknown size does not.
const DefaultAcceptanceFloor = 60.0

// PipelineResult is the full, observable outcome of the shared search pipeline. Ranked
// is always populated (the manual UI shows it even when nothing is acceptable); Pick is
// the winning candidate only when Acceptable is true. Rejections + FloorReason carry the
// structured detail the caller writes into issue_events + slog (D-04). This package does
// NOT write events — it has no DB dependency.
type PipelineResult struct {
	Ranked      []ScoredCandidate
	Pick        *ScoredCandidate
	Acceptable  bool
	Rejections  []FilterResult
	FloorReason string
}

// Pipeline runs the one shared filter → rank → acceptance-floor flow (D-15). It is a
// pure function: identical input yields an identical result.
func Pipeline(
	cands []indexer.Candidate,
	target IssueMatch,
	bl BlacklistSet,
	fopts FilterOpts,
	sopts ScoreOpts,
	floor float64,
) PipelineResult {
	filtered := HardFilter(cands, target, bl, fopts)

	rejections := make([]FilterResult, 0)
	survivors := make([]indexer.Candidate, 0, len(filtered))
	for _, r := range filtered {
		if r.Rejected {
			rejections = append(rejections, r)
		} else {
			survivors = append(survivors, r.Candidate)
		}
	}

	ranked := Rank(survivors, sopts)

	result := PipelineResult{
		Ranked:     ranked,
		Rejections: rejections,
	}

	if len(ranked) == 0 {
		result.Acceptable = false
		result.FloorReason = "no candidate passed the hard filter"
		return result
	}

	top := ranked[0]
	if top.Score < floor {
		result.Acceptable = false
		result.FloorReason = fmt.Sprintf("best candidate scored %.0f < floor %.0f", top.Score, floor)
		return result
	}

	// Acceptable: copy the top into Pick (a pointer into a stable local).
	pick := top
	result.Acceptable = true
	result.Pick = &pick
	return result
}
