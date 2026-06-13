// Package indexer defines the swappable IndexerProvider abstraction (ADR 0005) and a
// per-host rate-pacing gateway that fronts it. Newznab (NZB) and GetComics (DDL) are
// the first real implementations; a fixture-backed FakeProvider satisfies the same
// interface so CI never hits a live indexer.
//
// This layer only fetches and normalizes releases to the shared Candidate type. It
// does NOT score, filter, or match issue numbers — that is the search service's job
// (Plan 03). Providers parse a best-effort raw issue number from the release title;
// the service is responsible for matching it via series.Normalize.
package indexer

import "context"

// Indexer kinds reported by a provider's Kind(). NewznabKind is the only value stored in
// indexers.kind (05-07). GetComicsKind is NOT an indexer-row kind anymore: the GetComics
// provider still reports Kind()=="getcomics" because it remains the built-in, config-gated
// DDL source (toggled by enable_ddl, base URL = the static defaultGetComicsBaseURL), but it
// is never constructed from an indexer row.
const (
	NewznabKind   = "newznab"
	GetComicsKind = "getcomics"
)

// Candidate is one release returned by an IndexerProvider, normalized across sources.
// It is the provider-layer struct; the proto Candidate (api-spec) is the transport
// form, mapped provider -> service -> transport (like metadata.SeriesResult).
type Candidate struct {
	// Provider identifies the source (the provider Kind: "newznab"/"getcomics").
	Provider string
	// ReleaseKey is the stable identity for dedup + blacklist (Newznab guid /
	// GetComics post URL). It is never empty.
	ReleaseKey string
	// Title is the raw release title as published by the indexer.
	Title string
	// IssueNumberRaw is a best-effort issue number parsed from the title. The service
	// normalizes + matches it (provider parses, service matches).
	IssueNumberRaw string
	// SizeBytes is the release size; 0 means unknown (not a rejection signal).
	SizeBytes int64
	// DownloadURL is the NZB url (Newznab) or the post/mirror url (GetComics). Mirror
	// resolution for DDL happens at grab time (Plan 04), not here.
	DownloadURL string
	// Format is the inferred container ("cbz"/"cbr"/"pdf"/""), best-effort from title.
	Format string
	// IsPack is true when the release looks like a volume/pack rather than a single issue.
	IsPack bool
}

// IndexerProvider is the swappable search source (ADR 0005). Search runs a targeted
// query; Feed returns the recent-uploads RSS feed (same normalization). Kind reports
// the provider type ("newznab"/"getcomics"). Implementations must NOT rate-limit
// internally — pacing lives in the Gateway.
//
//nolint:revive // IndexerProvider is the deliberate, canonical contract name (ADR 0005 / plan 04-02); the package-qualified stutter is intentional and mirrors metadata.MetadataProvider.
type IndexerProvider interface {
	// Search returns candidates for a free-text query.
	Search(ctx context.Context, query string) ([]Candidate, error)
	// Feed returns the indexer's recent-uploads feed as candidates.
	Feed(ctx context.Context) ([]Candidate, error)
	// Kind reports the provider type.
	Kind() string
}
