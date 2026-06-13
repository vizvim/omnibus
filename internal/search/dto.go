package search

import "github.com/vizvim/omnibus/internal/indexer"

// CandidateView is the domain view of a ranked candidate the transport layer consumes.
// Score is the pipeline score; Reason carries the human "why" for rejected/below-floor
// candidates (D-04). For an acceptable pick Reason is empty.
type CandidateView struct {
	Provider   string
	ReleaseKey string
	Title      string
	SizeBytes  int64
	Score      float64
	Reason     string
}

// Result is the assembled outcome of a manual SearchIssue.
type Result struct {
	Candidates  []CandidateView
	Acceptable  bool
	FloorReason string
}

// TimelineEvent is the domain view of one per-issue timeline event (OBS-01).
type TimelineEvent struct {
	ID         int64
	IssueID    int64
	Type       string
	OccurredAt string
	Detail     string
}

// candidateViewFromScored maps a ranked survivor to a CandidateView (no reason — it
// passed the filter and floor).
func candidateViewFromScored(sc ScoredCandidate) CandidateView {
	return CandidateView{
		Provider:   sc.Candidate.Provider,
		ReleaseKey: sc.Candidate.ReleaseKey,
		Title:      sc.Candidate.Title,
		SizeBytes:  sc.Candidate.SizeBytes,
		Score:      sc.Score,
	}
}

// candidateViewFromRejected maps a rejected candidate to a CandidateView carrying the
// reject reason (D-04 transparency).
func candidateViewFromRejected(fr FilterResult) CandidateView {
	return CandidateView{
		Provider:   fr.Candidate.Provider,
		ReleaseKey: fr.Candidate.ReleaseKey,
		Title:      fr.Candidate.Title,
		SizeBytes:  fr.Candidate.SizeBytes,
		Reason:     string(fr.Reason),
	}
}

// findCandidate returns the indexer.Candidate matching provider+releaseKey from a list,
// or false if absent.
func findCandidate(cands []indexer.Candidate, provider, releaseKey string) (indexer.Candidate, bool) {
	for _, c := range cands {
		if c.Provider == provider && c.ReleaseKey == releaseKey {
			return c, true
		}
	}
	return indexer.Candidate{}, false
}
