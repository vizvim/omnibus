package series

import (
	"math"

	"github.com/vizvim/omnibus/internal/repository"
)

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
// imports only this service package — never repository/provider/db (PLAT-05 layer rule).

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
	HasCover         bool
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
		HasCover:         hasCover,
	}
}

func arcFromRow(r repository.StoryArc) StoryArc {
	return StoryArc{ID: r.ID, ComicvineArcID: r.ComicvineArcID.Int64, Name: r.Name}
}
