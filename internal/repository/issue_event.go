package repository

import (
	"context"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// IssueEventRow is the domain view of an issue_events row (the per-issue timeline).
type IssueEventRow struct {
	ID          int64
	IssueID     int64
	EventType   string
	PayloadJSON string
	OccurredAt  string
}

// IssueEventInsert carries the fields for appending a timeline event.
type IssueEventInsert struct {
	IssueID     int64
	EventType   string
	PayloadJSON string
	OccurredAt  string
}

// IssueEventRepository appends and lists per-issue lifecycle events (OBS-01).
type IssueEventRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewIssueEventRepository binds an IssueEventRepository to the read and write pools.
func NewIssueEventRepository(d *db.DB) *IssueEventRepository {
	return &IssueEventRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

func (r *IssueEventRepository) Insert(ctx context.Context, in IssueEventInsert) (IssueEventRow, error) {
	row, err := r.write.InsertIssueEvent(ctx, sqlc.InsertIssueEventParams{
		IssueID:     in.IssueID,
		EventType:   in.EventType,
		PayloadJson: nullString(in.PayloadJSON),
		OccurredAt:  in.OccurredAt,
	})
	if err != nil {
		return IssueEventRow{}, err
	}
	return mapIssueEvent(row), nil
}

func (r *IssueEventRepository) ListByIssue(ctx context.Context, issueID int64) ([]IssueEventRow, error) {
	rows, err := r.read.ListIssueEventsByIssue(ctx, issueID)
	if err != nil {
		return nil, err
	}
	out := make([]IssueEventRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapIssueEvent(row))
	}
	return out, nil
}

func mapIssueEvent(in sqlc.IssueEvent) IssueEventRow {
	return IssueEventRow{
		ID:          in.ID,
		IssueID:     in.IssueID,
		EventType:   in.EventType,
		PayloadJSON: in.PayloadJson.String,
		OccurredAt:  in.OccurredAt,
	}
}
