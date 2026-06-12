package search

import (
	"math"
	"sort"
	"strings"

	"github.com/vizvim/omnibus/internal/provider/indexer"
)

// ScoreOpts are the weighted-score knobs (fixed code defaults this phase, D-05/D-07).
type ScoreOpts struct {
	// QualityWeights maps a lowercase format to its quality contribution (cbz>cbr>pdf,
	// D-07). Unknown formats fall back to QualityWeights[""].
	QualityWeights map[string]float64
	// SizeFitWeight scales the size-fit component (closeness to IdealSizeBytes).
	SizeFitWeight float64
	// IdealSizeBytes is the size a single issue is expected to be; the size-fit
	// component peaks here and decays as the candidate diverges.
	IdealSizeBytes int64
	// CompletenessWeight rewards single issues over packs (small weight).
	CompletenessWeight float64
}

// DefaultScoreOpts are the fixed default weights: quality dominates, then size-fit,
// then completeness (D-03 signal order).
func DefaultScoreOpts() ScoreOpts {
	return ScoreOpts{
		QualityWeights: map[string]float64{
			"cbz": 100,
			"cbr": 70,
			"pdf": 40,
			"":    20, // unknown format — neutral-low
		},
		SizeFitWeight:      30,
		IdealSizeBytes:     30 * 1024 * 1024, // ~30MB single issue
		CompletenessWeight: 10,
	}
}

// ScoredCandidate pairs a candidate with its computed score.
type ScoredCandidate struct {
	Candidate indexer.Candidate
	Score     float64
}

// Score computes the weighted additive score for a candidate: quality (dominant) +
// size-fit (medium, neutral when size unknown) + completeness (small).
func Score(c indexer.Candidate, opts ScoreOpts) float64 {
	return qualityScore(c, opts) + sizeFitScore(c, opts) + completenessScore(c, opts)
}

// qualityScore looks up the format weight (unknown formats use the "" fallback).
func qualityScore(c indexer.Candidate, opts ScoreOpts) float64 {
	key := strings.ToLower(c.Format)
	if w, ok := opts.QualityWeights[key]; ok {
		return w
	}
	return opts.QualityWeights[""]
}

// sizeFitScore is SizeFitWeight scaled by closeness to IdealSizeBytes. Unknown size (0)
// scores neutral (half weight) so it neither wins nor loses on size alone (D-07).
func sizeFitScore(c indexer.Candidate, opts ScoreOpts) float64 {
	if c.SizeBytes <= 0 || opts.IdealSizeBytes <= 0 {
		return opts.SizeFitWeight * 0.5
	}
	ratio := float64(c.SizeBytes) / float64(opts.IdealSizeBytes)
	// A symmetric closeness in log-space: 1.0 at the ideal, decaying for too-small or
	// too-large. fit in (0,1].
	fit := 1.0 / (1.0 + math.Abs(math.Log2(ratio)))
	return opts.SizeFitWeight * fit
}

// completenessScore rewards a single issue over a pack.
func completenessScore(c indexer.Candidate, opts ScoreOpts) float64 {
	if c.IsPack {
		return 0
	}
	return opts.CompletenessWeight
}

// Rank scores the survivors and returns them sorted by score descending, with a
// deterministic tie-break on release_key (D-03) so ordering is stable across runs.
func Rank(survivors []indexer.Candidate, opts ScoreOpts) []ScoredCandidate {
	scored := make([]ScoredCandidate, 0, len(survivors))
	for _, c := range survivors {
		scored = append(scored, ScoredCandidate{Candidate: c, Score: Score(c, opts)})
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		// Deterministic tie-break on release_key (ascending).
		return scored[i].Candidate.ReleaseKey < scored[j].Candidate.ReleaseKey
	})
	return scored
}
