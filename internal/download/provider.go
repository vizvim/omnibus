// Package download defines the swappable DownloadProvider abstraction (ADR 0005) used to
// hand a chosen release off to a download client. SABnzbd (NZB submit) and GetComics DDL
// are the first implementations; a fake satisfies the same interface for service tests.
//
// Submit hands a release off and returns a client reference. Tracking (status polling,
// progress, failure detection) is the Tracker sub-interface, implemented for SAB here
// (D-01 pull-based Completed Download Handling). GetComics DDL tracking lands later.
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
// reference (e.g. a SABnzbd nzo_id or a DDL job id) used later to track it.
//
//nolint:revive // DownloadProvider is the deliberate, canonical contract name (ADR 0005); the package-qualified stutter is intentional and mirrors metadata.MetadataProvider / indexer.IndexerProvider.
type DownloadProvider interface {
	// Submit hands the release off and returns the client reference, or an error.
	Submit(ctx context.Context, req GrabRequest) (clientRef string, err error)
	// Kind reports the provider type ("sabnzbd"/"getcomics").
	Kind() string
}

// PollStatus is the coarse lifecycle state a Poll reports for one tracked client ref.
type PollStatus string

// The four poll outcomes. NotReady is distinct from terminal states: SAB sometimes reports
// a Completed history entry before the storage path is populated (RESEARCH Pitfall 1), so
// "Completed with empty storage" maps to NotReady (re-poll next tick), NOT a success.
const (
	// PollInProgress: still queued/downloading (or a transient history phase like
	// Moving/Extracting/Verifying/Repairing). Progress may carry a percentage.
	PollInProgress PollStatus = "in-progress"
	// PollCompleted: terminal success with a real StoragePath.
	PollCompleted PollStatus = "completed"
	// PollFailed: terminal failure; FailMessage carries the client's reason.
	PollFailed PollStatus = "failed"
	// PollNotReady: the client returned nothing actionable yet (e.g. Completed-with-empty
	// -storage); the caller should treat it as a no-op and poll again next tick.
	PollNotReady PollStatus = "not-ready"
)

// PollResult is the tracked state of one download client reference at a point in time.
type PollResult struct {
	Status      PollStatus
	ProgressPct float64
	StoragePath string
	FailMessage string
}

// Tracker is the pull-based tracking contract (D-01, the *arr/Mylar "Completed Download
// Handling" standard): poll a client reference for progress + terminal state, and remove a
// completed item from the client after a successful import. SABnzbd implements it; the fake
// satisfies it for service tests. GetComics DDL gains tracking in a later slice.
type Tracker interface {
	// Poll reports the current state of one client reference (an nzo_id for SAB).
	Poll(ctx context.Context, clientRef string) (PollResult, error)
	// RemoveFromHistory removes a completed item from the client (the *arr "Remove
	// Completed" default, D-05). The hard-linked/copied file persists in the library.
	RemoveFromHistory(ctx context.Context, clientRef string) error
}
