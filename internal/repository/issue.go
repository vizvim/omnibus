package repository

import (
	"context"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// Issue is the domain view of an issue row.
type Issue = sqlc.Issue

// IssueUpsert carries the fields needed to insert or update an issue, keyed on the
// immutable ComicVine issue id. The three issue-number fields preserve the
// distinctness of e.g. 7 vs 7.INH.
type IssueUpsert struct {
	SeriesID         int64
	ComicvineIssueID int64
	IssueNumberRaw   string
	IssueNumberSort  float64
	IssueNumberQual  string
	Title            string
	CoverDate        string
	StoreDate        string
	ReleaseDate      string
	Description      string
	ImageURL         string
	CVLastUpdated    string
	IssueType        string
	AltIssueNumber   string
	PageCount        int64
	Status           string
	CreatedAt        string
}

// IssueRepository persists issues, pinned to comicvine_issue_id.
type IssueRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewIssueRepository binds an IssueRepository to the read and write pools.
func NewIssueRepository(d *db.DB) *IssueRepository {
	return &IssueRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

func (r *IssueRepository) Upsert(ctx context.Context, in IssueUpsert) (Issue, error) {
	return r.write.UpsertIssue(ctx, sqlc.UpsertIssueParams{
		SeriesID:         in.SeriesID,
		ComicvineIssueID: in.ComicvineIssueID,
		IssueNumberRaw:   in.IssueNumberRaw,
		IssueNumberSort:  in.IssueNumberSort,
		IssueNumberQual:  nullString(in.IssueNumberQual),
		Title:            nullString(in.Title),
		CoverDate:        nullString(in.CoverDate),
		StoreDate:        nullString(in.StoreDate),
		ReleaseDate:      nullString(in.ReleaseDate),
		Description:      nullString(in.Description),
		ImageUrl:         nullString(in.ImageURL),
		CvLastUpdated:    nullString(in.CVLastUpdated),
		IssueType:        defaultIssueType(in.IssueType),
		AltIssueNumber:   nullString(in.AltIssueNumber),
		PageCount:        nullInt64(in.PageCount),
		Status:           in.Status,
		CreatedAt:        in.CreatedAt,
	})
}

// defaultIssueType maps an empty issue-type to the schema default so the NOT NULL
// CHECK column is always satisfied even when a caller leaves the field unset.
func defaultIssueType(t string) string {
	if t == "" {
		return "standard"
	}
	return t
}

func (r *IssueRepository) GetByID(ctx context.Context, id int64) (Issue, error) {
	return r.read.GetIssueByID(ctx, id)
}

func (r *IssueRepository) ListBySeries(ctx context.Context, seriesID int64) ([]Issue, error) {
	return r.read.ListIssuesBySeries(ctx, seriesID)
}

func (r *IssueRepository) CountBySeries(ctx context.Context, seriesID int64) (int64, error) {
	return r.read.CountIssuesBySeries(ctx, seriesID)
}

func (r *IssueRepository) UpdateStatus(ctx context.Context, id int64, status string, searchAttempts int32) error {
	return r.write.UpdateIssueStatus(ctx, sqlc.UpdateIssueStatusParams{
		Status:         status,
		SearchAttempts: int64(searchAttempts),
		ID:             id,
	})
}

// ListWantedForAutoSearch returns a bounded batch of Wanted issues eligible for
// auto-search (fewest search_attempts first, then oldest), excluding issues at or
// above the attempt cap (cold). batch bounds the result (D-08/D-09).
func (r *IssueRepository) ListWantedForAutoSearch(ctx context.Context, attemptCap, batch int32) ([]Issue, error) {
	return r.read.ListWantedForAutoSearch(ctx, sqlc.ListWantedForAutoSearchParams{
		SearchAttempts: int64(attemptCap),
		Limit:          int64(batch),
	})
}

// IncrementSearchAttempts records a no-result auto-search attempt (D-09 backoff).
func (r *IssueRepository) IncrementSearchAttempts(ctx context.Context, id int64) error {
	return r.write.IncrementSearchAttempts(ctx, id)
}

// IncrementDownloadAttempts records a failed-grab / replacement cycle (D-13). It bumps
// the SEPARATE download_attempts counter, distinct from search_attempts.
func (r *IssueRepository) IncrementDownloadAttempts(ctx context.Context, id int64) error {
	return r.write.IncrementDownloadAttempts(ctx, id)
}

// ResetDownloadAttempts clears the download_attempts cool-off so a manual retry can
// re-run the pipeline immediately (DL-07 / D-14).
func (r *IssueRepository) ResetDownloadAttempts(ctx context.Context, id int64) error {
	return r.write.ResetDownloadAttempts(ctx, id)
}

// ListWantedForMatch returns all currently-Wanted issues for RSS feed matching.
func (r *IssueRepository) ListWantedForMatch(ctx context.Context) ([]Issue, error) {
	return r.read.ListWantedForMatch(ctx)
}
