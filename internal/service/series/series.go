package series

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/vizvim/omnibus/internal/provider/metadata"
	"github.com/vizvim/omnibus/internal/repository"
)

// Gateway is the metadata access surface the service depends on (satisfied by
// *metadata.Gateway). Declaring it here keeps the service testable against a fake.
type Gateway interface {
	SearchSeries(ctx context.Context, query string) ([]metadata.SeriesResult, error)
	GetVolume(ctx context.Context, volumeID int64) (metadata.VolumeDetail, error)
	ListIssues(ctx context.Context, volumeID int64, offset int) ([]metadata.IssueDetail, bool, error)
	GetCover(ctx context.Context, url string) ([]byte, error)
	// CachedSourceUpdatedAt returns the cached CV date_last_updated marker for a
	// volume (empty/false if absent) — the conditional-refresh comparison point.
	CachedSourceUpdatedAt(ctx context.Context, volumeID int64) (string, bool)
}

// Enqueuer durably enqueues background work for the service. It is satisfied by the
// jobs client; declaring it here keeps the service testable against a fake and avoids
// a dependency on the concrete jobs package (which itself depends on the service via
// the inverted ImportRunner interface).
type Enqueuer interface {
	EnqueueImport(ctx context.Context, seriesID, comicvineVolumeID int64) error
	EnqueueRefresh(ctx context.Context, seriesID, comicvineVolumeID int64) error
	// EnqueueSearchIssue enqueues a one-off auto-search when an issue becomes Wanted
	// (D-10). Duplicate enqueues collapse via the job's per-issue uniqueness, so calling
	// it on every imported Wanted issue is idempotent and bounded by River's worker pool.
	EnqueueSearchIssue(ctx context.Context, issueID int64) error
}

// Deps are the Service's injected collaborators.
type Deps struct {
	Gateway    Gateway
	Repos      *repository.Repositories
	AttemptCap int
	Logger     *slog.Logger
	// Enqueuer durably enqueues the import job. It may be nil at construction and set
	// later via SetEnqueuer to resolve the service<->jobs wiring order.
	Enqueuer Enqueuer
	// StalenessThreshold is how old a series' last_refreshed_at must be before the
	// scheduled sweep refreshes it. Zero falls back to defaultStaleness.
	StalenessThreshold time.Duration
}

// Service implements the SeriesService domain logic. It depends only on repository
// interfaces, the metadata gateway, and the job Enqueuer.
type Service struct {
	gw                 Gateway
	repos              *repository.Repositories
	attemptCap         int
	logger             *slog.Logger
	enqueuer           Enqueuer
	stalenessThreshold time.Duration
}

// View is the assembled GetSeries result, expressed entirely in series-owned domain
// types so the transport layer never imports repository.
type View struct {
	Series    Series
	Issues    []Issue
	Publisher string
	StoryArcs []StoryArc
}

// defaultStaleness is the fallback sweep staleness threshold when Deps leaves it zero.
const defaultStaleness = 7 * 24 * time.Hour

// New constructs a Service.
func New(d Deps) *Service {
	staleness := d.StalenessThreshold
	if staleness <= 0 {
		staleness = defaultStaleness
	}
	return &Service{
		gw:                 d.Gateway,
		repos:              d.Repos,
		attemptCap:         d.AttemptCap,
		logger:             d.Logger,
		enqueuer:           d.Enqueuer,
		stalenessThreshold: staleness,
	}
}

// SetEnqueuer wires the job enqueuer after construction. This resolves the
// service<->jobs construction cycle: the jobs client needs the service (as its
// ImportRunner) and the service needs the jobs client (as its Enqueuer), so the
// service is built first with a nil enqueuer and the jobs client is injected here.
func (s *Service) SetEnqueuer(e Enqueuer) { s.enqueuer = e }

// SearchResult is a transport-agnostic search candidate so the transport layer does
// not import the provider package.
type SearchResult struct {
	ComicvineVolumeID int64
	Name              string
	StartYear         int32
	Publisher         string
	CountOfIssues     int32
	CoverURL          string
	Description       string
}

