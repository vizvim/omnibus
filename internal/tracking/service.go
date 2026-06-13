// Package tracking owns the download-status poll loop: it reconciles every active
// (Queued/Downloading) download against its download client via the pull-based Tracker
// contract, surfaces progress + lifecycle on the per-issue timeline, records append-only
// history, and detects failures (explicit client failure + stall). It is the PollRunner the
// periodic DownloadPoll River job delegates to.
package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/vizvim/omnibus/internal/download"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/series"
)

// defaultStallWindow is how long a Downloading row may sit with no byte progress before it
// is reaped as Failed (the *arr stalled-reap behavior). A hardcoded sensible default; not a
// config knob (lean-config posture).
const defaultStallWindow = 30 * time.Minute

// Enqueuer is the reactive seam the poll loop fans out through after a terminal transition.
// The post-process and replacement-search jobs land later, so the tracking service enqueues by
// intent here and the jobs client (or a later wiring) provides the implementation. Declaring it
// here keeps tracking free of any internal/jobs import. A nil enqueuer makes both calls no-ops
// (the terminal flip + history still happen), so this is independently shippable before the
// downstream jobs exist.
type Enqueuer interface {
	// EnqueuePostProcess schedules post-processing of a completed download.
	EnqueuePostProcess(ctx context.Context, downloadID int64) error
	// EnqueueReplacementSearch schedules an attempt-capped replacement search for an issue
	// whose download failed, excluding the dead release_keys.
	EnqueueReplacementSearch(ctx context.Context, issueID int64) error
}

// DDLFetcher resolves a working mirror from a GetComics post page (the download's clientRef)
// and streams the file to an intermediate dir, returning the local path. The
// download.GetComicsDDLProvider satisfies it via Fetch. Declaring the narrow interface here
// keeps tracking free of a hard dependency on the concrete provider type. progress, when
// non-nil, is called with (bytesWritten, totalBytes) as the stream flows.
type DDLFetcher interface {
	Fetch(ctx context.Context, clientRef string, progress func(done, total int64)) (string, error)
}

// Deps are the tracking Service's injected collaborators.
type Deps struct {
	Repos *repository.Repositories
	// Trackers maps a provider kind ("sabnzbd"/"getcomics") to its Tracker. Only kinds with
	// a Tracker are polled; others are skipped (GetComics DDL tracking lands later).
	Trackers map[string]download.Tracker
	// DDLFetchers maps a provider kind ("getcomics") to its DDLFetcher. The DDLFetch job
	// (RunDDLFetch) resolves+streams via the matching fetcher, then feeds the file into the
	// SAME Completed->post-process path as a SAB download.
	DDLFetchers map[string]DDLFetcher
	Logger      *slog.Logger
	// StallWindow overrides defaultStallWindow when > 0 (test seam).
	StallWindow time.Duration
	// Now is an injectable clock for deterministic timestamps in tests.
	Now func() time.Time
}

// Service polls active downloads and drives their observable lifecycle. It depends only on
// repository interfaces + the Tracker contract + the download-status state machine.
type Service struct {
	repos       *repository.Repositories
	trackers    map[string]download.Tracker
	ddlFetchers map[string]DDLFetcher
	logger      *slog.Logger
	stallWindow time.Duration
	now         func() time.Time
	enqueuer    Enqueuer
}

// New constructs a tracking Service.
func New(d Deps) *Service {
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	stall := d.StallWindow
	if stall <= 0 {
		stall = defaultStallWindow
	}
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		repos:       d.Repos,
		trackers:    d.Trackers,
		ddlFetchers: d.DDLFetchers,
		logger:      logger,
		stallWindow: stall,
		now:         now,
	}
}

// SetEnqueuer wires the reactive enqueuer after construction (mirrors the search/series
// service<->jobs cycle resolution). A nil enqueuer leaves the post-process / replacement
// fan-out as no-ops.
func (s *Service) SetEnqueuer(e Enqueuer) { s.enqueuer = e }

