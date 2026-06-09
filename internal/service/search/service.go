package search

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/vizvim/omnibus/internal/provider/download"
	"github.com/vizvim/omnibus/internal/provider/indexer"
	"github.com/vizvim/omnibus/internal/repository"
)

// IndexerGateway is the search surface the service depends on (satisfied by
// *indexer.Gateway). Declaring it here keeps the service testable against a fake.
type IndexerGateway interface {
	Search(ctx context.Context, providers []indexer.IndexerProvider, query string) ([]indexer.Candidate, error)
	Feed(ctx context.Context, providers []indexer.IndexerProvider) ([]indexer.Candidate, error)
}

// downloadKindFor maps an indexer kind to the download-provider kind that grabs its
// candidates: Newznab → SABnzbd (NZB), GetComics → GetComics DDL.
func downloadKindFor(indexerKind string) string {
	if indexerKind == indexer.NewznabKind {
		return "sabnzbd"
	}
	return "getcomics"
}

// Deps are the search Service's injected collaborators.
type Deps struct {
	Gateway           IndexerGateway
	Repos             *repository.Repositories
	DownloadProviders map[string]download.DownloadProvider
	Logger            *slog.Logger
	AttemptCap        int
	// Pipeline opts; zero values fall back to the package defaults.
	FilterOpts FilterOpts
	ScoreOpts  ScoreOpts
	Floor      float64
}

// Service implements SearchService domain logic: manual search, candidate selection,
// and the per-issue timeline. It composes the indexer gateway (Plan 02), the
// filter/score pipeline (Plan 03), and the grab handoff (Plan 04, via the embedded
// Grabber). It depends only on repository interfaces + the gateway + download providers.
type Service struct {
	*Grabber
	gateway    IndexerGateway
	repos      *repository.Repositories
	logger     *slog.Logger
	filterOpts FilterOpts
	scoreOpts  ScoreOpts
	floor      float64
}

// New constructs a search Service.
func New(d Deps) *Service {
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	fopts := d.FilterOpts
	if fopts.MinSizeBytes == 0 && fopts.MaxSizeBytes == 0 && len(fopts.AllowedFormats) == 0 {
		fopts = DefaultFilterOpts()
	}
	sopts := d.ScoreOpts
	if sopts.QualityWeights == nil {
		sopts = DefaultScoreOpts()
	}
	floor := d.Floor
	if floor == 0 {
		floor = DefaultAcceptanceFloor
	}
	grabber := NewGrabber(GrabDeps{
		Providers:  d.DownloadProviders,
		Repos:      d.Repos,
		Logger:     logger,
		AttemptCap: d.AttemptCap,
	})
	return &Service{
		Grabber:    grabber,
		gateway:    d.Gateway,
		repos:      d.Repos,
		logger:     logger,
		filterOpts: fopts,
		scoreOpts:  sopts,
		floor:      floor,
	}
}

// SearchIssue runs the shared pipeline for a Wanted issue: gather candidates across the
// enabled indexers, filter/score/floor them, write a searched timeline event with the
// reasons (D-04), and return the ranked list. Manual search ignores cool-off (D-09).
func (s *Service) SearchIssue(ctx context.Context, issueID int64) (Result, error) {
	issue, err := s.repos.Issue.GetByID(ctx, issueID)
	if err != nil {
		return Result{}, fmt.Errorf("load issue %d: %w", issueID, err)
	}

	cands, err := s.gatherCandidates(ctx, issue)
	if err != nil {
		return Result{}, err
	}

	target := IssueMatch{Sort: issue.IssueNumberSort, Qual: issue.IssueNumberQual.String}
	result := Pipeline(cands, target, s.blacklistFor(issue.ID), s.filterOpts, s.scoreOpts, s.floor)

	if err := s.writeSearchedEvent(ctx, issueID, result); err != nil {
		return Result{}, err
	}

	return searchResultFromPipeline(result), nil
}

// SelectCandidate validates that the release_key was a candidate for THIS issue, writes a
// candidate-selected event, then grabs it (submit + Snatched + snatched event).
func (s *Service) SelectCandidate(ctx context.Context, issueID int64, provider, releaseKey string) (DownloadResult, error) {
	issue, err := s.repos.Issue.GetByID(ctx, issueID)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("load issue %d: %w", issueID, err)
	}

	cands, err := s.gatherCandidates(ctx, issue)
	if err != nil {
		return DownloadResult{}, err
	}

	cand, ok := findCandidate(cands, provider, releaseKey)
	if !ok {
		// The release was not produced for this issue — reject (cross-issue guard, T-4-02).
		return DownloadResult{}, ErrCrossIssueGrab
	}

	// candidate-selected event (D-04 timeline) before the grab handoff.
	if err := s.writeEvent(ctx, issueID, "candidate-selected", map[string]string{
		"provider": provider, "release_key": releaseKey, "title": cand.Title,
	}); err != nil {
		return DownloadResult{}, err
	}

	return s.Grab(ctx, issueID, downloadKindFor(provider), releaseKey, cand)
}

