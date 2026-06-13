package postprocess

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vizvim/omnibus/internal/provider/download"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/service/renameconfig"
	"github.com/vizvim/omnibus/internal/service/series"
)

// ErrDownloadNotCompleted is returned when Process is asked to handle a download that has
// not reached the Completed download-status (a programming/sequencing error, not a user
// failure). The poll loop only enqueues post-process on the Completed transition.
var ErrDownloadNotCompleted = errors.New("download is not Completed")

// ErrNoStoragePath is returned when a Completed download has no captured storage path on
// its history (the client reported an empty path), so there is nothing to import.
var ErrNoStoragePath = errors.New("completed download has no storage path")

// RenameConfigGetter resolves the live renaming config so user toggles take effect
// without a restart. The renameconfig.Service satisfies it via Get. Declaring the narrow
// interface here keeps postprocess free of a hard service dependency.
type RenameConfigGetter interface {
	Get(ctx context.Context) (renameconfig.Config, error)
}

// ReplacementEnqueuer is the seam the corrupt-archive failure branch fans out through: a
// corrupt download routes into the same replacement path as a failed grab. The jobs client
// satisfies it via EnqueueReplacementSearch. A nil enqueuer leaves the replacement fan-out a
// no-op (the terminal Failed flip + history still happen).
type ReplacementEnqueuer interface {
	EnqueueReplacementSearch(ctx context.Context, issueID int64) error
}

// Deps are the orchestrator's injected collaborators, mirroring the downloadclient/tracking
// Deps shape (repos + logger + injectable clock).
type Deps struct {
	Repos *repository.Repositories
	// Providers maps a provider kind ("sabnzbd"/"getcomics") to its DownloadProvider; the
	// Tracker capability (RemoveFromHistory) is type-asserted per provider.
	Providers map[string]download.DownloadProvider
	// RenameConfig resolves the live renaming templates/toggles.
	RenameConfig RenameConfigGetter
	// LibraryPath is the root of the organized library; the import dst is asserted under it.
	LibraryPath string
	// AttemptCap bounds Snatched/Completed->Downloaded transitions (mirrors grab's cap).
	AttemptCap int
	Logger     *slog.Logger
	// Now is an injectable clock for deterministic timestamps in tests.
	Now func() time.Time
}

// Service composes the post-process units against a real completed download: it validates the
// archive, renders the folder+file templates from the live renameconfig, imports the file into
// the library, flips the issue to Downloaded via the state machine, writes downloaded+processed
// timeline events and a download_history row, and removes the item from the client. A corrupt
// archive routes into the shared replacement path.
type Service struct {
	repos       *repository.Repositories
	providers   map[string]download.DownloadProvider
	renameCfg   RenameConfigGetter
	importer    *Importer
	libraryPath string
	attemptCap  int
	logger      *slog.Logger
	now         func() time.Time
	enqueuer    ReplacementEnqueuer
}

// New constructs a post-process Service.
func New(d Deps) *Service {
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		repos:       d.Repos,
		providers:   d.Providers,
		renameCfg:   d.RenameConfig,
		importer:    NewImporter(),
		libraryPath: d.LibraryPath,
		attemptCap:  d.AttemptCap,
		logger:      logger,
		now:         now,
	}
}

// SetEnqueuer wires the replacement-search enqueuer after construction (mirrors the
// tracking/search service<->jobs cycle resolution). A nil enqueuer leaves the corrupt-archive
// replacement fan-out a no-op.
func (s *Service) SetEnqueuer(e ReplacementEnqueuer) { s.enqueuer = e }

// processedPayload is the issue_events.payload_json for a processed event (the import landed).
type processedPayload struct {
	Provider   string `json:"provider"`
	ReleaseKey string `json:"release_key"`
	Format     string `json:"format"`
	Path       string `json:"path"`
}

// downloadedPayload is the issue_events.payload_json for a downloaded event (the issue is
// now satisfied by a filed release).
type downloadedPayload struct {
	Provider   string `json:"provider"`
	ReleaseKey string `json:"release_key"`
}

// corruptPayload is the issue_events.payload_json for a failed (corrupt-archive) event.
type corruptPayload struct {
	Provider   string `json:"provider"`
	ReleaseKey string `json:"release_key"`
	Reason     string `json:"reason"`
}

