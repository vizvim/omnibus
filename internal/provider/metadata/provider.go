// Package metadata defines the swappable MetadataProvider abstraction (ADR 0005)
// and the gateway that fronts it with a global rate limiter and the metadata_cache
// (architecture.md). ComicVine is the first real implementation; a fixture-backed
// fake satisfies the same interface for tests so CI needs no ComicVine API key (D-10).
package metadata

import "context"

// SeriesResult is a search candidate — enough to disambiguate same-title volumes
// (D-09): name, start year, publisher, issue count, and a cover thumbnail URL.
type SeriesResult struct {
	ComicvineVolumeID int64
	Name              string
	StartYear         int32
	Publisher         string
	CountOfIssues     int32
	CoverURL          string
	Description       string
}

// PublisherRef is a publisher reference attached to a volume.
type PublisherRef struct {
	ComicvineID int64
	Name        string
}

// ArcRef is a story-arc reference attached to a volume/issue.
type ArcRef struct {
	ComicvineID int64
	Name        string
}

// VolumeDetail is the full volume metadata needed to create a series and drive import.
type VolumeDetail struct {
	ComicvineVolumeID int64
	Name              string
	StartYear         int32
	Description       string
	CountOfIssues     int32
	Publisher         PublisherRef
	Arcs              []ArcRef
	CoverURL          string
	// DateLastUpdated is CV's date_last_updated, stored as the conditional-refresh
	// marker (META-05, used in Phase 3).
	DateLastUpdated string
}

// IssueDetail is a single issue's metadata. IssueNumber is the raw display form; the
// service layer normalizes it into the (sort, qualifier) model (SER-05).
type IssueDetail struct {
	ComicvineIssueID int64
	IssueNumber      string
	Title            string
	CoverDate        string
	StoreDate        string
	CoverURL         string
	Arcs             []ArcRef
}

// MetadataProvider is the swappable metadata source (ADR 0005). Methods returning raw
// bytes do so the gateway can cache the exact provider payload (metadata_cache).
//
//nolint:revive // ADR 0005 + the plan name this interface MetadataProvider exactly; the metadata.MetadataProvider stutter is the deliberate, contract-mandated name.
type MetadataProvider interface {
	// SearchSeries returns candidate volumes for a free-text query (META-01).
	SearchSeries(ctx context.Context, query string) ([]SeriesResult, error)
	// GetVolume returns a volume's detail plus the raw payload for caching.
	GetVolume(ctx context.Context, volumeID int64) (VolumeDetail, []byte, error)
	// ListIssues returns one page (offset, 100 per page) of issues for a volume,
	// whether more pages remain, and the raw payload for caching.
	ListIssues(ctx context.Context, volumeID int64, offset int) (issues []IssueDetail, hasMore bool, raw []byte, err error)
	// GetCover fetches cover image bytes from a provider image URL.
	GetCover(ctx context.Context, url string) ([]byte, error)
}

// pageSize is ComicVine's fixed result page size for issue pagination.
const pageSize = 100
