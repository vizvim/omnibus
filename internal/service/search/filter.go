// Package search holds the correctness core of the search phase (D-01..D-07): a set of
// pure functions over []indexer.Candidate — a Stage-1 hard filter (reject with
// structured reasons), a Stage-2 weighted score, and an acceptance-floor gate. The one
// pipeline is shared by manual search, scheduled auto-search, and RSS (D-15). It depends
// only on internal/provider/indexer (Candidate) and internal/service/series
// (Normalize/Key) — no DB, no transport.
package search

import (
	"strings"

	"github.com/vizvim/omnibus/internal/provider/indexer"
	"github.com/vizvim/omnibus/internal/service/series"
)

// RejectReason is a structured reason a candidate was rejected by the hard filter (D-04
// — every reject is observable, unlike Mylar3).
type RejectReason string

// Reject reasons.
const (
	ReasonWrongIssue  RejectReason = "wrong-issue"
	ReasonBlacklisted RejectReason = "blacklisted"
	ReasonIgnoreWord  RejectReason = "ignore-word"
	ReasonSize        RejectReason = "size"
	ReasonFormat      RejectReason = "format"
)

// IssueMatch is the target issue's normalized identity (sort key + qualifier). A
// candidate is accepted by the issue-match gate only when its parsed number normalizes
// to the same series.Key.
type IssueMatch struct {
	Sort float64
	Qual string
}

// BlacklistSet is a set of "provider|release_key" identities to reject.
type BlacklistSet map[string]struct{}

// Has reports whether a candidate's provider|release_key is blacklisted.
func (b BlacklistSet) Has(c indexer.Candidate) bool {
	if b == nil {
		return false
	}
	_, ok := b[blacklistKey(c.Provider, c.ReleaseKey)]
	return ok
}

// NewBlacklistSet builds a BlacklistSet from provider|release_key pairs.
func NewBlacklistSet(pairs ...[2]string) BlacklistSet {
	set := make(BlacklistSet, len(pairs))
	for _, p := range pairs {
		set[blacklistKey(p[0], p[1])] = struct{}{}
	}
	return set
}

func blacklistKey(provider, releaseKey string) string {
	return provider + "|" + releaseKey
}

// FilterOpts are the hard-filter knobs (fixed code defaults this phase, D-05).
type FilterOpts struct {
	IgnoreWords    []string
	MinSizeBytes   int64
	MaxSizeBytes   int64
	AllowedFormats []string
}

// DefaultFilterOpts are the comic-aware defaults: a 1MB–500MB size band, cbz/cbr/pdf
// formats, and a starter ignore-word list (D-05/D-06/D-07).
func DefaultFilterOpts() FilterOpts {
	return FilterOpts{
		IgnoreWords:    []string{"sample", "trailer"},
		MinSizeBytes:   1 * 1024 * 1024,
		MaxSizeBytes:   500 * 1024 * 1024,
		AllowedFormats: []string{"cbz", "cbr", "pdf"},
	}
}

// FilterResult pairs a candidate with the hard-filter verdict.
type FilterResult struct {
	Candidate indexer.Candidate
	Rejected  bool
	Reason    RejectReason
}

// HardFilter applies the Stage-1 gates in order (issue match → blacklist → ignore-words
// → size → format) and returns a verdict per candidate. Every reject carries a reason.
func HardFilter(cands []indexer.Candidate, target IssueMatch, bl BlacklistSet, opts FilterOpts) []FilterResult {
	out := make([]FilterResult, 0, len(cands))
	for _, c := range cands {
		reason, rejected := rejectReasonFor(c, target, bl, opts)
		out = append(out, FilterResult{Candidate: c, Rejected: rejected, Reason: reason})
	}
	return out
}

// rejectReasonFor returns the first gate a candidate fails, or ("", false) if it passes.
func rejectReasonFor(c indexer.Candidate, target IssueMatch, bl BlacklistSet, opts FilterOpts) (RejectReason, bool) {
	// (1) Issue match via the shared normalizer (pitfall #3 — "7" never satisfies "7.INH").
	candSort, candQual := series.Normalize(c.IssueNumberRaw)
	if series.Key(candSort, candQual) != series.Key(target.Sort, target.Qual) {
		return ReasonWrongIssue, true
	}
	// (2) Blacklist.
	if bl.Has(c) {
		return ReasonBlacklisted, true
	}
	// (3) Ignore-words (case-insensitive whole-token match, D-06).
	if hasIgnoreWord(c.Title, opts.IgnoreWords) {
		return ReasonIgnoreWord, true
	}
	// (4) Size band — unknown size (0) passes (D-07).
	if c.SizeBytes > 0 {
		if opts.MinSizeBytes > 0 && c.SizeBytes < opts.MinSizeBytes {
			return ReasonSize, true
		}
		if opts.MaxSizeBytes > 0 && c.SizeBytes > opts.MaxSizeBytes {
			return ReasonSize, true
		}
	}
	// (5) Format — only reject a known format not in the allow-list (unknown "" passes).
	if c.Format != "" && !containsFold(opts.AllowedFormats, c.Format) {
		return ReasonFormat, true
	}
	return "", false
}

// tokenSeparators splits a release title into tokens for ignore-word matching.
//
//nolint:gosec // G101 false positive: this is a set of title delimiter characters, not a credential.
const tokenSeparators = "._-[]() \t\n"

// hasIgnoreWord reports whether any whole token of the title equals an ignore-word
// (both lowercased).
func hasIgnoreWord(title string, ignoreWords []string) bool {
	if len(ignoreWords) == 0 {
		return false
	}
	tokens := strings.FieldsFunc(title, func(r rune) bool {
		return strings.ContainsRune(tokenSeparators, r)
	})
	for _, tok := range tokens {
		for _, w := range ignoreWords {
			if strings.EqualFold(tok, w) {
				return true
			}
		}
	}
	return false
}

// containsFold reports whether list contains v (case-insensitive).
func containsFold(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

// Survivors returns the candidates from a filter pass that were not rejected.
func Survivors(results []FilterResult) []indexer.Candidate {
	out := make([]indexer.Candidate, 0, len(results))
	for _, r := range results {
		if !r.Rejected {
			out = append(out, r.Candidate)
		}
	}
	return out
}