// RunPostProcess is the runner-facing entrypoint the PostProcess River worker delegates to
// (jobs.PostProcessRunner). It is an alias for Process so internal/jobs depends on a narrow
// interface rather than the concrete service, mirroring search.RunReplacement.
func (s *Service) RunPostProcess(ctx context.Context, downloadID int64) error {
	return s.Process(ctx, downloadID)
}

// Process runs the post-process pipeline for a completed download: load the download + its
// issue + series, validate the archive, render the folder+file templates, import the file
// into the library, flip the issue to Downloaded, write downloaded+processed events + a
// success history row, and remove the item from the client. A corrupt archive routes into the
// shared failure path (download Failed, attempts++, replacement enqueued) and returns nil — it
// is a handled failure, not a job error. A re-run on an already-Downloaded issue is a safe
// no-op.
func (s *Service) Process(ctx context.Context, downloadID int64) error {
	row, err := s.repos.Downloads.Get(ctx, downloadID)
	if err != nil {
		return fmt.Errorf("load download %d: %w", downloadID, err)
	}
	// Idempotency on the download row's own status: only a Completed download is processable.
	// An already-terminal row (Blacklisted from a prior corrupt run, or Failed) is
	// a handled no-op so a River retry of a corrupt job — e.g. a worker crash after handleCorrupt
	// committed the Blacklisted flip but before the job ack — acks cleanly instead of looping on
	// ErrDownloadNotCompleted and double-counting download_attempts.
	switch series.DownloadStatus(row.Status) {
	case series.DownloadCompleted:
		// proceed
	case series.DownloadBlacklisted, series.DownloadFailed:
		s.logger.Info("post-process skipped: download already terminal",
			slog.Int64("download_id", downloadID), slog.String("status", row.Status))
		return nil
	default:
		return fmt.Errorf("%w: download %d is %q", ErrDownloadNotCompleted, downloadID, row.Status)
	}

	issue, err := s.repos.Issue.GetByID(ctx, row.IssueID)
	if err != nil {
		return fmt.Errorf("load issue %d: %w", row.IssueID, err)
	}

	// Idempotency guard (mirrors grab.go's already-Snatched guard): a download whose issue is
	// already Downloaded was post-processed by a prior run — skip the transition + events so a
	// re-enqueue coalesces to a no-op.
	if series.IssueStatus(issue.Status) == series.StatusDownloaded {
		s.logger.Info("post-process skipped: issue already Downloaded",
			slog.Int64("download_id", downloadID), slog.Int64("issue_id", row.IssueID))
		return nil
	}

	storagePath, ok, err := s.repos.Downloads.GetCompletedStorage(ctx, row.IssueID, row.Provider, row.ReleaseKey)
	if err != nil {
		return fmt.Errorf("resolve storage path for download %d: %w", downloadID, err)
	}
	if !ok || strings.TrimSpace(storagePath) == "" {
		return fmt.Errorf("%w: download %d", ErrNoStoragePath, downloadID)
	}

	// (1) Validate the archive. A corrupt/truncated/unrecognized archive routes into the
	// shared failure path (same as a failed grab).
	format, verr := detectAndValidate(storagePath)
	if verr != nil {
		s.logger.Warn("post-process: archive validation failed, routing to replacement",
			slog.Int64("download_id", downloadID), slog.String("storage", storagePath),
			slog.Any("error", verr))
		return s.handleCorrupt(ctx, row, issue)
	}

	// (2) Render the folder + file templates from the live renameconfig.
	srcSeries, err := s.repos.Series.GetByID(ctx, issue.SeriesID)
	if err != nil {
		return fmt.Errorf("load series %d: %w", issue.SeriesID, err)
	}
	cfg, err := s.renameConfig(ctx)
	if err != nil {
		return err
	}
	opts := renderOptsFrom(cfg)
	values := s.tokenValues(ctx, srcSeries, issue)

	// Folder: prefer the series add-time location when present, else recompute from the folder
	// template. The file keeps the source extension (validation confirmed format).
	folder := s.resolveFolder(issue, cfg.FolderFormat, values, opts)
	fileBase := strings.TrimSpace(Render(cfg.FileFormat, values, opts))
	// Reject an empty rendered file base: otherwise filepath.Join yields a dotfile like
	// ".cbz" (or ".cbz" at the library root when folder is empty too), which safeJoin accepts —
	// filing a comic as a hidden file that silently collides with the next empty-render import.
	if fileBase == "" {
		return fmt.Errorf("post-process: rendered empty filename for download %d", downloadID)
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(storagePath)), ".")
	if ext == "" {
		ext = format // fall back to the detected archive format as the extension
	}
	relPath := filepath.Join(folder, fileBase+"."+ext)

	// (3) Import (hardlink->copy) into the library, asserting containment under LibraryPath
	// (the importer owns the path-traversal guard).
	if ierr := s.importer.Import(storagePath, relPath, s.libraryPath); ierr != nil {
		return fmt.Errorf("import download %d into library: %w", downloadID, ierr)
	}
	finalPath := filepath.Join(s.libraryPath, relPath)

	nowISO := s.now().UTC().Format(time.RFC3339)

	// (4) Flip the issue Snatched/Completed->Downloaded via the state machine, never a raw
	// UPDATE.
	if terr := s.transitionToDownloaded(ctx, issue); terr != nil {
		return terr
	}

	// (5) Write the downloaded + processed timeline events.
	if eerr := s.writeEvent(ctx, row.IssueID, "downloaded", downloadedPayload{
		Provider: row.Provider, ReleaseKey: row.ReleaseKey,
	}, nowISO); eerr != nil {
		return eerr
	}
	if eerr := s.writeEvent(ctx, row.IssueID, "processed", processedPayload{
		Provider: row.Provider, ReleaseKey: row.ReleaseKey, Format: format, Path: finalPath,
	}, nowISO); eerr != nil {
		return eerr
	}

	// (6) Append the terminal success history row.
	if _, herr := s.repos.Downloads.InsertHistory(ctx, repository.DownloadHistoryInsert{
		IssueID: row.IssueID, Provider: row.Provider, ReleaseKey: row.ReleaseKey,
		Result: "success", Detail: finalPath, OccurredAt: nowISO,
	}); herr != nil {
		return fmt.Errorf("record success history for download %d: %w", downloadID, herr)
	}

	// (7) Remove the item from the client — only AFTER a confirmed successful import so a failed
	// file is never orphaned. A removal failure is non-fatal.
	s.removeFromClient(ctx, row)

	s.logger.Info("post-process complete",
		slog.Int64("download_id", downloadID), slog.Int64("issue_id", row.IssueID),
		slog.String("format", format), slog.String("path", finalPath))
	return nil
}

