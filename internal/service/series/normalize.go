// Package series holds the SeriesService domain logic: the issue-number normalizer,
// the issue-status state machine, the search/add/view service, and the bounded
// import orchestration. It depends only on repository interfaces and the metadata
// gateway — never on transport or concrete SQL.
package series

import (
	"fmt"
	"strconv"
	"strings"
)

// Normalize maps a raw ComicVine issue-number string to a sortable numeric key and a
// qualifier, per the three-field model. Decimal suffixes fold into
// the fractional part of sort; pure-text qualifiers (INH, NOW, annual, half) keep the
// integer base in sort and set qual. This guarantees "7" (7.0,"") and "7.INH"
// (7.0,"INH") are distinct — pitfall #3.
func Normalize(raw string) (sortKey float64, qual string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, ""
	}

	// Vulgar-fraction one-halves.
	if s == "½" || strings.EqualFold(s, "1/2") {
		return 0.5, "half"
	}

	// "Annual N" / "Annual" -> (N or 0, "annual").
	if lower := strings.ToLower(s); strings.HasPrefix(lower, "annual") {
		rest := strings.TrimSpace(s[len("annual"):])
		n, err := strconv.ParseFloat(rest, 64)
		if err != nil {
			return 0, issueTypeAnnual
		}
		return n, issueTypeAnnual
	}

	// "<base>.<suffix>" — numeric suffix stays in sort; text suffix becomes qual.
	if dot := strings.LastIndex(s, "."); dot >= 0 {
		base := s[:dot]
		suffix := s[dot+1:]
		if _, err := strconv.ParseFloat(suffix, 64); err != nil && suffix != "" {
			// Text qualifier (e.g. INH, NOW): keep the integer base in sort.
			if b, berr := strconv.ParseFloat(base, 64); berr == nil {
				return b, strings.ToUpper(suffix)
			}
		}
	}

	// ".NOW"-style leading-dot qualifiers (no numeric base).
	if rest, ok := strings.CutPrefix(s, "."); ok {
		return 0, strings.ToUpper(rest)
	}

	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return n, ""
	}

	// Unparseable: sort 0 with the raw string upper-cased as the qualifier so it
	// remains distinct from numeric issues.
	return 0, strings.ToUpper(s)
}

// Key returns the composite uniqueness key for a normalized issue number, mirroring
// the DB's (issue_number_sort, COALESCE(issue_number_qual,”)) unique index.
func Key(sortKey float64, qual string) string {
	return fmt.Sprintf("%g|%s", sortKey, qual)
}
