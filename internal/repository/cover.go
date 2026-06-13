package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// CoverRepository persists cover art blobs keyed on (entity_type, entity_id). Covers
// live in their own table so the series/issues hot rows stay free of BLOBs.
type CoverRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewCoverRepository binds a CoverRepository to the read and write pools.
func NewCoverRepository(d *db.DB) *CoverRepository {
	return &CoverRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

func (r *CoverRepository) Upsert(ctx context.Context, entityType string, id int64, image []byte, contentType string) error {
	return r.write.UpsertCover(ctx, sqlc.UpsertCoverParams{
		EntityType:  entityType,
		EntityID:    id,
		Image:       image,
		ContentType: contentType,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	})
}

func (r *CoverRepository) Get(ctx context.Context, entityType string, id int64) ([]byte, string, bool, error) {
	row, err := r.read.GetCover(ctx, sqlc.GetCoverParams{EntityType: entityType, EntityID: id})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", false, nil
		}
		return nil, "", false, err
	}
	return row.Image, row.ContentType, true, nil
}

func (r *CoverRepository) Exists(ctx context.Context, entityType string, id int64) (bool, error) {
	return r.read.CoverExists(ctx, sqlc.CoverExistsParams{EntityType: entityType, EntityID: id})
}