// progressPayload is the issue_events.payload_json for an in-progress download event.
type progressPayload struct {
	Provider    string  `json:"provider"`
	ReleaseKey  string  `json:"release_key"`
	ClientRef   string  `json:"client_ref"`
	ProgressPct float64 `json:"progress_pct"`
}

// failedPayload is the issue_events.payload_json for a failed download event.
type failedPayload struct {
	Provider   string `json:"provider"`
	ReleaseKey string `json:"release_key"`
	ClientRef  string `json:"client_ref"`
	Reason     string `json:"reason"`
}

// RunDownloadPoll reconciles every active download against its client in one tick. It loads
// the active set (ListActive), polls each via its Tracker, and applies: progress event +
// Queued->Downloading on progress; Completed -> capture storage + history + enqueue
// post-process; Failed -> failed event + history + enqueue replacement; stall -> Failed. A
// not-ready poll is a no-op (re-poll next tick). One row's error is
// logged and skipped so a single bad download does not stall the whole tick.
func (s *Service) RunDownloadPoll(ctx context.Context) error {
	active, err := s.repos.Downloads.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("list active downloads: %w", err)
	}
	for _, row := range active {
		if err := ctx.Err(); err != nil {
			return err
		}
		if perr := s.pollOne(ctx, row); perr != nil {
			s.logger.Warn("download poll failed for row",
				slog.Int64("download_id", row.ID),
				slog.String("client_ref", row.ClientRef),
				slog.Any("error", perr))
		}
	}
	return nil
}

// RunDDLFetch resolves+streams a GetComics direct download and feeds it into the shared
// completion path, or routes mirror exhaustion into the shared failure path. It
// is the DDLFetchRunner the serialized "ddl" River job delegates to. The download row is
// loaded fresh; a row already in a terminal state (Completed/Failed/Blacklisted) is a safe
// no-op (idempotent re-run), mirroring the poll loop's terminal-state handling.
func (s *Service) RunDDLFetch(ctx context.Context, downloadID int64) error {
	row, err := s.repos.Downloads.Get(ctx, downloadID)
	if err != nil {
		return fmt.Errorf("load download %d: %w", downloadID, err)
	}

	// Idempotency guard: only an in-flight (Queued/Downloading) download is fetchable. A
	// re-enqueue of an already-terminal row is a no-op (the unique-by-args opts make this rare).
	switch series.DownloadStatus(row.Status) {
	case series.DownloadQueued, series.DownloadDownloading:
		// fetchable
	default:
		s.logger.Info("ddl fetch skipped: download already terminal",
			slog.Int64("download_id", downloadID), slog.String("status", row.Status))
		return nil
	}

	fetcher, ok := s.ddlFetchers[row.Provider]
	if !ok {
		return fmt.Errorf("no ddl fetcher for provider %q (download %d)", row.Provider, downloadID)
	}

	// Stream the file. The clientRef is the post URL the resolver re-fetches for mirror links.
	// Progress is surfaced on the per-issue timeline by reusing the poll loop's progress path
	// (Queued->Downloading on the first tick, then periodic download-progress + updated_at bumps
	// so a multi-GB DDL shows real movement and stays liveness-observable).
	prog := s.newDDLProgressEmitter(ctx, row, downloadID)
	path, ferr := fetcher.Fetch(ctx, row.ClientRef, prog)
	if ferr != nil {
		if errors.Is(ferr, download.ErrMirrorExhaustion) {
			// Mirror exhaustion is a handled failure: route into the shared failure path
			// (Failed + replacement) and return nil so River does not retry a dead post.
			s.logger.Warn("ddl fetch exhausted all mirrors, routing to replacement",
				slog.Int64("download_id", downloadID), slog.String("client_ref", row.ClientRef))
			// Re-load the row in case onProgress flipped Queued->Downloading mid-stream.
			cur, gerr := s.repos.Downloads.Get(ctx, downloadID)
			if gerr != nil {
				return fmt.Errorf("reload download %d after exhaustion: %w", downloadID, gerr)
			}
			return s.onFailed(ctx, cur, "mirror exhaustion")
		}
		// A transient error (network/disk) is a job error so River retries it under the cap.
		return fmt.Errorf("ddl fetch download %d: %w", downloadID, ferr)
	}

	// Success: feed the streamed file into the SAME Completed->post-process path as a SAB grab
	// by reusing onCompleted with a synthetic completed PollResult carrying the local
	// path as storage. Re-load the row so onCompleted transitions from the current status.
	cur, gerr := s.repos.Downloads.Get(ctx, downloadID)
	if gerr != nil {
		return fmt.Errorf("reload download %d after fetch: %w", downloadID, gerr)
	}
	return s.onCompleted(ctx, cur, download.PollResult{
		Status: download.PollCompleted, StoragePath: path,
	})
}

