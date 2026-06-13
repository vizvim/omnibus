package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// IssueCredit is the domain view of an issue_credits row: a normalized creator
// credit (role + name) for an issue, optionally carrying the ComicVine person id.
type IssueCredit struct {
	IssueID    int64
	Role       string
	Name       string
	CVPersonID int64
}

// IssueCreditRepository persists the normalized per-issue creator credits derived
// from ComicVine person_credits.
type IssueCreditRepository struct {
	db    *db.DB
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewIssueCreditRepository binds an IssueCreditRepository to the read and write pools.
func NewIssueCreditRepository(d *db.DB) *IssueCreditRepository {
	return &IssueCreditRepository{db: d, read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

// Replace atomically swaps an issue's credit set: it deletes every existing row
// then inserts the supplied credits inside one write transaction, so a re-import
// is idempotent and never leaves a half-updated credit list.
func (r *IssueCreditRepository) Replace(ctx context.Context, issueID int64, credits []IssueCredit) (err error) {
	tx, err := r.db.Write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin issue-credit tx: %w", err)
	}
	// Roll back on any error path; on the success path the explicit Commit below has
	// already run so the rollback is a no-op (its ErrTxDone is intentionally ignored).
	defer func() {
		if err != nil {
			if rerr := tx.Rollback(); rerr != nil && !errors.Is(rerr, sql.ErrTxDone) {
				err = errors.Join(err, fmt.Errorf("rollback issue-credit tx: %w", rerr))
			}
		}
	}()

	q := r.write.WithTx(tx)
	if derr := q.DeleteIssueCredits(ctx, issueID); derr != nil {
		return fmt.Errorf("delete issue credits: %w", derr)
	}
	for _, c := range credits {
		if ierr := q.InsertIssueCredit(ctx, sqlc.InsertIssueCreditParams{
			IssueID:    issueID,
			Role:       c.Role,
			Name:       c.Name,
			CvPersonID: c.CVPersonID,
		}); ierr != nil {
			return fmt.Errorf("insert issue credit: %w", ierr)
		}
	}

	if cerr := tx.Commit(); cerr != nil {
		return fmt.Errorf("commit issue-credit tx: %w", cerr)
	}
	return nil
}

// ListByIssue returns an issue's credits ordered by (role, name).
func (r *IssueCreditRepository) ListByIssue(ctx context.Context, issueID int64) ([]IssueCredit, error) {
	rows, err := r.read.ListIssueCredits(ctx, issueID)
	if err != nil {
		return nil, err
	}
	out := make([]IssueCredit, 0, len(rows))
	for _, row := range rows {
		out = append(out, IssueCredit{
			IssueID:    row.IssueID,
			Role:       row.Role,
			Name:       row.Name,
			CVPersonID: row.CvPersonID,
		})
	}
	return out, nil
}
