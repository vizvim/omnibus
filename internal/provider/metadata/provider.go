// Package metadata defines the swappable MetadataProvider abstraction and the
// gateway that fronts it with a global rate limiter and the metadata_cache.
// ComicVine is the first real implementation; a fixture-backed fake satisfies the
// same interface for tests so CI needs no ComicVine API key.
package metadata

import "context"

// SeriesResult is a search candidate — enough to disambiguate same-title volumes
// by name, start year, publisher, issue count, and a cover thumbnail URL.
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
	// marker.
	DateLastUpdated string
}

// IssueDetail is a single issue's metadata. IssueNumber is the raw display form; the
// service layer normalizes it into the (sort, qualifier) model.
type IssueDetail struct {
	ComicvineIssueID int64
	IssueNumber      string
	Title            string
	CoverDate        string
	StoreDate        string
	CoverURL         string
	Arcs             []ArcRef
}

// MetadataProvider is the swappable metadata source. Methods returning raw
// bytes do so the gateway can cache the exact provider payload (metadata_cache).
//
//nolint:revive // MetadataProvider is the deliberate, canonical contract name for this interface; the metadata.MetadataProvider package-qualified stutter is intentional.
type MetadataProvider interface {
	// SearchSeries returns candidate volumes for a free-text query.
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
