package repository

import (
	"context"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// BlacklistKey is a (provider, release_key) pair a user has blacklisted for an issue
// (D-11). The replacement search unions these with the dead-download keys to build the
// BlacklistSet it filters candidates against.
type BlacklistKey struct {
	Provider   string
	ReleaseKey string
}

// BlacklistAdd carries the fields to blacklist a specific release for an issue (D-11,
// per-issue scope: series_id is always NULL this phase).
type BlacklistAdd struct {
	IssueID    int64
	Provider   string
	ReleaseKey string
	Reason     string
}

// BlacklistRepository persists per-issue release blacklists over the blacklists table.
// Writes go through the single-writer pool, reads through the read pool (ADR 0002).
type BlacklistRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewBlacklistRepository binds a BlacklistRepository to the read and write pools.
func NewBlacklistRepository(d *db.DB) *BlacklistRepository {
	return &BlacklistRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

// Add blacklists a specific release for an issue (D-11). A duplicate (same provider,
// release_key, issue) is a no-op via the UNIQUE constraint.
func (r *BlacklistRepository) Add(ctx context.Context, in BlacklistAdd, nowISO string) error {
	return r.write.AddBlacklist(ctx, sqlc.AddBlacklistParams{
		IssueID:    nullInt64(in.IssueID),
		Provider:   in.Provider,
		ReleaseKey: in.ReleaseKey,
		Reason:     nullString(in.Reason),
		CreatedAt:  nowISO,
	})
}

// ListForIssue returns the (provider, release_key) pairs blacklisted for an issue.
func (r *BlacklistRepository) ListForIssue(ctx context.Context, issueID int64) ([]BlacklistKey, error) {
	rows, err := r.read.ListBlacklistForIssue(ctx, nullInt64(issueID))
	if err != nil {
		return nil, err
	}
	out := make([]BlacklistKey, 0, len(rows))
	for _, row := range rows {
		out = append(out, BlacklistKey{Provider: row.Provider, ReleaseKey: row.ReleaseKey})
	}
	return out, nil
}
