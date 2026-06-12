package series

import "fmt"

// IssueStatus is the typed issue lifecycle state, backed by the string
// values stored in issues.status.
type IssueStatus string

// The seven issue statuses.
const (
	StatusWanted     IssueStatus = "Wanted"
	StatusSnatched   IssueStatus = "Snatched"
	StatusDownloaded IssueStatus = "Downloaded"
	StatusArchived   IssueStatus = "Archived"
	StatusSkipped    IssueStatus = "Skipped"
	StatusFailed     IssueStatus = "Failed"
	StatusIgnored    IssueStatus = "Ignored"
)

// legalTransitions encodes the legal issue-status edges. Failed→Wanted is
// special-cased by the attempt cap in CanTransition.
var legalTransitions = map[IssueStatus]map[IssueStatus]bool{
	StatusWanted:     {StatusSnatched: true, StatusSkipped: true, StatusIgnored: true},
	StatusSnatched:   {StatusDownloaded: true, StatusFailed: true},
	StatusDownloaded: {StatusArchived: true, StatusFailed: true},
	StatusFailed:     {StatusWanted: true, StatusIgnored: true},
	StatusSkipped:    {StatusWanted: true},
	StatusIgnored:    {StatusWanted: true},
	StatusArchived:   {StatusWanted: true},
}

// CanTransition reports whether moving from -> to is legal. The
// Failed→Wanted edge is additionally gated by the (search) attempt cap (loop prevention):
// once attempts reach the cap, the issue stays Failed pending user action.
func CanTransition(from, to IssueStatus, attempts, attemptCap int) bool {
	if !legalTransitions[from][to] {
		return false
	}
	if from == StatusFailed && to == StatusWanted && attempts >= attemptCap {
		return false
	}
	return true
}

// CanTransitionWithCaps reports whether from -> to is legal under BOTH the search-attempt
// cap and the SEPARATE download-attempt cap (D-13/D-14). The Failed→Wanted edge is blocked
// when EITHER counter has reached its cap: a search-exhausted issue goes cold, and a
// download-exhausted issue parks in cool-off (until a manual retry resets download_attempts).
// Other edges are unaffected. This is the gate the replacement loop consults so a failed
// download cannot re-search indefinitely.
func CanTransitionWithCaps(from, to IssueStatus, searchAttempts, searchCap, downloadAttempts, downloadCap int) bool {
	if !CanTransition(from, to, searchAttempts, searchCap) {
		return false
	}
	if from == StatusFailed && to == StatusWanted && downloadAttempts >= downloadCap {
		return false
	}
	return true
}

// Transition validates from -> to and returns the new status or an error if the edge
// is illegal (or blocked by the attempt cap).
func Transition(from, to IssueStatus, attempts, attemptCap int) (IssueStatus, error) {
	if !CanTransition(from, to, attempts, attemptCap) {
		return from, fmt.Errorf("illegal issue status transition %s -> %s (attempts=%d cap=%d)", from, to, attempts, attemptCap)
	}
	return to, nil
}

// DownloadStatus is the typed download lifecycle state, backed by the string values stored
// in downloads.status (the CHECK constraint in 0001_init). It is distinct from IssueStatus:
// a download row tracks one grab attempt, an issue tracks the work item.
type DownloadStatus string

// The five download statuses (downloads.status CHECK).
const (
	DownloadQueued      DownloadStatus = "Queued"
	DownloadDownloading DownloadStatus = "Downloading"
	DownloadCompleted   DownloadStatus = "Completed"
	DownloadFailed      DownloadStatus = "Failed"
	DownloadBlacklisted DownloadStatus = "Blacklisted"
)

// legalDownloadTransitions encodes the legal download-status edges. A grab starts Queued,
// progresses to Downloading, and ends Completed or Failed; either active state can also
// fail outright (explicit client failure or stall), and a user can Blacklist an active or
// failed release. Completed is terminal. Every downloads.status flip in Phase 5 routes
// through TransitionDownload, never a raw UPDATE downloads SET status.
var legalDownloadTransitions = map[DownloadStatus]map[DownloadStatus]bool{
	DownloadQueued:      {DownloadDownloading: true, DownloadCompleted: true, DownloadFailed: true, DownloadBlacklisted: true},
	DownloadDownloading: {DownloadCompleted: true, DownloadFailed: true, DownloadBlacklisted: true},
	DownloadFailed:      {DownloadBlacklisted: true},
	DownloadCompleted:   {},
	DownloadBlacklisted: {},
}

// CanTransitionDownload reports whether moving a download row from -> to is legal.
func CanTransitionDownload(from, to DownloadStatus) bool {
	return legalDownloadTransitions[from][to]
}

// TransitionDownload validates a download-status edge and returns the new status or an
// error if the edge is illegal (e.g. Completed -> Downloading).
func TransitionDownload(from, to DownloadStatus) (DownloadStatus, error) {
	if !CanTransitionDownload(from, to) {
		return from, fmt.Errorf("illegal download status transition %s -> %s", from, to)
	}
	return to, nil
}
