// Package download provides the concrete download-client providers used to hand a chosen
// release off to a download client and to track it: SABnzbd (NZB submit + pull-based status
// polling + history removal, D-01) and GetComics (DDL streaming). Consumers depend on narrow,
// consumer-owned interfaces — search.Submitter (Submit), tracking.Poller (Poll),
// tracking.DDLFetcher (Fetch), and postprocess.HistoryRemover (RemoveFromHistory) — while this
// package exports the concrete providers plus the shared request/result value types
// (GrabRequest, PollResult) they exchange. A FakeProvider satisfies those consumer seams for
// service tests.
package download

// GrabRequest is the handoff payload for a single release.
type GrabRequest struct {
	Title       string
	DownloadURL string
	ReleaseKey  string
	SizeBytes   int64
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
