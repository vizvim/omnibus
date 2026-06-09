// Package download defines the swappable DownloadProvider abstraction (ADR 0005) used to
// hand a chosen release off to a download client. SABnzbd (NZB submit) and GetComics DDL
// are the first implementations; a fake satisfies the same interface for service tests.
//
// This phase covers the SUBMIT half only (D-14): Submit hands off and returns a client
// reference. Status polling, progress, and failure detection are Phase 5.
package download

import "context"

// GrabRequest is the handoff payload for a single release.
type GrabRequest struct {
	Title       string
	DownloadURL string
	ReleaseKey  string
	SizeBytes   int64
}

// DownloadProvider hands a release off to a download client and returns a client-side
// reference (e.g. a SABnzbd nzo_id or a DDL job id) used later (Phase 5) to track it.
//
//nolint:revive // DownloadProvider is the deliberate, canonical contract name (ADR 0005); the package-qualified stutter is intentional and mirrors metadata.MetadataProvider / indexer.IndexerProvider.
type DownloadProvider interface {
	// Submit hands the release off and returns the client reference, or an error.
	Submit(ctx context.Context, req GrabRequest) (clientRef string, err error)
	// Kind reports the provider type ("sabnzbd"/"getcomics").
	Kind() string
}
