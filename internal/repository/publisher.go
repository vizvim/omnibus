package repository

import (
	"context"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// Publisher is the domain view of a publisher row.
type Publisher = sqlc.Publisher

// PublisherUpsert carries the fields to insert or update a publisher (idempotent on name).
type PublisherUpsert struct {
	ComicvinePublisherID *int64
	Name                 string
	CreatedAt            string
}

// PublisherRepository persists publishers.
type PublisherRepository interface {
	Upsert(ctx context.Context, in PublisherUpsert) (Publisher, error)
	GetByID(ctx context.Context, id int64) (Publisher, error)
}

type publisherRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewPublisherRepository binds a PublisherRepository to the read and write pools.
func NewPublisherRepository(d *db.DB) PublisherRepository {
	return &publisherRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

func (r *publisherRepository) Upsert(ctx context.Context, in PublisherUpsert) (Publisher, error) {
	return r.write.UpsertPublisher(ctx, sqlc.UpsertPublisherParams{
		ComicvinePublisherID: nullInt64Ptr(in.ComicvinePublisherID),
		Name:                 in.Name,
		CreatedAt:            in.CreatedAt,
	})
}

func (r *publisherRepository) GetByID(ctx context.Context, id int64) (Publisher, error) {
	return r.read.GetPublisherByID(ctx, id)
}