// GetTimeline returns the per-issue events in occurred_at order (OBS-01).
func (s *Service) GetTimeline(ctx context.Context, issueID int64) ([]TimelineEvent, error) {
	rows, err := s.repos.IssueEvents.ListByIssue(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("list issue events: %w", err)
	}
	out := make([]TimelineEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, TimelineEvent{
			ID:         r.ID,
			IssueID:    r.IssueID,
			Type:       r.EventType,
			OccurredAt: r.OccurredAt,
			Detail:     r.PayloadJSON,
		})
	}
	return out, nil
}

// gatherCandidates loads the enabled indexers, builds their providers, and searches them
// via the paced gateway. The query is the issue's series name + issue number.
func (s *Service) gatherCandidates(ctx context.Context, issue repository.Issue) ([]indexer.Candidate, error) {
	rows, err := s.repos.Indexers.ListEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("list enabled indexers: %w", err)
	}
	providers := buildIndexerProviders(rows)
	if len(providers) == 0 {
		return nil, nil
	}
	query := searchQuery(issue)
	cands, err := s.gateway.Search(ctx, providers, query)
	if err != nil {
		return nil, fmt.Errorf("indexer search: %w", err)
	}
	return cands, nil
}

// searchQuery builds the indexer query from an issue (issue number is the discriminator;
// the series name would be joined in once the series is loaded — kept minimal here).
func searchQuery(issue repository.Issue) string {
	return issue.IssueNumberRaw
}

// buildIndexerProviders constructs an IndexerProvider per enabled indexer row.
func buildIndexerProviders(rows []repository.IndexerRow) []indexer.IndexerProvider {
	out := make([]indexer.IndexerProvider, 0, len(rows))
	for _, row := range rows {
		switch row.Kind {
		case indexer.NewznabKind:
			out = append(out, indexer.NewNewznabProvider(row.BaseURL, row.APIKey, row.Categories))
		case indexer.GetComicsKind:
			out = append(out, indexer.NewGetComicsProvider(row.BaseURL))
		}
	}
	return out
}

// blacklistFor loads the issue's blacklist set. A dedicated blacklist repository lands in
// Phase 5; for this phase the set is empty (no candidates are blacklisted yet). The hard
// filter accepts a nil set.
func (s *Service) blacklistFor(_ int64) BlacklistSet {
	return nil
}

// writeSearchedEvent records a searched event carrying the floor decision + reject reasons.
func (s *Service) writeSearchedEvent(ctx context.Context, issueID int64, r PipelineResult) error {
	reasons := make([]map[string]string, 0, len(r.Rejections))
	for _, rej := range r.Rejections {
		reasons = append(reasons, map[string]string{
			"release_key": rej.Candidate.ReleaseKey,
			"reason":      string(rej.Reason),
		})
	}
	payload := map[string]any{
		"acceptable":   r.Acceptable,
		"floor_reason": r.FloorReason,
		"ranked_count": len(r.Ranked),
		"rejected":     reasons,
	}
	return s.writeEvent(ctx, issueID, "searched", payload)
}

// writeEvent marshals payload and appends a timeline event.
func (s *Service) writeEvent(ctx context.Context, issueID int64, eventType string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s event: %w", eventType, err)
	}
	if _, err := s.repos.IssueEvents.Insert(ctx, repository.IssueEventInsert{
		IssueID:     issueID,
		EventType:   eventType,
		PayloadJSON: string(raw),
		OccurredAt:  s.now().UTC().Format("2006-01-02T15:04:05Z07:00"),
	}); err != nil {
		return fmt.Errorf("write %s event: %w", eventType, err)
	}
	return nil
}

// searchResultFromPipeline maps a PipelineResult to the transport-facing Result.
// Ranked survivors come first (with scores), then rejected candidates (with reasons) so
// the manual UI can show the full picture (D-04).
func searchResultFromPipeline(r PipelineResult) Result {
	views := make([]CandidateView, 0, len(r.Ranked)+len(r.Rejections))
	for _, sc := range r.Ranked {
		views = append(views, candidateViewFromScored(sc))
	}
	for _, rej := range r.Rejections {
		views = append(views, candidateViewFromRejected(rej))
	}
	return Result{
		Candidates:  views,
		Acceptable:  r.Acceptable,
		FloorReason: r.FloorReason,
	}
}
