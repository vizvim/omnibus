package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/vizvim/omnibus/internal/repository"
)

// errNoEnqueuer is returned when the auto-search sweep runs before SetEnqueuer wired the
// jobs client (a misconfiguration — the sweep cannot fan out without it).
var errNoEnqueuer = errors.New("auto-search sweep: enqueuer not configured")

// Enqueuer enqueues a one-off auto-search job per issue. It is satisfied by the
// jobs client (*jobs.Client.EnqueueSearchIssue). Declaring it here keeps the search
// service free of any import of internal/jobs (inverted dependency, mirroring the
// series<->jobs seam).
type Enqueuer interface {
	EnqueueSearchIssue(ctx context.Context, issueID int64) error
	// EnqueueReplacementSearch durably enqueues an attempt-capped replacement search for an
	// issue whose download failed or was blacklisted (D-12/DL-05). Satisfied by the jobs
	// client (*jobs.Client.EnqueueReplacementSearch).
	EnqueueReplacementSearch(ctx context.Context, issueID int64) error
}

// SetEnqueuer wires the job enqueuer after construction. This resolves the service<->jobs
// construction cycle: the jobs client references the service (as the AutoSearchRunner)
// and the service needs the client (as its Enqueuer), so the service is built first
// with a nil enqueuer and the client is injected here — mirroring series.Service.
func (s *Service) SetEnqueuer(e Enqueuer) { s.enqueuer = e }

