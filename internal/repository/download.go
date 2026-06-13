package repository

import (
	"context"
	"database/sql"
	"errors"

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

// DownloadHistoryInsert carries the fields for an append-only download_history row (DL-03).
type DownloadHistoryInsert struct {
	IssueID    int64
	Provider   string
	ReleaseKey string
	Result     string
	Detail     string
	OccurredAt string
}

// DownloadHistoryRow is the domain view of a download_history row.
type DownloadHistoryRow struct {
	ID         int64
	IssueID    int64
	Provider   string
	ReleaseKey string
	Result     string
	Detail     string
	OccurredAt string
}

// DeadReleaseKey is a (provider, release_key) pair already known-bad for an issue (a
// Failed/Blacklisted downloads row), excluded from replacement searches (D-12).
type DeadReleaseKey struct {
	Provider   string
	ReleaseKey string
}

// DownloadRepository persists downloads, idempotent on (provider, release_key, issue_id).
type DownloadRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewDownloadRepository binds a DownloadRepository to the read and write pools.
func NewDownloadRepository(d *db.DB) *DownloadRepository {
	return &DownloadRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

func (r *DownloadRepository) Upsert(ctx context.Context, in DownloadUpsert, nowISO string) (DownloadRow, error) {
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

func (r *DownloadRepository) Get(ctx context.Context, id int64) (DownloadRow, error) {
	row, err := r.read.GetDownloadByID(ctx, id)
	if err != nil {
		return DownloadRow{}, err
	}
	return mapDownload(row), nil
}

func (r *DownloadRepository) ListByIssue(ctx context.Context, issueID int64) ([]DownloadRow, error) {
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

// ListActive returns the in-flight downloads (Queued/Downloading) the poll job
// reconciles each tick, oldest-updated-first (D-01).
func (r *DownloadRepository) ListActive(ctx context.Context) ([]DownloadRow, error) {
	rows, err := r.read.ListActiveDownloads(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]DownloadRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapDownload(row))
	}
	return out, nil
}

// UpdateStatus flips a download row's status and bumps updated_at, returning the row.
// Status writes route through the download-status state machine before this call.
func (r *DownloadRepository) UpdateStatus(ctx context.Context, id int64, status, nowISO string) (DownloadRow, error) {
	row, err := r.write.UpdateDownloadStatus(ctx, sqlc.UpdateDownloadStatusParams{
		Status:    status,
		UpdatedAt: nowISO,
		ID:        id,
	})
	if err != nil {
		return DownloadRow{}, err
	}
	return mapDownload(row), nil
}

// InsertHistory appends a download_history row (append-only audit trail, DL-03).
func (r *DownloadRepository) InsertHistory(ctx context.Context, in DownloadHistoryInsert) (DownloadHistoryRow, error) {
	row, err := r.write.InsertDownloadHistory(ctx, sqlc.InsertDownloadHistoryParams{
		IssueID:    in.IssueID,
		Provider:   in.Provider,
		ReleaseKey: in.ReleaseKey,
		Result:     in.Result,
		Detail:     nullString(in.Detail),
		OccurredAt: in.OccurredAt,
	})
	if err != nil {
		return DownloadHistoryRow{}, err
	}
	return DownloadHistoryRow{
		ID:         row.ID,
		IssueID:    row.IssueID,
		Provider:   row.Provider,
		ReleaseKey: row.ReleaseKey,
		Result:     row.Result,
		Detail:     row.Detail.String,
		OccurredAt: row.OccurredAt,
	}, nil
}

// ListDeadReleaseKeys returns the (provider, release_key) pairs already Failed or
// Blacklisted for an issue, so a replacement search cannot re-pick them (D-12).
func (r *DownloadRepository) ListDeadReleaseKeys(ctx context.Context, issueID int64) ([]DeadReleaseKey, error) {
	rows, err := r.read.ListDeadReleaseKeysForIssue(ctx, issueID)
	if err != nil {
		return nil, err
	}
	out := make([]DeadReleaseKey, 0, len(rows))
	for _, row := range rows {
		out = append(out, DeadReleaseKey{Provider: row.Provider, ReleaseKey: row.ReleaseKey})
	}
	return out, nil
}

// GetCompletedStorage returns the storage path the poll loop captured when this
// release completed for the issue (DL-03 history.detail with result='completed'). The
// post-process orchestrator reads it to locate the file on disk. The bool is false when
// no completed history row exists (the download never reached Completed).
func (r *DownloadRepository) GetCompletedStorage(ctx context.Context, issueID int64, provider, releaseKey string) (string, bool, error) {
	detail, err := r.read.GetLatestCompletedStorageForIssue(ctx, sqlc.GetLatestCompletedStorageForIssueParams{
		IssueID:    issueID,
		Provider:   provider,
		ReleaseKey: releaseKey,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	// A completed history row with a NULL/empty detail means the client reported no storage
	// path: treat as "present row, but no usable path" so the orchestrator fails loudly.
	return detail.String, true, nil
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