// ddlProgressMinInterval throttles DDL progress emission by elapsed wall time and
// ddlProgressMinPctDelta by percentage advance: a tick is emitted when EITHER threshold is
// crossed (or it is the first tick). This keeps a multi-GB stream's timeline meaningful without
// writing an event per buffer.
const (
	ddlProgressMinInterval = 5 * time.Second
	ddlProgressMinPctDelta = 1.0
)

// newDDLProgressEmitter returns an onProgress(done, total) callback for the DDL Fetch stream
// that emits periodic, throttled progress to the per-issue timeline. The first
// observed progress drives the Queued->Downloading flip + initial event; subsequent ticks are
// emitted when at least ddlProgressMinInterval has elapsed OR the percentage advanced by at
// least ddlProgressMinPctDelta, each bumping updated_at so liveness is observable. The callback
// is invoked serially from the streaming reader, so no locking is needed for its closed-over
// state. A nil/zero total tick is ignored (percentage is undefined).
func (s *Service) newDDLProgressEmitter(ctx context.Context, row repository.DownloadRow, downloadID int64) func(done, total int64) {
	var (
		started   bool
		lastEmit  time.Time
		lastPct   float64
		curStatus = row.Status
	)
	return func(done, total int64) {
		if total <= 0 {
			return
		}
		pct := float64(done) / float64(total) * 100
		now := s.now()
		if started && now.Sub(lastEmit) < ddlProgressMinInterval && pct-lastPct < ddlProgressMinPctDelta {
			return
		}
		started = true
		lastEmit = now
		lastPct = pct

		// Reflect the live row status so the second+ ticks go through the "already Downloading"
		// updated_at bump rather than re-attempting the Queued->Downloading flip.
		emitRow := row
		emitRow.Status = curStatus
		if perr := s.onProgress(ctx, emitRow, download.PollResult{
			Status: download.PollInProgress, ProgressPct: pct,
		}); perr != nil {
			s.logger.Warn("ddl progress event failed (non-fatal)",
				slog.Int64("download_id", downloadID), slog.Any("error", perr))
			return
		}
		// After the first successful emit the row is Downloading; keep that for later ticks so
		// onProgress takes the bump-only path (and so a real pct>0 keeps updated_at fresh).
		curStatus = string(series.DownloadDownloading)
	}
}

// pollOne reconciles a single active download row.
func (s *Service) pollOne(ctx context.Context, row repository.DownloadRow) error {
	tracker, ok := s.trackers[row.Provider]
	if !ok {
		// No tracker for this provider kind yet (e.g. GetComics DDL): skip, not an error.
		return nil
	}

	res, err := tracker.Poll(ctx, row.ClientRef)
	if err != nil {
		return fmt.Errorf("poll %s ref %q: %w", row.Provider, row.ClientRef, err)
	}

	switch res.Status {
	case download.PollInProgress:
		return s.onProgress(ctx, row, res)
	case download.PollCompleted:
		return s.onCompleted(ctx, row, res)
	case download.PollFailed:
		return s.onFailed(ctx, row, res.FailMessage)
	case download.PollNotReady:
		// Nothing actionable yet; but a Downloading row stuck past the stall window with no
		// byte progress is reaped as Failed (stall branch).
		return s.maybeStall(ctx, row)
	default:
		return s.maybeStall(ctx, row)
	}
}

