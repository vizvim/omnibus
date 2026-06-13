package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/vizvim/omnibus/internal/download"
	"github.com/vizvim/omnibus/internal/indexer"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/series"
)

// clampInt32 clamps an int64 into int32 range (search_attempts is small; the clamp
// keeps gosec G115 satisfied).
func clampInt32(v int64) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}

// ErrProviderNotFound is returned when no Submitter matches a grab's kind.
var ErrProviderNotFound = errors.New("no download provider for kind")

// ErrCrossIssueGrab is returned when a grab's release_key was not a candidate for the
// target issue (T-4-02).
var ErrCrossIssueGrab = errors.New("release does not belong to this issue")

// DownloadResult is the outcome of a successful grab.
type DownloadResult struct {
	DownloadID int64
	ClientRef  string
	Provider   string
	ReleaseKey string
	Status     string
}

// Submitter hands a chosen release off to a download client and returns a client-side
// reference (e.g. a SABnzbd nzo_id or a GetComics DDL job id) used later to track it. It is the
// narrow, consumer-owned seam the grab path needs from a download provider: the map key already
// supplies the provider kind, so Submit is the only method required here. The SABnzbd and
// GetComics providers satisfy it; the download.FakeProvider satisfies it for tests.
type Submitter interface {
	Submit(ctx context.Context, req download.GrabRequest) (clientRef string, err error)
}

// GrabDeps are the collaborators a Grabber needs. It is embedded into the search service
// (Plan 05) which assembles these.
type GrabDeps struct {
	// Providers maps a provider kind ("sabnzbd"/"getcomics") to its Submitter.
	Providers  map[string]Submitter
	Repos      *repository.Repositories
	Logger     *slog.Logger
	AttemptCap int
	// Now is an injectable clock for deterministic timestamps in tests.
	Now func() time.Time
}

// Grabber performs the grab handoff: submit → create download row → flip the issue to
// Snatched via the state machine → write the snatched timeline event (D-14). It stops at
// the snatch — no polling, progress, history, or post-processing (Phase 5).
type Grabber struct {
	providers  map[string]Submitter
	repos      *repository.Repositories
	logger     *slog.Logger
	attemptCap int
	now        func() time.Time
}

// NewGrabber builds a Grabber from its deps.
func NewGrabber(d GrabDeps) *Grabber {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Grabber{
		providers:  d.Providers,
		repos:      d.Repos,
		logger:     logger,
		attemptCap: d.AttemptCap,
		now:        now,
	}
}

// snatchedPayload is the issue_events.payload_json for a snatched event (D-04 timeline).
type snatchedPayload struct {
	Provider   string `json:"provider"`
	ReleaseKey string `json:"release_key"`
	Title      string `json:"title"`
	ClientRef  string `json:"client_ref"`
}

// Grab hands off the chosen candidate, records the download, transitions the issue to
// Snatched, and writes the snatched event. providerKind selects the DownloadProvider
// (sabnzbd for NZB, getcomics for DDL). releaseKey must match cand.ReleaseKey (the
// caller's chosen release for THIS issue).
func (g *Grabber) Grab(
	ctx context.Context,
	issueID int64,
	providerKind, releaseKey string,
	cand indexer.Candidate,
) (DownloadResult, error) {
	// Cross-issue guard (T-4-02): the release_key argument must match the candidate the
	// caller resolved for this issue.
	if releaseKey != cand.ReleaseKey {
		return DownloadResult{}, ErrCrossIssueGrab
	}

	provider, ok := g.providers[providerKind]
	if !ok {
		return DownloadResult{}, fmt.Errorf("%w: %q", ErrProviderNotFound, providerKind)
	}

	// Load the issue to drive the state-machine transition (not a raw UPDATE, ADR 0004).
	issue, err := g.repos.Issue.GetByID(ctx, issueID)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("load issue %d: %w", issueID, err)
	}

	// (1) Submit to the download client.
	clientRef, err := provider.Submit(ctx, download.GrabRequest{
		Title:       cand.Title,
		DownloadURL: cand.DownloadURL,
		ReleaseKey:  cand.ReleaseKey,
		SizeBytes:   cand.SizeBytes,
	})
	if err != nil {
		return DownloadResult{}, fmt.Errorf("submit to %s: %w", providerKind, err)
	}

	nowISO := g.now().UTC().Format(time.RFC3339)

	// (2) Create the downloads row (idempotent on provider+release_key+issue_id).
	row, err := g.repos.Downloads.Upsert(ctx, repository.DownloadUpsert{
		IssueID:      issueID,
		Provider:     providerKind,
		ReleaseKey:   cand.ReleaseKey,
		ReleaseTitle: cand.Title,
		SizeBytes:    cand.SizeBytes,
		Status:       "Queued",
		ClientRef:    clientRef,
	}, nowISO)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("create download row: %w", err)
	}

	// (3) Wanted→Snatched via the state machine seam (ADR 0004), not a raw status write.
	// An already-Snatched issue makes the grab an idempotent no-op for the transition +
	// event (the download upsert above already deduped the row) — a duplicate grab must
	// not error or write a second snatched event.
	if series.IssueStatus(issue.Status) != series.StatusSnatched {
		newStatus, terr := series.Transition(
			series.IssueStatus(issue.Status), series.StatusSnatched,
			int(issue.SearchAttempts), g.attemptCap,
		)
		if terr != nil {
			return DownloadResult{}, fmt.Errorf("transition issue %d to Snatched: %w", issueID, terr)
		}
		if uerr := g.repos.Issue.UpdateStatus(ctx, issueID, string(newStatus), clampInt32(issue.SearchAttempts)); uerr != nil {
			return DownloadResult{}, fmt.Errorf("persist snatched status: %w", uerr)
		}

		// (4) Write the snatched timeline event (D-04 / OBS-01) — only on the first snatch.
		payload, merr := json.Marshal(snatchedPayload{
			Provider:   providerKind,
			ReleaseKey: cand.ReleaseKey,
			Title:      cand.Title,
			ClientRef:  clientRef,
		})
		if merr != nil {
			return DownloadResult{}, fmt.Errorf("encode snatched payload: %w", merr)
		}
		if _, eerr := g.repos.IssueEvents.Insert(ctx, repository.IssueEventInsert{
			IssueID:     issueID,
			EventType:   "snatched",
			PayloadJSON: string(payload),
			OccurredAt:  nowISO,
		}); eerr != nil {
			return DownloadResult{}, fmt.Errorf("write snatched event: %w", eerr)
		}
	}

	g.logger.Info("grabbed release",
		slog.Int64("issue_id", issueID),
		slog.String("provider", providerKind),
		slog.String("release_key", cand.ReleaseKey),
		slog.String("client_ref", clientRef),
	)

	return DownloadResult{
		DownloadID: row.ID,
		ClientRef:  clientRef,
		Provider:   providerKind,
		ReleaseKey: cand.ReleaseKey,
		Status:     row.Status,
	}, nil
}
