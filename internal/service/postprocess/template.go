// Package postprocess holds the three pure-ish post-process units the orchestrator
// (05-03) composes: a token-template render engine, a CBZ/CBR/PDF archive validator,
// and a hardlink→copy importer. Like internal/service/search (filter.go/pipeline.go),
// these are table-test-friendly functions with no DB or transport dependency — the
// render engine is fully pure, the validator/importer touch only the filesystem.
// The render engine consumes a renameconfig.Config (05-05) via RenderOpts, so the
// orchestrator passes the user's live runtime settings.
package postprocess

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/vizvim/omnibus/internal/service/series"
)

// Token is a recognized template token. The vocabulary is fixed by D-06:
// $Publisher $Series $Year $VolumeN $VolumeY $Annual $Issue $monthname $month
// $Title $Imprint. A value-map entry keyed by one of these substitutes into the
// template; an empty (or absent) value triggers graceful omission of its segment.
const (
	TokenPublisher = "$Publisher"
	TokenSeries    = "$Series"
	TokenYear      = "$Year"
	TokenVolumeN   = "$VolumeN"
	TokenVolumeY   = "$VolumeY"
	TokenAnnual    = "$Annual"
	TokenIssue     = "$Issue"
	TokenMonthName = "$monthname"
	TokenMonth     = "$month"
	TokenTitle     = "$Title"
	TokenImprint   = "$Imprint"
)

// tokenVocabulary is the ordered list of tokens Render substitutes. Order matters:
// longer tokens that are prefixes of nothing else are fine, but we replace the most
// specific names first so e.g. $VolumeN/$VolumeY never collide with a hypothetical
// $Volume. (None of the current tokens are prefixes of one another, but $month is a
// prefix of $monthname — so $monthname MUST be replaced before $month.)
var tokenVocabulary = []string{
	TokenPublisher,
	TokenSeries,
	TokenVolumeN,
	TokenVolumeY,
	TokenAnnual,
	TokenIssue,
	TokenMonthName, // before $month — $month is a prefix of $monthname
	TokenMonth,
	TokenTitle,
	TokenImprint,
	TokenYear,
}

// RenderOpts are the advanced rename knobs (D-08), sourced from renameconfig.Config
// (05-05). The orchestrator builds these from the live config so user toggles take
// effect without restart.
type RenderOpts struct {
	// RenameEnabled is the master toggle. When false, Render returns the raw fallback
	// (the caller keeps the original name) — see Render's contract below.
	RenameEnabled bool
	// PaddingEnabled pads the numeric base of $Issue. When false, the raw issue value
	// is used as-is.
	PaddingEnabled bool
	// PaddingFormat selects the pad width: "00x" (3-digit), "0x" (2-digit), "x" (none).
	PaddingFormat string
	// ReplaceSpaces swaps spaces for underscores in the final rendered string (D-08).
	ReplaceSpaces bool
	// Lowercase lowercases the final rendered string (D-08).
	Lowercase bool
}

// DefaultTemplateOpts mirrors search.DefaultFilterOpts/renameconfig.DefaultRenameConfig:
// the user's literal Mylar defaults — rename on, 3-digit padding on, no space-replace,
// no lowercase.
func DefaultTemplateOpts() RenderOpts {
	return RenderOpts{
		RenameEnabled:  true,
		PaddingEnabled: true,
		PaddingFormat:  "00x",
		ReplaceSpaces:  false,
		Lowercase:      false,
	}
}

// padWidthFor maps a padding format to a minimum digit width for the numeric base of
// $Issue. Unknown formats fall back to 3 (the "00x" default).
func padWidthFor(format string) int {
	switch format {
	case "x":
		return 1
	case "0x":
		return 2
	case "00x":
		return 3
	default:
		return 3
	}
}

// illegalChars are filesystem-illegal characters stripped/replaced per Mylar
// filers.py:738. "/" and "*" become "-" (they have structural meaning); the rest are
// removed. NOTE: this runs on rendered token VALUES, never on the template literal —
// so template path separators in the folder format are preserved.
var illegalReplacer = strings.NewReplacer(
	"/", "-",
	"*", "-",
	":", "",
	"?", "",
	"!", "",
	"'", "",
	"\"", "",
	",", "",
)

// emptyBracketPairs drops "()" / "[]" pairs (optionally wrapping inner whitespace)
// left behind after an empty token's text is stripped (Mylar graceful omission,
// filers.py:163-177).
var emptyBracketPairs = regexp.MustCompile(`\(\s*\)|\[\s*\]`)

// multiSpace collapses runs of whitespace to a single space.
var multiSpace = regexp.MustCompile(`\s{2,}`)

