package indexer

import (
	"regexp"
	"strings"
)

// issueNumberRe extracts a best-effort issue number from a release title. It matches a
// "#NNN" form or a bare number following the series name, including decimal issues.
// The service does the authoritative match via series.Normalize — this is only a hint.
var issueNumberRe = regexp.MustCompile(`#\s*(\d+(?:\.\d+)?)|(?:\s)(\d{1,4}(?:\.\d+)?)(?:\s|$|\.cbz|\.cbr|\.pdf)`)

// parseIssueNumber returns the best-effort issue number from a title, or "" if none is
// confidently found.
func parseIssueNumber(title string) string {
	m := issueNumberRe.FindStringSubmatch(title)
	if m == nil {
		return ""
	}
	if m[1] != "" {
		return m[1]
	}
	return strings.TrimSpace(m[2])
}

// inferFormat returns the container format implied by the title extension, or "".
func inferFormat(title string) string {
	lower := strings.ToLower(title)
	switch {
	case strings.Contains(lower, ".cbz"):
		return "cbz"
	case strings.Contains(lower, ".cbr"):
		return "cbr"
	case strings.Contains(lower, ".pdf"):
		return "pdf"
	default:
		return ""
	}
}

// looksLikePack reports whether the title indicates a volume/pack rather than a single
// issue (so downstream completeness scoring can prefer single issues for a wanted one).
func looksLikePack(title string) bool {
	lower := strings.ToLower(title)
	for _, marker := range []string{"vol.", "volume", "tpb", "complete", "collection", "annual pack", "(pack)"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
