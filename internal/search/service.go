package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/vizvim/omnibus/internal/indexer"
	"github.com/vizvim/omnibus/internal/repository"
)

// keyEnableDDL is the user_config key the DDL enable toggle is persisted under (05-07,
// owned by the ddlconfig service). The search/grab path reads it to gate the built-in
// GetComics fallback. It is duplicated here (a single string) rather than importing the
// repository's unexported key, matching the layer's read-through-the-repo convention.
const keyEnableDDL = "enable_ddl"

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

// indexerKindFor is the inverse of downloadKindFor: it maps a download-provider kind
// (as stored on a downloads row) back to the indexer kind a candidate carries in its
// Provider field. The dead-download keys (D-12) are recorded with the download kind, but
// the BlacklistSet is matched against candidate.Provider (the indexer kind), so the dead
// keys must be translated before they go into the set.
func indexerKindFor(downloadKind string) string {
	if downloadKind == "sabnzbd" {
		return indexer.NewznabKind
	}
	return downloadKind
}

// Deps are the search Service's injected collaborators.
type Deps struct {
	Gateway           IndexerGateway
	Repos             *repository.Repositories
	DownloadProviders map[string]Submitter
	Logger            *slog.Logger
	AttemptCap        int
	// DownloadAttemptCap bounds the attempt-capped replacement loop on the SEPARATE
	// download_attempts counter (D-13/D-14). Zero falls back to AttemptCap.
	DownloadAttemptCap int
	// AutoSearchBatch bounds how many Wanted issues one auto-search sweep tick enqueues
	// (D-08). Zero falls back to defaultAutoSearchBatch.
	AutoSearchBatch int
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
	gateway            IndexerGateway
	repos              *repository.Repositories
	logger             *slog.Logger
	filterOpts         FilterOpts
	scoreOpts          ScoreOpts
	floor              float64
	attemptCap         int
	downloadAttemptCap int
	autoSearchBatch    int
	enqueuer           Enqueuer
}

// defaultAutoSearchBatch bounds an auto-search sweep tick when AutoSearchBatch is unset.
const defaultAutoSearchBatch = 20

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
	batch := d.AutoSearchBatch
	if batch <= 0 {
		batch = defaultAutoSearchBatch
	}
	dlCap := d.DownloadAttemptCap
	if dlCap <= 0 {
		dlCap = d.AttemptCap
	}
	grabber := NewGrabber(GrabDeps{
		Providers:  d.DownloadProviders,
		Repos:      d.Repos,
		Logger:     logger,
		AttemptCap: d.AttemptCap,
	})
	return &Service{
		Grabber:            grabber,
		gateway:            d.Gateway,
		repos:              d.Repos,
		logger:             logger,
		filterOpts:         fopts,
		scoreOpts:          sopts,
		floor:              floor,
		attemptCap:         d.AttemptCap,
		downloadAttemptCap: dlCap,
		autoSearchBatch:    batch,
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

	bl, err := s.blacklistFor(ctx, issue.ID)
	if err != nil {
		return Result{}, err
	}

	result, _, err := s.gatherWithDDLFallback(ctx, issue, bl)
	if err != nil {
		return Result{}, err
	}

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

	bl, err := s.blacklistFor(ctx, issue.ID)
	if err != nil {
		return DownloadResult{}, err
	}

	// The candidate set the grab is validated against includes the built-in GetComics
	// candidates ONLY when enable_ddl is true (gatherWithDDLFallback enforces the
	// fallback rule). When DDL is off, a getcomics release_key has no matching candidate
	// and is rejected below by the cross-issue guard.
	_, cands, err := s.gatherWithDDLFallback(ctx, issue, bl)
	if err != nil {
		return DownloadResult{}, err
	}

	cand, ok := findCandidate(cands, provider, releaseKey)
	if !ok {
		// The release was not produced for this issue — reject (cross-issue guard, T-4-02).
		return DownloadResult{}, ErrCrossIssueGrab
	}

	// candidate-selected event (D-04 timeline) before the grab handoff.
	if werr := s.writeEvent(ctx, issueID, "candidate-selected", map[string]string{
		"provider": provider, "release_key": releaseKey, "title": cand.Title,
	}); werr != nil {
		return DownloadResult{}, werr
	}

	downloadKind := downloadKindFor(provider)
	res, gerr := s.Grab(ctx, issueID, downloadKind, releaseKey, cand)
	if gerr != nil {
		return DownloadResult{}, gerr
	}

	s.maybeEnqueueDDLFetch(ctx, downloadKind, res)
	return res, nil
}

