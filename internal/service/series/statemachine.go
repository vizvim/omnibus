package series

import "fmt"

// IssueStatus is the typed issue lifecycle state (ADR 0004), backed by the string
// values stored in issues.status.
type IssueStatus string

// The seven issue statuses (schema.md / ADR 0004).
const (
	StatusWanted     IssueStatus = "Wanted"
	StatusSnatched   IssueStatus = "Snatched"
	StatusDownloaded IssueStatus = "Downloaded"
	StatusArchived   IssueStatus = "Archived"
	StatusSkipped    IssueStatus = "Skipped"
	StatusFailed     IssueStatus = "Failed"
	StatusIgnored    IssueStatus = "Ignored"
)

// legalTransitions encodes ADR 0004's legal issue-status edges. Failed→Wanted is
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

// CanTransition reports whether moving from -> to is legal per ADR 0004. The
// Failed→Wanted edge is additionally gated by the attempt cap (loop prevention): once
// attempts reach the cap, the issue stays Failed pending user action.
func CanTransition(from, to IssueStatus, attempts, attemptCap int) bool {
	if !legalTransitions[from][to] {
		return false
	}
	if from == StatusFailed && to == StatusWanted && attempts >= attemptCap {
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
