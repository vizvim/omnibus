package search

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/vizvim/omnibus/internal/indexer"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/series"
)

// RunRSSPoll is the scheduled RSS auto-grab tick (the body the River periodic RSSPoll
// job invokes). It polls each enabled RSS indexer's recent-uploads feed (Plan 02),
// matches feed items against currently-Wanted issues by normalized issue number
// (series.Normalize/Key), runs the SAME shared filter/score/floor pipeline (Plan 03) per
// matched issue, and auto-grabs a clearing candidate (Plan 04 Grab). It dedups on
// release_key: a candidate whose (provider, release_key) already has a downloads row for
// the issue is skipped (no re-grab, D-15). RSS reuses everything — it adds only the feed
// poll + the matcher; there is no parallel scoring/filter path here.
func (s *Service) RunRSSPoll(ctx context.Context) error {
	rows, err := s.repos.Indexers.ListEnabled(ctx)
	if err != nil {
		return fmt.Errorf("list enabled indexers: %w", err)
	}
	providers := buildRSSProviders(rows)
	if len(providers) == 0 {
		return nil
	}

	feedCands, err := s.gateway.Feed(ctx, providers)
	if err != nil {
		return fmt.Errorf("rss feed: %w", err)
	}
	if len(feedCands) == 0 {
		return nil
	}

	wanted, err := s.repos.Issue.ListWantedForMatch(ctx)
	if err != nil {
		return fmt.Errorf("list wanted for match: %w", err)
	}

	// Group feed candidates by the Wanted issue they match (normalized issue number).
	byIssue := matchFeedToIssues(feedCands, wanted)

	grabbed := 0
	for issueID, cands := range byIssue {
		did, err := s.processRSSIssue(ctx, issueID, cands)
		if err != nil {
			s.logger.Warn("rss issue processing failed",
				slog.Int64("issue_id", issueID), slog.Any("error", err))
			continue
		}
		if did {
			grabbed++
		}
	}
	s.logger.Info("rss poll complete",
		slog.Int("matched_issues", len(byIssue)), slog.Int("grabbed", grabbed))
	return nil
}

// processRSSIssue runs the shared pipeline for one Wanted issue's matched feed
// candidates, writes a searched event, and auto-grabs a floor-clearing pick unless its
// release_key is already deduped. It returns whether a grab occurred.
func (s *Service) processRSSIssue(ctx context.Context, issueID int64, cands []indexer.Candidate) (bool, error) {
	issue, err := s.repos.Issue.GetByID(ctx, issueID)
	if err != nil {
		return false, fmt.Errorf("load issue %d: %w", issueID, err)
	}

	bl, err := s.blacklistFor(ctx, issue.ID)
	if err != nil {
		return false, err
	}
	target := IssueMatch{Sort: issue.IssueNumberSort, Qual: issue.IssueNumberQual.String}
	result := Pipeline(cands, target, bl, s.filterOpts, s.scoreOpts, s.floor)

	if werr := s.writeSearchedEvent(ctx, issueID, result); werr != nil {
		return false, werr
	}

	if !result.Acceptable || result.Pick == nil {
		return false, nil
	}

	pick := result.Pick.Candidate

	// Dedup on release_key: skip if this release already has a downloads row for the issue
	// (no re-grab, D-15).
	seen, err := s.releaseAlreadyGrabbed(ctx, issueID, pick.Provider, pick.ReleaseKey)
	if err != nil {
		return false, err
	}
	if seen {
		s.logger.Debug("rss skip — release already grabbed",
			slog.Int64("issue_id", issueID), slog.String("release_key", pick.ReleaseKey))
		return false, nil
	}

	dlKind := downloadKindFor(pick.Provider)
	res, err := s.Grab(ctx, issueID, dlKind, pick.ReleaseKey, pick)
	if err != nil {
		return false, fmt.Errorf("rss auto-grab issue %d: %w", issueID, err)
	}
	// Defense-in-depth: RSS is Newznab-only today (buildRSSProviders excludes getcomics), so
	// this is a no-op now, but routing through the shared chokepoint keeps every grab path
	// uniform so a future getcomics-over-RSS path cannot strand in Queued (CR-01).
	s.maybeEnqueueDDLFetch(ctx, dlKind, res)
	return true, nil
}

// releaseAlreadyGrabbed reports whether a (provider, release_key) already has a downloads
// row for the issue — the RSS dedup guard (D-15).
func (s *Service) releaseAlreadyGrabbed(ctx context.Context, issueID int64, provider, releaseKey string) (bool, error) {
	dls, err := s.repos.Downloads.ListByIssue(ctx, issueID)
	if err != nil {
		return false, fmt.Errorf("list downloads for issue %d: %w", issueID, err)
	}
	for _, d := range dls {
		if d.Provider == provider && d.ReleaseKey == releaseKey {
			return true, nil
		}
	}
	return false, nil
}

// matchFeedToIssues groups feed candidates by the Wanted issue they match. A candidate
// matches an issue when its normalized issue number key equals the issue's. A candidate
// with no parseable issue number is ignored.
func matchFeedToIssues(cands []indexer.Candidate, wanted []repository.Issue) map[int64][]indexer.Candidate {
	// Index Wanted issues by their normalized key.
	byKey := make(map[string][]int64, len(wanted))
	for _, iss := range wanted {
		key := series.Key(iss.IssueNumberSort, iss.IssueNumberQual.String)
		byKey[key] = append(byKey[key], iss.ID)
	}

	out := make(map[int64][]indexer.Candidate)
	for _, c := range cands {
		if c.IssueNumberRaw == "" {
			continue
		}
		sort, qual := series.Normalize(c.IssueNumberRaw)
		key := series.Key(sort, qual)
		for _, issueID := range byKey[key] {
			out[issueID] = append(out[issueID], c)
		}
	}
	return out
}

// buildRSSProviders constructs an IndexerProvider per enabled indexer flagged use_for_rss.
// RSS is Newznab-only (05-07): GetComics has no per-user RSS indexer row, and the built-in
// GetComics DDL source is a targeted-search fallback (consulted per Wanted issue when no
// Newznab candidate is acceptable), not an RSS recent-uploads feed. So GetComics is
// excluded from RSS entirely — there is no enable_ddl gate here because there is no
// GetComics RSS path to gate.
func buildRSSProviders(rows []repository.IndexerRow) []indexer.IndexerProvider {
	out := make([]indexer.IndexerProvider, 0, len(rows))
	for _, row := range rows {
		if !row.UseForRSS {
			continue
		}
		if row.Kind == indexer.NewznabKind {
			out = append(out, indexer.NewNewznabProvider(row.BaseURL, row.APIKey, row.Categories))
		}
	}
	return out
}