// SearchComicVine returns candidate volumes for a query.
func (s *Service) SearchComicVine(ctx context.Context, query string) ([]SearchResult, error) {
	results, err := s.gw.SearchSeries(ctx, query)
	if err != nil {
		return nil, err
	}
	out := make([]SearchResult, 0, len(results))
	for _, r := range results {
		out = append(out, SearchResult{
			ComicvineVolumeID: r.ComicvineVolumeID,
			Name:              r.Name,
			StartYear:         r.StartYear,
			Publisher:         r.Publisher,
			CountOfIssues:     r.CountOfIssues,
			CoverURL:          r.CoverURL,
			Description:       r.Description,
		})
	}
	return out, nil
}

// AddSeries upserts the series on its immutable volume id and returns immediately,
// enqueuing a durable River import job (which survives a process restart).
func (s *Service) AddSeries(ctx context.Context, volumeID int64) (Series, error) {
	vol, err := s.gw.GetVolume(ctx, volumeID)
	if err != nil {
		return Series{}, fmt.Errorf("fetch volume %d: %w", volumeID, err)
	}

	var publisherID *int64
	if vol.Publisher.Name != "" {
		pub, perr := s.repos.Publisher.Upsert(ctx, repository.PublisherUpsert{
			ComicvinePublisherID: nonZero(vol.Publisher.ComicvineID),
			Name:                 vol.Publisher.Name,
			CreatedAt:            nowISO(),
		})
		if perr == nil {
			publisherID = &pub.ID
		}
	}

	startYear := vol.StartYear
	created, err := s.repos.Series.Upsert(ctx, repository.SeriesUpsert{
		ComicvineVolumeID: vol.ComicvineVolumeID,
		PublisherID:       publisherID,
		Name:              vol.Name,
		StartYear:         &startYear,
		Description:       vol.Description,
		Status:            "Active",
		TotalIssues:       vol.CountOfIssues,
		CreatedAt:         nowISO(),
	})
	if err != nil {
		return Series{}, fmt.Errorf("upsert series: %w", err)
	}

	// Fast return: the import runs as a durable River job (survives restart, idempotent
	// on re-run). Enqueue and return immediately without blocking on the import.
	if err := s.enqueuer.EnqueueImport(ctx, created.ID, created.ComicvineVolumeID); err != nil {
		return Series{}, fmt.Errorf("enqueue import for series %d: %w", created.ID, err)
	}

	publisher := ""
	if vol.Publisher.Name != "" {
		publisher = vol.Publisher.Name
	}
	// A freshly-added series has no cover yet — the import job stores it later.
	return seriesFromRow(created, publisher, false), nil
}

// RunImport loads the series row + its volume detail and runs the existing idempotent
// import. It is the body the durable River import worker invokes (satisfying
// jobs.ImportRunner). Re-running is safe: runImport is an idempotent conditional
// upsert, so a job retried after restart fills gaps without duplicating issues.
func (s *Service) RunImport(ctx context.Context, seriesID, comicvineVolumeID int64) error {
	ser, err := s.repos.Series.GetByID(ctx, seriesID)
	if err != nil {
		return fmt.Errorf("load series %d: %w", seriesID, err)
	}
	vol, err := s.gw.GetVolume(ctx, comicvineVolumeID)
	if err != nil {
		return fmt.Errorf("fetch volume %d: %w", comicvineVolumeID, err)
	}
	return s.runImport(ctx, ser, vol)
}

// coverPresent reports whether a cover blob exists for an entity. Presence checks are
// best-effort: a failure must not fail the surrounding request, so it logs and treats
// the cover as absent.
func (s *Service) coverPresent(ctx context.Context, kind string, id int64) bool {
	has, err := s.repos.Cover.Exists(ctx, kind, id)
	if err != nil {
		s.logger.Warn("cover presence check", slog.String("kind", kind), slog.Int64("id", id), slog.Any("error", err))
		return false
	}
	return has
}

