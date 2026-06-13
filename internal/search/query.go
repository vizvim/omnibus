package search

import (
	"strconv"
	"strings"
)

// Issue-type discriminators (mirrors the issues.issue_type CHECK values) that select the
// query-composition shape in buildQueries.
const (
	issueTypeAnnual  = "annual"
	issueTypeOneShot = "one-shot"
)

// cleanSeriesName normalizes a series name into a search-friendly term, mylar-style:
// it replaces "/" and "-" with spaces, removes the stop-words "the" and "and" on word
// boundaries (case-insensitive), strips the punctuation "& : ? ,", collapses runs of
// whitespace, and trims. It does NOT url-encode — the indexer provider encodes the q=
// parameter itself.
func cleanSeriesName(name string) string {
	s := strings.NewReplacer("/", " ", "-", " ").Replace(name)

	// Remove stop-words on word boundaries (case-insensitive).
	fields := strings.Fields(s)
	kept := make([]string, 0, len(fields))
	for _, f := range fields {
		if strings.EqualFold(f, "the") || strings.EqualFold(f, "and") {
			continue
		}
		kept = append(kept, f)
	}
	s = strings.Join(kept, " ")

	// Strip punctuation we never want in a query term.
	s = strings.NewReplacer("&", "", ":", "", "?", "", ",", "").Replace(s)

	// Collapse whitespace and trim.
	return strings.Join(strings.Fields(s), " ")
}

// issueNumberVariants returns the zero-pad variants mylar tries for an issue number,
// widest-pad first. The leading integer is padded by width: 1 digit -> 007/07/7,
// 2 digits -> 023/23, 3+ digits -> the number itself. A decimal pads its integer part
// (7.5 -> 007.5/07.5/7.5). A non-numeric raw value is returned unchanged as the only
// variant.
func issueNumberVariants(raw string) []string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return []string{raw}
	}

	intPart, fracPart := s, ""
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		intPart, fracPart = s[:dot], s[dot:]
		// Only a numeric fraction (a true decimal like 7.5) is paddable. A text
		// qualifier suffix (7.INH) is not a decimal — return the raw value unchanged.
		if _, err := strconv.Atoi(fracPart[1:]); err != nil {
			return []string{raw}
		}
	}

	n, err := strconv.Atoi(intPart)
	if err != nil || n < 0 {
		return []string{raw}
	}

	var pads []int
	switch len(intPart) {
	case 1:
		pads = []int{3, 2, 1}
	case 2:
		pads = []int{3, 2}
	default:
		pads = []int{len(intPart)}
	}

	out := make([]string, 0, len(pads))
	seen := make(map[string]struct{}, len(pads))
	for _, w := range pads {
		v := padInt(n, w) + fracPart
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// padInt renders n left-padded with zeros to at least width digits.
func padInt(n, width int) string {
	s := strconv.Itoa(n)
	if len(s) >= width {
		return s
	}
	return strings.Repeat("0", width-len(s)) + s
}

// buildQueries composes the newznab q= terms for an issue, mylar-style and issue_type
// aware:
//   - one-shot: a single query of just the cleaned series name (no issue number).
//   - annual:   "<clean name> annual <variant>" per numeric variant.
//   - standard: "<clean name> <variant>" per numeric variant.
//
// The returned slice is de-duplicated, preserving first-occurrence order.
func buildQueries(seriesName, issueNumberRaw, issueType string) []string {
	name := cleanSeriesName(seriesName)

	var raw []string
	switch issueType {
	case issueTypeOneShot:
		raw = []string{name}
	case issueTypeAnnual:
		for _, v := range issueNumberVariants(issueNumberRaw) {
			raw = append(raw, name+" annual "+v)
		}
	default:
		for _, v := range issueNumberVariants(issueNumberRaw) {
			raw = append(raw, name+" "+v)
		}
	}

	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, q := range raw {
		if _, dup := seen[q]; dup {
			continue
		}
		seen[q] = struct{}{}
		out = append(out, q)
	}
	return out
}
