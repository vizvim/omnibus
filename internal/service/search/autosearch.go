package search

import (
	"context"
	"fmt"
	"log/slog"
)

// SearchEnqueuer enqueues a one-off auto-search job per issue. It is satisfied by the
// jobs client (*jobs.Client.EnqueueSearchIssue). Declaring it here keeps the search
// service free of any import of internal/jobs (inverted dependency, mirroring the
// series<->jobs seam).
type SearchEnqueuer interface {
	EnqueueSearchIssue(ctx context.Context, issueID int64) error
}

// RunAutoSearchSweep is the scheduled auto-search tick (the body the River periodic
// AutoSearchSweep job invokes). It loads a bounded batch of eligible Wanted issues
// (fewest-attempts-first, capped — D-08) and enqueues one one-off SearchIssue job per
// issue via the enqueuer. It does NOT search inline: River's bounded worker pool absorbs
// the fan-out (D-10). Leftover eligible issues roll to the next tick.
func (s *Service) RunAutoSearchSweep(ctx context.Context, enqueuer SearchEnqueuer) error {
	cap32 := clampInt32(int64(s.attemptCap))
	batch32 := clampInt32(int64(s.autoSearchBatch))
	issues, err := s.repos.Issue.ListWantedForAutoSearch(ctx, cap32, batch32)
	if err != nil {
		return fmt.Errorf("list wanted for auto-search: %w", err)
	}

	enqueued := 0
	for _, iss := range issues {
		if err := enqueuer.EnqueueSearchIssue(ctx, iss.ID); err != nil {
			s.logger.Warn("auto-search enqueue failed",
				slog.Int64("issue_id", iss.ID), slog.Any("error", err))
			continue
		}
		enqueued++
	}
	s.logger.Info("auto-search sweep enqueued batch",
		slog.Int("enqueued", enqueued), slog.Int("eligible", len(issues)))
	return nil
}

// RunSearchIssue is the auto-search worker body for a single issue. It runs the SAME
// shared pipeline as the manual path, writes a searched event with the reasons (D-04),
// and then diverges from manual: if the top candidate clears the acceptance floor (D-02)
// it auto-grabs it (Plan 04 Grab → Snatched + snatched event); if nothing was acceptable
// it increments search_attempts (D-09 backoff/cool-off). An already-non-Wanted issue is a
// no-op (it may have been grabbed/skipped between enqueue and run).
func (s *Service) RunSearchIssue(ctx context.Context, issueID int64) error {
	issue, err := s.repos.Issue.GetByID(ctx, issueID)
	if err != nil {
		return fmt.Errorf("load issue %d: %w", issueID, err)
	}

	cands, err := s.gatherCandidates(ctx, issue)
	if err != nil {
		return err
	}

	target := IssueMatch{Sort: issue.IssueNumberSort, Qual: issue.IssueNumberQual.String}
	result := Pipeline(cands, target, s.blacklistFor(issue.ID), s.filterOpts, s.scoreOpts, s.floor)

	if err := s.writeSearchedEvent(ctx, issueID, result); err != nil {
		return err
	}

	if !result.Acceptable || result.Pick == nil {
		// No clearing candidate — record the attempt so backoff/cool-off applies (D-09).
		if err := s.repos.Issue.IncrementSearchAttempts(ctx, issueID); err != nil {
			return fmt.Errorf("increment search_attempts for issue %d: %w", issueID, err)
		}
		s.logger.Info("auto-search no acceptable candidate",
			slog.Int64("issue_id", issueID), slog.String("reason", result.FloorReason))
		return nil
	}

	// A candidate cleared the floor — auto-grab it through the shared Grab path (Plan 04).
	pick := result.Pick.Candidate
	if _, err := s.Grab(ctx, issueID, downloadKindFor(pick.Provider), pick.ReleaseKey, pick); err != nil {
		return fmt.Errorf("auto-grab issue %d: %w", issueID, err)
	}
	return nil
}