// handleCorrupt routes a corrupt archive into the shared failure path, exactly mirroring a
// failed grab: flip the download to Failed(reason=corrupt), bump download_attempts, write a
// failed timeline event + a corrupt history row, and enqueue a replacement search. It returns
// nil — a corrupt archive is a handled failure, not a job error (River must not retry it).
func (s *Service) handleCorrupt(ctx context.Context, row repository.DownloadRow, issue repository.Issue) error {
	nowISO := s.now().UTC().Format(time.RFC3339)

	// Partial-replay guard: if this download was already routed to Blacklisted by a prior
	// (interrupted) corrupt run, do not re-bump download_attempts / re-write history /
	// re-enqueue a replacement. A re-read of the freshest status closes the window where a crash
	// between the Blacklisted flip and the job ack would otherwise double-count.
	if cur, gerr := s.repos.Downloads.Get(ctx, row.ID); gerr == nil {
		if series.DownloadStatus(cur.Status) == series.DownloadBlacklisted {
			s.logger.Info("post-process: corrupt download already blacklisted, skipping replay",
				slog.Int64("download_id", row.ID), slog.Int64("issue_id", row.IssueID))
			return nil
		}
	}

	// The download itself genuinely Completed (the bytes arrived) — Completed is terminal in
	// the download-status machine (Completed->Failed is illegal). A corrupt archive is a
	// POST-download failure, so we route the download row Completed->Blacklisted (a legal
	// edge): this both respects the state machine and records the dead release_key so the
	// replacement dedup (ListDeadReleaseKeys covers Failed/Blacklisted) excludes this corrupt
	// release from the replacement search.
	next, terr := series.TransitionDownload(series.DownloadStatus(row.Status), series.DownloadBlacklisted)
	if terr != nil {
		return fmt.Errorf("transition download %d to Blacklisted: %w", row.ID, terr)
	}
	if _, uerr := s.repos.Downloads.UpdateStatus(ctx, row.ID, string(next), nowISO); uerr != nil {
		return fmt.Errorf("blacklist corrupt download %d: %w", row.ID, uerr)
	}

	// Flip the issue Snatched->Failed via the state machine (the snatched issue's grab is dead).
	if series.IssueStatus(issue.Status) != series.StatusFailed {
		newStatus, ierr := series.Transition(
			series.IssueStatus(issue.Status), series.StatusFailed,
			int(issue.SearchAttempts), s.attemptCap,
		)
		if ierr != nil {
			return fmt.Errorf("transition issue %d to Failed: %w", issue.ID, ierr)
		}
		if perr := s.repos.Issue.UpdateStatus(ctx, issue.ID, string(newStatus), clampInt32(issue.SearchAttempts)); perr != nil {
			return fmt.Errorf("persist failed issue status: %w", perr)
		}
	}

	// Bump download_attempts — a corrupt archive counts as a failed download cycle.
	if aerr := s.repos.Issue.IncrementDownloadAttempts(ctx, issue.ID); aerr != nil {
		return fmt.Errorf("bump download_attempts for issue %d: %w", issue.ID, aerr)
	}

	if eerr := s.writeEvent(ctx, row.IssueID, "failed", corruptPayload{
		Provider: row.Provider, ReleaseKey: row.ReleaseKey, Reason: "corrupt",
	}, nowISO); eerr != nil {
		return eerr
	}

	if _, herr := s.repos.Downloads.InsertHistory(ctx, repository.DownloadHistoryInsert{
		IssueID: row.IssueID, Provider: row.Provider, ReleaseKey: row.ReleaseKey,
		Result: "corrupt", Detail: "archive validation failed", OccurredAt: nowISO,
	}); herr != nil {
		return fmt.Errorf("record corrupt history for download %d: %w", row.ID, herr)
	}

	// Fan out a replacement search through the shared seam. Nil enqueuer = no-op.
	if s.enqueuer != nil {
		if qerr := s.enqueuer.EnqueueReplacementSearch(ctx, row.IssueID); qerr != nil {
			return fmt.Errorf("enqueue replacement for issue %d: %w", row.IssueID, qerr)
		}
	}

	s.logger.Info("post-process: corrupt archive routed to replacement",
		slog.Int64("download_id", row.ID), slog.Int64("issue_id", row.IssueID))
	return nil
}