// ListSeries returns watched series (paged).
func (s *Service) ListSeries(ctx context.Context, page int32) ([]Series, error) {
	const pageLen = 50
	if page < 0 {
		page = 0
	}
	rows, err := s.repos.Series.List(ctx, pageLen, page*pageLen)
	if err != nil {
		return nil, err
	}
	out := make([]Series, 0, len(rows))
	for _, r := range rows {
		out = append(out, seriesFromRow(r, "", s.coverPresent(ctx, "series", r.ID)))
	}
	return out, nil
}

// GetSeries returns the full series view: series + issues + publisher + arcs.
func (s *Service) GetSeries(ctx context.Context, seriesID int64) (View, error) {
	ser, err := s.repos.Series.GetByID(ctx, seriesID)
	if err != nil {
		return View{}, fmt.Errorf("get series %d: %w", seriesID, err)
	}
	issueRows, err := s.repos.Issue.ListBySeries(ctx, seriesID)
	if err != nil {
		return View{}, fmt.Errorf("list issues: %w", err)
	}
	arcRows, err := s.repos.Arc.ListBySeries(ctx, seriesID)
	if err != nil {
		return View{}, fmt.Errorf("list arcs: %w", err)
	}

	publisher := ""
	if ser.PublisherID.Valid {
		if p, perr := s.repos.Publisher.GetByID(ctx, ser.PublisherID.Int64); perr == nil {
			publisher = p.Name
		}
	}

	issues := make([]Issue, 0, len(issueRows))
	for _, r := range issueRows {
		issues = append(issues, issueFromRow(r, s.coverPresent(ctx, "issues", r.ID)))
	}
	arcs := make([]StoryArc, 0, len(arcRows))
	for _, r := range arcRows {
		arcs = append(arcs, arcFromRow(r))
	}

	return View{
		Series:    seriesFromRow(ser, publisher, s.coverPresent(ctx, "series", ser.ID)),
		Issues:    issues,
		Publisher: publisher,
		StoryArcs: arcs,
	}, nil
}

// UpdateSeriesSettings updates the watch status (Active/Paused/Ended).
func (s *Service) UpdateSeriesSettings(ctx context.Context, seriesID int64, status string) (Series, error) {
	switch status {
	case "Active", "Paused", "Ended":
	default:
		return Series{}, fmt.Errorf("invalid series status %q", status)
	}
	row, err := s.repos.Series.UpdateSettings(ctx, seriesID, status, "")
	if err != nil {
		return Series{}, err
	}
	return seriesFromRow(row, "", s.coverPresent(ctx, "series", row.ID)), nil
}

// RefreshSeries enqueues a durable, unique conditional refresh job for a series and
// returns the current series row immediately (the UI polls GetSeries for the result,
// mirroring AddSeries' fast-return shape). A duplicate enqueue while a refresh is
// already queued/running for the series is a no-op (River unique jobs).
func (s *Service) RefreshSeries(ctx context.Context, seriesID int64) (Series, error) {
	ser, err := s.repos.Series.GetByID(ctx, seriesID)
	if err != nil {
		return Series{}, fmt.Errorf("get series %d: %w", seriesID, err)
	}
	if err := s.enqueuer.EnqueueRefresh(ctx, ser.ID, ser.ComicvineVolumeID); err != nil {
		return Series{}, fmt.Errorf("enqueue refresh for series %d: %w", ser.ID, err)
	}

	publisher := ""
	if ser.PublisherID.Valid {
		if p, perr := s.repos.Publisher.GetByID(ctx, ser.PublisherID.Int64); perr == nil {
			publisher = p.Name
		}
	}
	return seriesFromRow(ser, publisher, s.coverPresent(ctx, "series", ser.ID)), nil
}

func nowISO() string { return time.Now().UTC().Format(time.RFC3339) }

func nonZero(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}
