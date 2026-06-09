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
	DataPath   string
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
	dataPath   string
	attemptCap int
	logger     *slog.Logger
	lifeCtx    context.Context
	wg         *sync.WaitGroup
}

// View is the assembled GetSeries result.
type View struct {
	Series    repository.Series
	Issues    []repository.Issue
	Publisher string
	StoryArcs []repository.StoryArc
}

// New constructs a Service.
func New(d Deps) *Service {
	return &Service{
		gw:         d.Gateway,
		repos:      d.Repos,
		dataPath:   d.DataPath,
		attemptCap: d.AttemptCap,
		logger:     d.Logger,
		lifeCtx:    d.LifeCtx,
		wg:         d.WaitGroup,
	}
}

// SearchComicVine returns candidate volumes for a query (META-01).
func (s *Service) SearchComicVine(ctx context.Context, query string) ([]metadata.SeriesResult, error) {
	return s.gw.SearchSeries(ctx, query)
}

// AddSeries upserts the series on its immutable volume id and returns immediately
// (D-03), launching a bounded background import goroutine (D-03/04/05).
func (s *Service) AddSeries(ctx context.Context, volumeID int64) (repository.Series, error) {
	vol, err := s.gw.GetVolume(ctx, volumeID)
	if err != nil {
		return repository.Series{}, fmt.Errorf("fetch volume %d: %w", volumeID, err)
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
		return repository.Series{}, fmt.Errorf("upsert series: %w", err)
	}

	// Fast return: import runs in the background, bounded by the lifecycle context.
	s.startImport(created, vol)
	return created, nil
}

// ListSeries returns watched series (paged).
func (s *Service) ListSeries(ctx context.Context, page int32) ([]repository.Series, error) {
	const pageLen = 50
	if page < 0 {
		page = 0
	}
	return s.repos.Series.List(ctx, pageLen, page*pageLen)
}

// GetSeries returns the full series view: series + issues + publisher + arcs (SER-03).
func (s *Service) GetSeries(ctx context.Context, seriesID int64) (View, error) {
	ser, err := s.repos.Series.GetByID(ctx, seriesID)
	if err != nil {
		return View{}, fmt.Errorf("get series %d: %w", seriesID, err)
	}
	issues, err := s.repos.Issue.ListBySeries(ctx, seriesID)
	if err != nil {
		return View{}, fmt.Errorf("list issues: %w", err)
	}
	arcs, err := s.repos.Arc.ListBySeries(ctx, seriesID)
	if err != nil {
		return View{}, fmt.Errorf("list arcs: %w", err)
	}

	publisher := ""
	if ser.PublisherID.Valid {
		if p, perr := s.repos.Publisher.GetByID(ctx, ser.PublisherID.Int64); perr == nil {
			publisher = p.Name
		}
	}

	return View{Series: ser, Issues: issues, Publisher: publisher, StoryArcs: arcs}, nil
}

// UpdateSeriesSettings updates the watch status (Active/Paused/Ended).
func (s *Service) UpdateSeriesSettings(ctx context.Context, seriesID int64, status string) (repository.Series, error) {
	switch status {
	case "Active", "Paused", "Ended":
	default:
		return repository.Series{}, fmt.Errorf("invalid series status %q", status)
	}
	return s.repos.Series.UpdateSettings(ctx, seriesID, status, "")
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