// onProgress records a progress event and flips Queued->Downloading. updated_at is
// bumped (last-progress) only when the row was still Queued OR real progress is reported, so
// a Downloading row with no movement keeps its stale updated_at for stall detection.
func (s *Service) onProgress(ctx context.Context, row repository.DownloadRow, res download.PollResult) error {
	nowISO := s.now().UTC().Format(time.RFC3339)

	if series.DownloadStatus(row.Status) == series.DownloadQueued {
		next, terr := series.TransitionDownload(series.DownloadQueued, series.DownloadDownloading)
		if terr != nil {
			return terr
		}
		if _, uerr := s.repos.Downloads.UpdateStatus(ctx, row.ID, string(next), nowISO); uerr != nil {
			return fmt.Errorf("flip download %d to Downloading: %w", row.ID, uerr)
		}
		// First observed progress: write the timeline event once on the Queued->Downloading
		// edge so the per-issue timeline shows the download starting.
		if eerr := s.writeProgressEvent(ctx, row, res, nowISO); eerr != nil {
			return eerr
		}
		return nil
	}

	// Already Downloading: only bump last-progress (updated_at) when bytes actually moved,
	// so a hung download keeps its stale timestamp and the stall window can trip.
	if res.ProgressPct > 0 {
		if _, uerr := s.repos.Downloads.UpdateStatus(ctx, row.ID, row.Status, nowISO); uerr != nil {
			return fmt.Errorf("bump download %d progress: %w", row.ID, uerr)
		}
	}
	return nil
}

// onCompleted flips the row to Completed (carrying nothing extra; storage is captured in
// history + handed to post-process), records a download_history row, and enqueues
// post-process. Removal from the client is deferred to post-process and runs only AFTER a
// confirmed successful import, so a file that fails import is never orphaned from the client's
// history.
func (s *Service) onCompleted(ctx context.Context, row repository.DownloadRow, res download.PollResult) error {
	nowISO := s.now().UTC().Format(time.RFC3339)

	next, terr := series.TransitionDownload(series.DownloadStatus(row.Status), series.DownloadCompleted)
	if terr != nil {
		return terr
	}
	if _, uerr := s.repos.Downloads.UpdateStatus(ctx, row.ID, string(next), nowISO); uerr != nil {
		return fmt.Errorf("flip download %d to Completed: %w", row.ID, uerr)
	}

	if _, herr := s.repos.Downloads.InsertHistory(ctx, repository.DownloadHistoryInsert{
		IssueID: row.IssueID, Provider: row.Provider, ReleaseKey: row.ReleaseKey,
		Result: "completed", Detail: res.StoragePath, OccurredAt: nowISO,
	}); herr != nil {
		return fmt.Errorf("record completed history for download %d: %w", row.ID, herr)
	}

	// Removal from the client is intentionally NOT done here: it is deferred to post-process so
	// it runs only after a confirmed successful import. Removing at download-client completion
	// would orphan a corrupt archive that later fails import.
	if s.enqueuer != nil {
		if eerr := s.enqueuer.EnqueuePostProcess(ctx, row.ID); eerr != nil {
			return fmt.Errorf("enqueue post-process for download %d: %w", row.ID, eerr)
		}
	}
	s.logger.Info("download completed",
		slog.Int64("download_id", row.ID), slog.Int64("issue_id", row.IssueID),
		slog.String("storage", res.StoragePath))
	return nil
}