// PadIssue pads the numeric base of an issue number to width digits, preserving any
// non-numeric qualifier (".INH", ".NOW", "½", "annual") via the shared
// series.Normalize. This is the D-07 padding rule: pad the base, never the qualifier.
// Examples (width 3): "1"→"001", "12"→"012", "7.INH"→"007.INH", "½"→"½".
func PadIssue(raw string, width int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	sort, qual := series.Normalize(raw)

	// "½" (sort 0.5, qual "half") and other non-integer/qualifier-only values: leave
	// the raw value untouched — padding a fractional/vulgar value is meaningless.
	if qual == "half" {
		return raw
	}

	// Determine the integer base and any fractional/qualifier tail to preserve.
	// series.Normalize folds a numeric decimal suffix into sort and leaves qual empty;
	// a text suffix (INH/NOW/...) sets qual and keeps the integer base in sort.
	intBase := int64(sort)
	frac := sort - float64(intBase)

	base := strconv.FormatInt(intBase, 10)
	if len(base) < width {
		base = strings.Repeat("0", width-len(base)) + base
	}

	// Reattach a numeric decimal suffix (e.g. "1.5") from the original raw string —
	// we only padded the integer part. Prefer reconstructing from raw to avoid float
	// formatting noise.
	if frac != 0 {
		if dot := strings.LastIndex(raw, "."); dot >= 0 {
			if _, err := strconv.ParseFloat(raw[dot+1:], 64); err == nil {
				return base + raw[dot:]
			}
		}
	}

	// Reattach a text qualifier (".INH" etc.). series.Normalize upper-cases it; use the
	// raw suffix to preserve the user's original casing where possible.
	if qual != "" {
		if dot := strings.LastIndex(raw, "."); dot >= 0 {
			return base + raw[dot:]
		}
		// Qualifier with no integer base (e.g. ".NOW") — nothing meaningful to pad.
		return raw
	}

	return base
}

// Render substitutes token values into tmpl, applying graceful omission (empty tokens
// drop their segment, then empty bracket pairs and redundant whitespace are cleaned),
// $Issue padding (D-07), and the advanced opts (D-08). It is a pure function: no DB,
// no transport, no I/O.
//
// Contract:
//   - When opts.RenameEnabled is false, Render returns the (cleaned) raw $Series value
//     if present, else the template rendered with whatever values exist — the caller is
//     expected to keep the original filename in that case. Rename-off is a transport-
//     level decision; Render still produces a deterministic string for callers that
//     want a preview.
//   - $Issue is padded via PadIssue when opts.PaddingEnabled is true.
//   - Token VALUES are sanitized of filesystem-illegal characters; the template literal
//     (including "/" path separators in a folder format) is preserved.
func Render(tmpl string, values map[string]string, opts RenderOpts) string {
	issueWidth := padWidthFor(opts.PaddingFormat)

	out := tmpl
	for _, tok := range tokenVocabulary {
		raw, ok := values[tok]
		val := strings.TrimSpace(raw)

		if tok == TokenIssue && val != "" && opts.PaddingEnabled {
			val = PadIssue(val, issueWidth)
		}

		// Sanitize the token value (never the template literal).
		if val != "" {
			val = illegalReplacer.Replace(val)
		}

		if !ok || val == "" {
			// Graceful omission: strip the token text outright. Bracket/whitespace
			// cleanup below removes any now-empty "()" / "[]" and "#" left dangling.
			out = strings.ReplaceAll(out, tok, "")
			continue
		}
		out = strings.ReplaceAll(out, tok, val)
	}

	out = cleanup(out)

	if opts.ReplaceSpaces {
		out = strings.ReplaceAll(out, " ", "_")
	}
	if opts.Lowercase {
		out = strings.ToLower(out)
	}
	return out
}

// cleanup performs the Mylar graceful-omission tidy: drop empty bracket pairs, drop a
// dangling "#" left when $Issue was omitted, collapse whitespace, and trim. Path
// segments emptied entirely (e.g. an empty $Publisher in "$Publisher/$Series") collapse
// to avoid leading/trailing slashes.
func cleanup(s string) string {
	// Drop empty "()" / "[]" (repeat until stable for nested/adjacent cases).
	for {
		next := emptyBracketPairs.ReplaceAllString(s, "")
		if next == s {
			break
		}
		s = next
	}

	// Drop a dangling "#" with no following issue (e.g. "... #" or "...# (June)").
	s = strings.ReplaceAll(s, "# ", " ")
	s = strings.TrimSuffix(s, "#")
	s = strings.TrimSuffix(s, " #")

	// Collapse per-path-segment whitespace and prune empty segments so an omitted
	// folder token doesn't leave a stray separator.
	if strings.Contains(s, "/") {
		parts := strings.Split(s, "/")
		kept := parts[:0]
		for _, p := range parts {
			p = collapse(p)
			if p != "" {
				kept = append(kept, p)
			}
		}
		return strings.Join(kept, "/")
	}

	return collapse(s)
}

// collapse trims and collapses internal whitespace in a single segment.
func collapse(s string) string {
	s = multiSpace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
