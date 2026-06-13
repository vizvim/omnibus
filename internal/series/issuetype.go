package series

import (
	"regexp"
	"strings"
)

// Issue-type and qualifier string constants shared across the series package.
const (
	issueTypeStandard = "standard"
	issueTypeAnnual   = "annual"
	issueTypeOneShot  = "one-shot"
)

// annualWord matches the whole word "annual" (case-insensitive) anywhere in a string.
var annualWord = regexp.MustCompile(`(?i)\bannual\b`)

// DeriveIssueType classifies an issue as "annual", "one-shot", or "standard" from its
// raw issue number, title, and the size of the series:
//  1. annual   — the normalized issue number carries the "annual" qualifier, or the
//     raw number/title contains the whole word "annual" (case-insensitive).
//  2. one-shot — the series has exactly one issue.
//  3. standard — everything else.
func DeriveIssueType(issueNumberRaw, title string, seriesIssueCount int) string {
	if _, qual := Normalize(issueNumberRaw); strings.EqualFold(qual, issueTypeAnnual) {
		return issueTypeAnnual
	}
	if annualWord.MatchString(issueNumberRaw) || annualWord.MatchString(title) {
		return issueTypeAnnual
	}
	if seriesIssueCount == 1 {
		return issueTypeOneShot
	}
	return issueTypeStandard
}