// onFailed flips the row to Failed, writes a failed timeline event, records history, counts the
// terminal failure toward the issue's download-attempt cap, and enqueues an attempt-capped
// replacement search. reason carries the client's fail message (explicit failure) or "stall"
// (stall reap). Incrementing download_attempts here ensures the failed->replacement-grabbed->failed
// loop reaches cool-off; this path is disjoint from the post-process corrupt path, so the counter
// is bumped exactly once per terminal failure.
func (s *Service) onFailed(ctx context.Context, row repository.DownloadRow, reason string) error {
	nowISO := s.now().UTC().Format(time.RFC3339)

	next, terr := series.TransitionDownload(series.DownloadStatus(row.Status), series.DownloadFailed)
	if terr != nil {
		return terr
	}
	if _, uerr := s.repos.Downloads.UpdateStatus(ctx, row.ID, string(next), nowISO); uerr != nil {
		return fmt.Errorf("flip download %d to Failed: %w", row.ID, uerr)
	}

	payload, merr := json.Marshal(failedPayload{
		Provider: row.Provider, ReleaseKey: row.ReleaseKey, ClientRef: row.ClientRef, Reason: reason,
	})
	if merr != nil {
		return fmt.Errorf("encode failed payload: %w", merr)
	}
	if _, eerr := s.repos.IssueEvents.Insert(ctx, repository.IssueEventInsert{
		IssueID: row.IssueID, EventType: "failed", PayloadJSON: string(payload), OccurredAt: nowISO,
	}); eerr != nil {
		return fmt.Errorf("write failed event for download %d: %w", row.ID, eerr)
	}

	if _, herr := s.repos.Downloads.InsertHistory(ctx, repository.DownloadHistoryInsert{
		IssueID: row.IssueID, Provider: row.Provider, ReleaseKey: row.ReleaseKey,
		Result: "failed", Detail: reason, OccurredAt: nowISO,
	}); herr != nil {
		return fmt.Errorf("record failed history for download %d: %w", row.ID, herr)
	}

	// Count the terminal failure toward the replacement cap so the failed->replacement-grabbed->
	// failed loop eventually reaches cool-off. Runs once per terminal failure (this path and the
	// post-process corrupt path are mutually exclusive for a single event).
	if aerr := s.repos.Issue.IncrementDownloadAttempts(ctx, row.IssueID); aerr != nil {
		return fmt.Errorf("bump download_attempts for issue %d: %w", row.IssueID, aerr)
	}

	if s.enqueuer != nil {
		if eerr := s.enqueuer.EnqueueReplacementSearch(ctx, row.IssueID); eerr != nil {
			return fmt.Errorf("enqueue replacement search for issue %d: %w", row.IssueID, eerr)
		}
	}
	s.logger.Info("download failed",
		slog.Int64("download_id", row.ID), slog.Int64("issue_id", row.IssueID),
		slog.String("reason", reason))
	return nil
}

// maybeStall reaps a Downloading row whose last-progress (updated_at) is older than the
// stall window as Failed(reason=stall). Queued rows are not stalled (they have not
// started). A parse failure on updated_at is treated as "not stalled" (conservative).
func (s *Service) maybeStall(ctx context.Context, row repository.DownloadRow) error {
	if series.DownloadStatus(row.Status) != series.DownloadDownloading {
		return nil
	}
	if !s.isStalled(row) {
		return nil
	}
	return s.onFailed(ctx, row, "stall")
}

// isStalled reports whether a Downloading row's last-progress (updated_at) is older than the
// stall window. An unparseable updated_at is treated as "not stalled" (conservative): we
// cannot judge staleness, so we let a later tick re-evaluate after a status write refreshes it.
func (s *Service) isStalled(row repository.DownloadRow) bool {
	last, perr := time.Parse(time.RFC3339, row.UpdatedAt)
	if perr != nil {
		s.logger.Warn("download has unparseable updated_at; skipping stall check",
			slog.Int64("download_id", row.ID), slog.String("updated_at", row.UpdatedAt))
		return false
	}
	return s.now().UTC().Sub(last) >= s.stallWindow
}

// writeProgressEvent appends an in-progress download event to the per-issue timeline.
func (s *Service) writeProgressEvent(ctx context.Context, row repository.DownloadRow, res download.PollResult, nowISO string) error {
	payload, err := json.Marshal(progressPayload{
		Provider: row.Provider, ReleaseKey: row.ReleaseKey, ClientRef: row.ClientRef, ProgressPct: res.ProgressPct,
	})
	if err != nil {
		return fmt.Errorf("encode progress payload: %w", err)
	}
	if _, eerr := s.repos.IssueEvents.Insert(ctx, repository.IssueEventInsert{
		IssueID: row.IssueID, EventType: "download-progress", PayloadJSON: string(payload), OccurredAt: nowISO,
	}); eerr != nil {
		return fmt.Errorf("write progress event for download %d: %w", row.ID, eerr)
	}
	return nil
}
