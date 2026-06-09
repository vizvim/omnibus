package series

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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
}

// Deps are the Service's injected collaborators.
type Deps struct {
	Gateway    Gateway
	Repos      *repository.Repositories
	AttemptCap int
	Logger     *slog.Logger
	// LifeCtx bounds background import goroutines; cancel it on shutdown (PLAT-08).
	LifeCtx   context.Context
	WaitGroup *sync.WaitGroup
}

// Service implements the SeriesService domain logic (ADR 0007 segmentation). It
// depends only on repository interfaces and the metadata gateway.
type Service struct {
	gw         Gateway
	repos      *repository.Repositories
	attemptCap int
	logger     *slog.Logger
	lifeCtx    context.Context
	wg         *sync.WaitGroup
}

// View is the assembled GetSeries result, expressed entirely in series-owned domain
// types so the transport layer never imports repository (PLAT-05).
type View struct {
	Series    Series
	Issues    []Issue
	Publisher string
	StoryArcs []StoryArc
}

// New constructs a Service.
func New(d Deps) *Service {
	return &Service{
		gw:         d.Gateway,
		repos:      d.Repos,
		attemptCap: d.AttemptCap,
		logger:     d.Logger,
		lifeCtx:    d.LifeCtx,
		wg:         d.WaitGroup,
	}
}

// SearchResult is a transport-agnostic search candidate so the transport layer does
// not import the provider package (package-layout.md layer rule).
type SearchResult struct {
	ComicvineVolumeID int64
	Name              string
	StartYear         int32
	Publisher         string
	CountOfIssues     int32
	CoverURL          string
	Description       string
}

// SearchComicVine returns candidate volumes for a query (META-01).
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

// AddSeries upserts the series on its immutable volume id and returns immediately
// (D-03), launching a bounded background import goroutine (D-03/04/05).
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

	// Fast return: import runs in the background, bounded by the lifecycle context.
	s.startImport(created, vol)

	publisher := ""
	if vol.Publisher.Name != "" {
		publisher = vol.Publisher.Name
	}
	// A freshly-added series has no cover yet — the import goroutine stores it later.
	return seriesFromRow(created, publisher, false), nil
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

// GetSeries returns the full series view: series + issues + publisher + arcs (SER-03).
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

func nowISO() string { return time.Now().UTC().Format(time.RFC3339) }

func nonZero(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

// errImportCancelled is returned internally when the lifecycle context ends mid-import.
var errImportCancelled = errors.New("import canceled by shutdown")