// transitionToDownloaded flips the issue to Downloaded via the state machine. The legal
// source is Snatched (the live grab) — a Completed-but-not-yet-Downloaded issue should be
// Snatched at this point. An illegal edge is surfaced.
func (s *Service) transitionToDownloaded(ctx context.Context, issue repository.Issue) error {
	newStatus, terr := series.Transition(
		series.IssueStatus(issue.Status), series.StatusDownloaded,
		int(issue.SearchAttempts), s.attemptCap,
	)
	if terr != nil {
		return fmt.Errorf("transition issue %d to Downloaded: %w", issue.ID, terr)
	}
	if perr := s.repos.Issue.UpdateStatus(ctx, issue.ID, string(newStatus), clampInt32(issue.SearchAttempts)); perr != nil {
		return fmt.Errorf("persist downloaded status: %w", perr)
	}
	return nil
}

// resolveFolder picks the series add-time location when the issue carries one, else recomputes
// the folder from the folder template. A rendered folder is library-relative.
func (s *Service) resolveFolder(issue repository.Issue, folderTemplate string, values map[string]string, opts RenderOpts) string {
	if issue.Location.Valid && strings.TrimSpace(issue.Location.String) != "" {
		return strings.TrimSpace(issue.Location.String)
	}
	return Render(folderTemplate, values, opts)
}

