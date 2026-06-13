package series

import (
	"html"
	"math"
	"regexp"
	"strings"

	"github.com/vizvim/omnibus/internal/repository"
)

// htmlTagRE matches a single HTML/XML tag (e.g. <p>, </b>, <br/>, <a href="...">).
var htmlTagRE = regexp.MustCompile(`<[^>]*>`)

// wsRE collapses any run of whitespace (including the newlines that tag removal can
// leave behind) into a single space.
var wsRE = regexp.MustCompile(`\s+`)

// spaceBeforePunctRE matches a space sitting immediately before sentence punctuation,
// which tag removal (tags replaced by a space) can introduce, e.g. "<i>x</i>." -> "x .".
var spaceBeforePunctRE = regexp.MustCompile(` +([.,;:!?])`)

// stripHTML renders ComicVine's HTML description as plain text: it removes tags,
// unescapes HTML entities, and collapses whitespace. It is deliberately minimal — the
// descriptions are short, trusted-source summaries, not arbitrary user HTML — so a
// regex tag-strip plus stdlib entity unescaping is sufficient and dependency-free.
func stripHTML(s string) string {
	if s == "" {
		return ""
	}
	out := htmlTagRE.ReplaceAllString(s, " ")
	out = html.UnescapeString(out)
	out = wsRE.ReplaceAllString(out, " ")
	out = spaceBeforePunctRE.ReplaceAllString(out, "$1")
	return strings.TrimSpace(out)
}

// toInt32 clamps an int64 into int32 range. Series/issue counts and years are far
// within int32, but clamping keeps gosec (G115) satisfied and is defensive.
func toInt32(v int64) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}

// The series package owns the domain types the transport layer consumes, so transport
// imports only this service package — never repository/provider/db.

// Series is the domain view of a watched series.
type Series struct {
	ID                int64
	ComicvineVolumeID int64
	Name              string
	StartYear         int32
	Publisher         string
	Status            string
	HasCover          bool
	TotalIssues       int32
	HaveIssues        int32
	LastRefreshedAt   string
}

// Issue is the domain view of a single issue.
type Issue struct {
	ID               int64
	SeriesID         int64
	ComicvineIssueID int64
	IssueNumber      string
	IssueNumberSort  float64
	IssueNumberQual  string
	Title            string
	CoverDate        string
	Status           string
	IssueType        string
	HasCover         bool
}

// Credit is the domain view of a normalized per-issue creator credit, ordered by
// (role, name) within an IssueDetail.
type Credit struct {
	Role              string
	Name              string
	ComicvinePersonID int64
}

// IssueDetail is the rich per-issue view fetched on demand: the base Issue plus the
// summary, extra dates, and the ordered creator credits.
type IssueDetail struct {
	Issue          Issue
	Description    string
	IssueType      string
	AltIssueNumber string
	PageCount      int32
	StoreDate      string
	CVLastUpdated  string
	Credits        []Credit
}

// StoryArc is the domain view of a story arc.
type StoryArc struct {
	ID             int64
	ComicvineArcID int64
	Name           string
}

func seriesFromRow(r repository.Series, publisher string, hasCover bool) Series {
	return Series{
		ID:                r.ID,
		ComicvineVolumeID: r.ComicvineVolumeID,
		Name:              r.Name,
		StartYear:         toInt32(r.StartYear.Int64),
		Publisher:         publisher,
		Status:            r.Status,
		HasCover:          hasCover,
		TotalIssues:       toInt32(r.TotalIssues),
		HaveIssues:        toInt32(r.HaveIssues),
		LastRefreshedAt:   r.LastRefreshedAt.String,
	}
}

func issueFromRow(r repository.Issue, hasCover bool) Issue {
	return Issue{
		ID:               r.ID,
		SeriesID:         r.SeriesID,
		ComicvineIssueID: r.ComicvineIssueID,
		IssueNumber:      r.IssueNumberRaw,
		IssueNumberSort:  r.IssueNumberSort,
		IssueNumberQual:  r.IssueNumberQual.String,
		Title:            r.Title.String,
		CoverDate:        r.CoverDate.String,
		Status:           r.Status,
		IssueType:        r.IssueType,
		HasCover:         hasCover,
	}
}

func arcFromRow(r repository.StoryArc) StoryArc {
	return StoryArc{ID: r.ID, ComicvineArcID: r.ComicvineArcID.Int64, Name: r.Name}
}