// maybeEnqueueDDLFetch enqueues the serialized DDLFetch job (D-03) for a successful
// getcomics grab, and is a no-op for any other download kind. It is the single shared
// chokepoint every grab path routes through (manual SelectCandidate, auto-grab
// runSearchIssue, RSS processRSSIssue) so EVERY getcomics grab — not just the manual one
// — gets fetched and post-processed (CR-01). It MUST be called after a successful Grab.
//
// A GetComics DDL grab has no download-client poll loop (SAB's CDH model does not apply):
// the DDLFetch job resolves a mirror + streams the file, then feeds it into the same
// Completed->post-process path as a SAB download. SAB grabs are tracked by the periodic
// poll loop instead, so no DDL fetch is enqueued for them.
func (s *Service) maybeEnqueueDDLFetch(ctx context.Context, downloadKind string, res DownloadResult) {
	if downloadKind != "getcomics" || s.enqueuer == nil {
		return
	}
	if eerr := s.enqueuer.EnqueueDDLFetch(ctx, res.DownloadID); eerr != nil {
		// Non-fatal: the grab succeeded; a failed enqueue is logged and the download row
		// remains Queued for a later reactive enqueue. Surfacing it would falsely fail the grab.
		s.logger.Warn("enqueue ddl fetch failed (non-fatal)",
			slog.Int64("download_id", res.DownloadID), slog.Any("error", eerr))
	}
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

// gatherWithDDLFallback runs the two-stage candidate gather + pipeline that enforces the
// Mylar-classic fallback rule (05-07): Newznab indexers are always preferred; the built-in
// GetComics DDL source is consulted ONLY when enable_ddl is true AND no acceptable Newznab
// candidate was found. It returns the final PipelineResult and the full candidate set the
// result was computed from (so SelectCandidate can validate a grab against it). It is the
// single selection path shared by manual search (SearchIssue) and manual grab
// (SelectCandidate), so both honor the identical fallback rule.
func (s *Service) gatherWithDDLFallback(ctx context.Context, issue repository.Issue, bl BlacklistSet) (PipelineResult, []indexer.Candidate, error) {
	target := IssueMatch{Sort: issue.IssueNumberSort, Qual: issue.IssueNumberQual.String}

	// Stage 1: gather + pipeline the Newznab candidates (indexer rows are newznab-only).
	nzbCands, err := s.gatherNewznabCandidates(ctx, issue)
	if err != nil {
		return PipelineResult{}, nil, err
	}
	result := Pipeline(nzbCands, target, bl, s.filterOpts, s.scoreOpts, s.floor)
	if result.Acceptable {
		// An acceptable Newznab candidate exists — GetComics is NOT consulted (fallback only).
		return result, nzbCands, nil
	}

	// Stage 2: no acceptable Newznab candidate. Consult the built-in GetComics source ONLY
	// when enable_ddl is true, union its candidates with the Newznab set, and re-run the
	// pipeline so an acceptable GetComics pick (always last) can be selected/grabbed.
	enabled, err := s.ddlEnabled(ctx)
	if err != nil {
		return PipelineResult{}, nil, err
	}
	if !enabled {
		return result, nzbCands, nil
	}

	ddlCands, err := s.gatherGetComicsCandidates(ctx, issue)
	if err != nil {
		return PipelineResult{}, nil, err
	}
	if len(ddlCands) == 0 {
		return result, nzbCands, nil
	}

	union := unionCandidates(nzbCands, ddlCands)
	result = Pipeline(union, target, bl, s.filterOpts, s.scoreOpts, s.floor)
	return result, union, nil
}

// ddlEnabled reports whether the built-in GetComics DDL fallback is turned on, reading the
// enable_ddl user_config key. A missing key (never written) is treated as false — the
// default-OFF posture that matches the ddlconfig service (DDL is opt-in).
func (s *Service) ddlEnabled(ctx context.Context) (bool, error) {
	v, err := s.repos.UserConfig.Get(ctx, keyEnableDDL)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("read enable_ddl: %w", err)
	}
	return v == "true", nil
}

// gatherNewznabCandidates loads the enabled (newznab) indexers, builds their providers, and
// searches them via the paced gateway. It composes mylar-style queries from the issue's
// series name and padded issue-number variants (issue_type aware), runs each query, and
// unions the results deduped by Candidate.ReleaseKey (first occurrence wins, order preserved).
func (s *Service) gatherNewznabCandidates(ctx context.Context, issue repository.Issue) ([]indexer.Candidate, error) {
	rows, err := s.repos.Indexers.ListEnabled(ctx)
	if err != nil {
		return nil, fmt.Errorf("list enabled indexers: %w", err)
	}
	providers := buildIndexerProviders(rows)
	if len(providers) == 0 {
		return nil, nil
	}
	return s.searchProviders(ctx, issue, providers)
}

