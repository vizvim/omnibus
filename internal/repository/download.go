package repository

import (
	"context"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// DownloadRow is the domain view of a downloads row.
type DownloadRow struct {
	ID           int64
	IssueID      int64
	Provider     string
	ReleaseKey   string
	ReleaseTitle string
	SizeBytes    int64
	Status       string
	ClientRef    string
	CreatedAt    string
	UpdatedAt    string
}

// DownloadUpsert carries the fields for the idempotent download upsert.
type DownloadUpsert struct {
	IssueID      int64
	Provider     string
	ReleaseKey   string
	ReleaseTitle string
	SizeBytes    int64
	Status       string
	ClientRef    string
}

// DownloadRepository persists downloads, idempotent on (provider, release_key, issue_id).
type DownloadRepository interface {
	Upsert(ctx context.Context, in DownloadUpsert, nowISO string) (DownloadRow, error)
	Get(ctx context.Context, id int64) (DownloadRow, error)
	ListByIssue(ctx context.Context, issueID int64) ([]DownloadRow, error)
}

type downloadRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewDownloadRepository binds a DownloadRepository to the read and write pools.
func NewDownloadRepository(d *db.DB) DownloadRepository {
	return &downloadRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

func (r *downloadRepository) Upsert(ctx context.Context, in DownloadUpsert, nowISO string) (DownloadRow, error) {
	row, err := r.write.CreateDownload(ctx, sqlc.CreateDownloadParams{
		IssueID:      in.IssueID,
		Provider:     in.Provider,
		ReleaseKey:   in.ReleaseKey,
		ReleaseTitle: nullString(in.ReleaseTitle),
		SizeBytes:    nullInt64(in.SizeBytes),
		Status:       in.Status,
		ClientRef:    nullString(in.ClientRef),
		CreatedAt:    nowISO,
		UpdatedAt:    nowISO,
	})
	if err != nil {
		return DownloadRow{}, err
	}
	return mapDownload(row), nil
}

func (r *downloadRepository) Get(ctx context.Context, id int64) (DownloadRow, error) {
	row, err := r.read.GetDownloadByID(ctx, id)
	if err != nil {
		return DownloadRow{}, err
	}
	return mapDownload(row), nil
}

func (r *downloadRepository) ListByIssue(ctx context.Context, issueID int64) ([]DownloadRow, error) {
	rows, err := r.read.ListDownloadsByIssue(ctx, issueID)
	if err != nil {
		return nil, err
	}
	out := make([]DownloadRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapDownload(row))
	}
	return out, nil
}

func mapDownload(in sqlc.Download) DownloadRow {
	return DownloadRow{
		ID:           in.ID,
		IssueID:      in.IssueID,
		Provider:     in.Provider,
		ReleaseKey:   in.ReleaseKey,
		ReleaseTitle: in.ReleaseTitle.String,
		SizeBytes:    in.SizeBytes.Int64,
		Status:       in.Status,
		ClientRef:    in.ClientRef.String,
		CreatedAt:    in.CreatedAt,
		UpdatedAt:    in.UpdatedAt,
	}
}