// RunAutoSearchSweep is the scheduled auto-search tick (the body the River periodic
// AutoSearchSweep job invokes). It loads a bounded batch of eligible Wanted issues
// (fewest-attempts-first, capped — D-08) and enqueues one one-off SearchIssue job per
// issue via the enqueuer it holds. It does NOT search inline: River's bounded worker pool
// absorbs the fan-out (D-10). Leftover eligible issues roll to the next tick.
func (s *Service) RunAutoSearchSweep(ctx context.Context) error {
	if s.enqueuer == nil {
		return errNoEnqueuer
	}
	cap32 := clampInt32(int64(s.attemptCap))
	batch32 := clampInt32(int64(s.autoSearchBatch))
	issues, err := s.repos.Issue.ListWantedForAutoSearch(ctx, cap32, batch32)
	if err != nil {
		return fmt.Errorf("list wanted for auto-search: %w", err)
	}

	enqueued := 0
	for _, iss := range issues {
		if err := s.enqueuer.EnqueueSearchIssue(ctx, iss.ID); err != nil {
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

// AutoSearchOutcome is the result of an inline search-and-auto-grab pass: whether a
// release cleared the floor and was grabbed, the grabbed title, and (when nothing was
// grabbed) the human floor reason. The manual on-demand path returns this to the caller
// so the UI can show "grabbed X" vs "nothing acceptable"; the River worker discards it.
type AutoSearchOutcome struct {
	Grabbed     bool
	Title       string
	FloorReason string
}

// RunSearchIssue is the auto-search worker body for a single issue (jobs-facing,
// error-only so internal/jobs stays free of any search-package type). It delegates to the
// shared runSearchIssue core and discards the outcome.
func (s *Service) RunSearchIssue(ctx context.Context, issueID int64) error {
	_, err := s.runSearchIssue(ctx, issueID)
	return err
}

// SearchAndGrab runs the same inline search-and-auto-grab pass as the worker but returns
// the AutoSearchOutcome. It backs the on-demand manual "auto-grab & download" path
// (TriggerAutoSearch), which bypasses the River queue and runs synchronously.
func (s *Service) SearchAndGrab(ctx context.Context, issueID int64) (AutoSearchOutcome, error) {
	return s.runSearchIssue(ctx, issueID)
}

// RunReplacement is the reactive replacement-search entrypoint (the body the
// ReplacementSearch River job delegates to). It is triggered when a download Failed (poll
// reap, 05-01) or a post-process corruption (05-03) leaves an issue stranded. It mirrors
// runSearchIssue but on the SEPARATE download_attempts counter (D-13/D-14):
//
//   - If download_attempts has reached the cap, it enters cool-off: it does NOT re-search,
//     leaving the issue parked until a manual retry resets the counter (D-14).
//   - Otherwise it runs the shared pipeline, which now dedups against the dead-download
//     keys (D-12), so a replacement can never re-pick a release that already failed.
//   - If the pipeline grabs a clearing replacement, the cycle succeeds. If nothing was
//     acceptable (every survivor was excluded or below floor), it increments
//     download_attempts so the loop is bounded and eventually cools off.
func (s *Service) RunReplacement(ctx context.Context, issueID int64) error {
	issue, err := s.repos.Issue.GetByID(ctx, issueID)
	if err != nil {
		return fmt.Errorf("load issue %d: %w", issueID, err)
	}

	if int(issue.DownloadAttempts) >= s.downloadAttemptCap {
		// Cool-off: the replacement loop is exhausted; wait for a manual retry (D-14).
		s.logger.Info("replacement search in cool-off (download attempt cap reached)",
			slog.Int64("issue_id", issueID),
			slog.Int64("download_attempts", issue.DownloadAttempts),
			slog.Int("cap", s.downloadAttemptCap))
		return nil
	}

	outcome, err := s.runSearchIssue(ctx, issueID)
	if err != nil {
		return err
	}
	if !outcome.Grabbed {
		// No acceptable replacement this cycle — record the attempt on the SEPARATE
		// download_attempts counter so the replacement loop is bounded (D-13/D-14). Note
		// runSearchIssue already bumped search_attempts; the replacement loop additionally
		// counts on download_attempts so the two caps stay distinct.
		if ierr := s.repos.Issue.IncrementDownloadAttempts(ctx, issueID); ierr != nil {
			return fmt.Errorf("increment download_attempts for issue %d: %w", issueID, ierr)
		}
		s.logger.Info("replacement search found no acceptable release",
			slog.Int64("issue_id", issueID), slog.String("reason", outcome.FloorReason))
	}
	return nil
}

// BlacklistRelease blacklists a specific release for an issue (D-11) and enqueues a
// replacement search so omnibus auto-replaces it (DL-05 -> auto-replace). The blacklist
// add is per-issue (series_id NULL). The replacement is enqueued through the reactive
// enqueuer seam; a nil enqueuer leaves the blacklist persisted but skips the fan-out.
func (s *Service) BlacklistRelease(ctx context.Context, issueID int64, provider, releaseKey string) error {
	nowISO := s.now().UTC().Format("2006-01-02T15:04:05Z07:00")
	if err := s.repos.Blacklist.Add(ctx, repository.BlacklistAdd{
		IssueID: issueID, Provider: provider, ReleaseKey: releaseKey, Reason: "user blacklist",
	}, nowISO); err != nil {
		return fmt.Errorf("blacklist release %s/%s for issue %d: %w", provider, releaseKey, issueID, err)
	}
	if s.enqueuer != nil {
		if err := s.enqueuer.EnqueueReplacementSearch(ctx, issueID); err != nil {
			return fmt.Errorf("enqueue replacement search for issue %d: %w", issueID, err)
		}
	}
	s.logger.Info("release blacklisted; replacement enqueued",
		slog.Int64("issue_id", issueID), slog.String("provider", provider), slog.String("release_key", releaseKey))
	return nil
}

// RetryDownload is the manual-retry entrypoint (DL-07): it resets the download_attempts
// cool-off (D-14 — the user's explicit retry is the intended cap reset) and re-runs the
// search pipeline immediately (inline, bypassing the River queue), so a stranded issue is
// re-searched at once. It returns the outcome so the caller can report grabbed-vs-not.
func (s *Service) RetryDownload(ctx context.Context, issueID int64) (AutoSearchOutcome, error) {
	if err := s.repos.Issue.ResetDownloadAttempts(ctx, issueID); err != nil {
		return AutoSearchOutcome{}, fmt.Errorf("reset download_attempts for issue %d: %w", issueID, err)
	}
	return s.runSearchIssue(ctx, issueID)
}

// runSearchIssue runs the SAME shared pipeline as the manual path, writes a searched event
// with the reasons (D-04), and then diverges from a plain manual search: if the top
// candidate clears the acceptance floor (D-02) it auto-grabs it (Grab → Snatched +
// snatched event); if nothing was acceptable it increments search_attempts (D-09
// backoff/cool-off). It returns the outcome for callers that surface it.
func (s *Service) runSearchIssue(ctx context.Context, issueID int64) (AutoSearchOutcome, error) {
	issue, err := s.repos.Issue.GetByID(ctx, issueID)
	if err != nil {
		return AutoSearchOutcome{}, fmt.Errorf("load issue %d: %w", issueID, err)
	}

	cands, err := s.gatherCandidates(ctx, issue)
	if err != nil {
		return AutoSearchOutcome{}, err
	}

	bl, err := s.blacklistFor(ctx, issue.ID)
	if err != nil {
		return AutoSearchOutcome{}, err
	}
	target := IssueMatch{Sort: issue.IssueNumberSort, Qual: issue.IssueNumberQual.String}
	result := Pipeline(cands, target, bl, s.filterOpts, s.scoreOpts, s.floor)

	if err := s.writeSearchedEvent(ctx, issueID, result); err != nil {
		return AutoSearchOutcome{}, err
	}

	if !result.Acceptable || result.Pick == nil {
		// No clearing candidate — record the attempt so backoff/cool-off applies (D-09).
		if err := s.repos.Issue.IncrementSearchAttempts(ctx, issueID); err != nil {
			return AutoSearchOutcome{}, fmt.Errorf("increment search_attempts for issue %d: %w", issueID, err)
		}
		s.logger.Info("auto-search no acceptable candidate",
			slog.Int64("issue_id", issueID), slog.String("reason", result.FloorReason))
		return AutoSearchOutcome{Grabbed: false, FloorReason: result.FloorReason}, nil
	}

	// A candidate cleared the floor — auto-grab it through the shared Grab path (Plan 04).
	pick := result.Pick.Candidate
	if _, err := s.Grab(ctx, issueID, downloadKindFor(pick.Provider), pick.ReleaseKey, pick); err != nil {
		return AutoSearchOutcome{}, fmt.Errorf("auto-grab issue %d: %w", issueID, err)
	}
	return AutoSearchOutcome{Grabbed: true, Title: pick.Title}, nil
}