// tokenValues builds the token value-map for Render from the series + issue + publisher.
func (s *Service) tokenValues(ctx context.Context, srcSeries repository.Series, issue repository.Issue) map[string]string {
	values := map[string]string{
		TokenSeries: srcSeries.Name,
		TokenIssue:  issue.IssueNumberRaw,
		TokenTitle:  issue.Title.String,
	}
	if srcSeries.StartYear.Valid {
		year := strconv.FormatInt(srcSeries.StartYear.Int64, 10)
		values[TokenYear] = year
		values[TokenVolumeY] = "(" + year + ")"
	}
	if srcSeries.PublisherID.Valid {
		if pub, err := s.repos.Publisher.GetByID(ctx, srcSeries.PublisherID.Int64); err == nil {
			values[TokenPublisher] = pub.Name
		}
	}
	// $monthname/$month derive from the issue's cover date (YYYY-MM-DD) when present.
	if month, name := monthFromCoverDate(issue.CoverDate.String); month != "" {
		values[TokenMonth] = month
		values[TokenMonthName] = name
	}
	return values
}

// renameConfig resolves the live renaming config, falling back to the Mylar defaults when no
// getter was injected (defensive — app.go always injects one).
func (s *Service) renameConfig(ctx context.Context) (renameconfig.Config, error) {
	if s.renameCfg == nil {
		return renameconfig.DefaultRenameConfig(), nil
	}
	cfg, err := s.renameCfg.Get(ctx)
	if err != nil {
		return renameconfig.Config{}, fmt.Errorf("resolve rename config: %w", err)
	}
	return cfg, nil
}

// writeEvent appends a timeline event with a JSON payload.
func (s *Service) writeEvent(ctx context.Context, issueID int64, eventType string, payload any, nowISO string) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s payload: %w", eventType, err)
	}
	if _, eerr := s.repos.IssueEvents.Insert(ctx, repository.IssueEventInsert{
		IssueID: issueID, EventType: eventType, PayloadJSON: string(encoded), OccurredAt: nowISO,
	}); eerr != nil {
		return fmt.Errorf("write %s event for issue %d: %w", eventType, issueID, eerr)
	}
	return nil
}

// removeFromClient removes the completed item from the client via the Tracker capability. Only
// providers implementing Tracker support removal; a removal failure is non-fatal.
func (s *Service) removeFromClient(ctx context.Context, row repository.DownloadRow) {
	provider, ok := s.providers[row.Provider]
	if !ok {
		return
	}
	tracker, ok := provider.(download.Tracker)
	if !ok {
		return
	}
	if rerr := tracker.RemoveFromHistory(ctx, row.ClientRef); rerr != nil {
		s.logger.Warn("post-process: remove-from-history failed (non-fatal)",
			slog.Int64("download_id", row.ID), slog.Any("error", rerr))
	}
}

// renderOptsFrom maps the live renameconfig.Config to RenderOpts.
func renderOptsFrom(cfg renameconfig.Config) RenderOpts {
	return RenderOpts{
		RenameEnabled:  cfg.RenameEnabled,
		PaddingEnabled: cfg.IssuePaddingEnabled,
		PaddingFormat:  cfg.PaddingFormat,
		ReplaceSpaces:  cfg.ReplaceSpaces,
		Lowercase:      cfg.Lowercase,
	}
}

// monthNames maps a 1-based month index to its English name (for $monthname).
var monthNames = []string{
	"January", "February", "March", "April", "May", "June",
	"July", "August", "September", "October", "November", "December",
}

// monthFromCoverDate parses a YYYY-MM-DD cover date and returns the zero-padded month number
// and its English name. An unparseable/absent date yields empty strings (graceful omission).
func monthFromCoverDate(coverDate string) (month, name string) {
	coverDate = strings.TrimSpace(coverDate)
	if coverDate == "" {
		return "", ""
	}
	t, err := time.Parse("2006-01-02", coverDate)
	if err != nil {
		// Tolerate a bare YYYY-MM.
		if t, err = time.Parse("2006-01", coverDate); err != nil {
			return "", ""
		}
	}
	m := int(t.Month())
	return fmt.Sprintf("%02d", m), monthNames[m-1]
}

// clampInt32 clamps an int64 into int32 range for the search_attempts pass-through on
// UpdateStatus (mirrors grab.go's clamp; the counter is small).
func clampInt32(v int64) int32 {
	const maxInt32, minInt32 = int64(1<<31 - 1), int64(-(1 << 31))
	if v > maxInt32 {
		return int32(maxInt32)
	}
	if v < minInt32 {
		return int32(minInt32)
	}
	return int32(v)
}