// gatherGetComicsCandidates searches the built-in GetComics source for the issue. The base
// URL is the static built-in defaultGetComicsBaseURL constant (NewGetComicsProvider("")),
// NOT a user-editable indexer-row value — GetComics is config-gated built-in infrastructure
// (05-07), not an indexer. Candidates are gathered through the same paced gateway as Newznab.
func (s *Service) gatherGetComicsCandidates(ctx context.Context, issue repository.Issue) ([]indexer.Candidate, error) {
	providers := []indexer.IndexerProvider{indexer.NewGetComicsProvider("")}
	return s.searchProviders(ctx, issue, providers)
}

// searchProviders composes the mylar-style queries for the issue and runs them against the
// supplied providers via the paced gateway, unioning the results deduped by ReleaseKey
// (first occurrence wins, order preserved). It is the shared gather body for both the
// Newznab and built-in GetComics sources.
func (s *Service) searchProviders(ctx context.Context, issue repository.Issue, providers []indexer.IndexerProvider) ([]indexer.Candidate, error) {
	ser, err := s.repos.Series.GetByID(ctx, issue.SeriesID)
	if err != nil {
		return nil, fmt.Errorf("load series %d: %w", issue.SeriesID, err)
	}
	queries := buildQueries(ser.Name, issue.IssueNumberRaw, issue.IssueType)

	var union []indexer.Candidate
	seen := make(map[string]struct{})
	for _, q := range queries {
		cands, err := s.gateway.Search(ctx, providers, q)
		if err != nil {
			return nil, fmt.Errorf("indexer search: %w", err)
		}
		for _, c := range cands {
			if _, dup := seen[c.ReleaseKey]; dup {
				continue
			}
			seen[c.ReleaseKey] = struct{}{}
			union = append(union, c)
		}
	}
	return union, nil
}

// unionCandidates appends ddl candidates after the nzb candidates, deduped by ReleaseKey
// (the Newznab survivors keep precedence on a key collision). GetComics is always last so
// the structural fallback ordering holds even after the union.
func unionCandidates(nzb, ddl []indexer.Candidate) []indexer.Candidate {
	out := make([]indexer.Candidate, 0, len(nzb)+len(ddl))
	seen := make(map[string]struct{}, len(nzb)+len(ddl))
	for _, c := range nzb {
		if _, dup := seen[c.ReleaseKey]; dup {
			continue
		}
		seen[c.ReleaseKey] = struct{}{}
		out = append(out, c)
	}
	for _, c := range ddl {
		if _, dup := seen[c.ReleaseKey]; dup {
			continue
		}
		seen[c.ReleaseKey] = struct{}{}
		out = append(out, c)
	}
	return out
}

// buildIndexerProviders constructs an IndexerProvider per enabled indexer row. Indexer rows
// can no longer be getcomics (05-07), so this builds Newznab providers only; GetComics is a
// config-gated built-in source consulted via gatherGetComicsCandidates, not an indexer row.
func buildIndexerProviders(rows []repository.IndexerRow) []indexer.IndexerProvider {
	out := make([]indexer.IndexerProvider, 0, len(rows))
	for _, row := range rows {
		if row.Kind == indexer.NewznabKind {
			out = append(out, indexer.NewNewznabProvider(row.BaseURL, row.APIKey, row.Categories))
		}
	}
	return out
}

// blacklistFor builds the issue's BlacklistSet by UNIONing two sources (D-11/D-12):
//
//  1. user-blacklisted releases for the issue (ListForIssue, D-11), and
//  2. dead download keys — releases already Failed/Blacklisted for the issue in the
//     downloads table (ListDeadReleaseKeys, D-12) — translated from their stored
//     download-provider kind back to the candidate indexer kind so they match the
//     candidate.Provider keyspace the hard filter compares against.
//
// The dead-key dedup is the mandatory loop-prevention (RESEARCH Pitfall 3): it excludes a
// release that already failed WITHOUT requiring a user blacklist row, so a replacement
// search can never re-pick a dead release. The hard filter accepts a nil/empty set.
func (s *Service) blacklistFor(ctx context.Context, issueID int64) (BlacklistSet, error) {
	pairs := make([][2]string, 0)

	userKeys, err := s.repos.Blacklist.ListForIssue(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("list blacklist for issue %d: %w", issueID, err)
	}
	for _, k := range userKeys {
		pairs = append(pairs, [2]string{k.Provider, k.ReleaseKey})
	}

	deadKeys, err := s.repos.Downloads.ListDeadReleaseKeys(ctx, issueID)
	if err != nil {
		return nil, fmt.Errorf("list dead release keys for issue %d: %w", issueID, err)
	}
	for _, k := range deadKeys {
		// Dead keys are recorded with the download-provider kind; the BlacklistSet is keyed
		// on the indexer kind a candidate carries, so translate before unioning (D-12).
		pairs = append(pairs, [2]string{indexerKindFor(k.Provider), k.ReleaseKey})
	}

	return NewBlacklistSet(pairs...), nil
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
