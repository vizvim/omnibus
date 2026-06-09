package repository

import (
	"context"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// Issue is the domain view of an issue row.
type Issue = sqlc.Issue

// IssueUpsert carries the fields needed to insert or update an issue, keyed on the
// immutable ComicVine issue id (D-04). The three issue-number fields preserve the
// distinctness of e.g. 7 vs 7.INH (SER-05).
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
	Status           string
	CreatedAt        string
}

// IssueRepository persists issues, pinned to comicvine_issue_id.
type IssueRepository interface {
	Upsert(ctx context.Context, in IssueUpsert) (Issue, error)
	ListBySeries(ctx context.Context, seriesID int64) ([]Issue, error)
	CountBySeries(ctx context.Context, seriesID int64) (int64, error)
	UpdateStatus(ctx context.Context, id int64, status string, searchAttempts int32) error
}

type issueRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewIssueRepository binds an IssueRepository to the read and write pools.
func NewIssueRepository(d *db.DB) IssueRepository {
	return &issueRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

func (r *issueRepository) Upsert(ctx context.Context, in IssueUpsert) (Issue, error) {
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
		Status:           in.Status,
		CreatedAt:        in.CreatedAt,
	})
}

func (r *issueRepository) ListBySeries(ctx context.Context, seriesID int64) ([]Issue, error) {
	return r.read.ListIssuesBySeries(ctx, seriesID)
}

func (r *issueRepository) CountBySeries(ctx context.Context, seriesID int64) (int64, error) {
	return r.read.CountIssuesBySeries(ctx, seriesID)
}

func (r *issueRepository) UpdateStatus(ctx context.Context, id int64, status string, searchAttempts int32) error {
	return r.write.UpdateIssueStatus(ctx, sqlc.UpdateIssueStatusParams{
		Status:         status,
		SearchAttempts: int64(searchAttempts),
		ID:             id,
	})
}
