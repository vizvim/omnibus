package repository

import (
	"context"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// StoryArc is the domain view of a story_arcs row.
type StoryArc = sqlc.StoryArc

// ArcUpsert carries the fields to insert or update a story arc (idempotent on CV arc id).
type ArcUpsert struct {
	ComicvineArcID int64
	Name           string
	CreatedAt      string
}

// ArcRepository persists story arcs and their issue membership.
type ArcRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewArcRepository binds an ArcRepository to the read and write pools.
func NewArcRepository(d *db.DB) *ArcRepository {
	return &ArcRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

func (r *ArcRepository) Upsert(ctx context.Context, in ArcUpsert) (StoryArc, error) {
	return r.write.UpsertArc(ctx, sqlc.UpsertArcParams{
		ComicvineArcID: nullInt64(in.ComicvineArcID),
		Name:           in.Name,
		CreatedAt:      in.CreatedAt,
	})
}

func (r *ArcRepository) LinkIssue(ctx context.Context, arcID, issueID int64, position int32) error {
	return r.write.LinkArcIssue(ctx, sqlc.LinkArcIssueParams{
		ArcID:    arcID,
		IssueID:  issueID,
		Position: int64(position),
	})
}

func (r *ArcRepository) ListBySeries(ctx context.Context, seriesID int64) ([]StoryArc, error) {
	return r.read.ListArcsBySeries(ctx, seriesID)
}
