package repository

import (
	"context"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// MetadataCache is the domain view of a metadata_cache row.
type MetadataCache = sqlc.MetadataCache

// MetadataCacheEntry carries a cache write (META-04/05).
type MetadataCacheEntry struct {
	CacheKey        string
	Payload         string
	SourceUpdatedAt string
	FetchedAt       string
}

// MetadataCacheRepository persists cached provider responses (META-04).
type MetadataCacheRepository interface {
	Get(ctx context.Context, cacheKey string) (MetadataCache, error)
	Put(ctx context.Context, in MetadataCacheEntry) error
}

type metadataCacheRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewMetadataCacheRepository binds a MetadataCacheRepository to the read and write pools.
func NewMetadataCacheRepository(d *db.DB) MetadataCacheRepository {
	return &metadataCacheRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

func (r *metadataCacheRepository) Get(ctx context.Context, cacheKey string) (MetadataCache, error) {
	return r.read.GetMetadataCache(ctx, cacheKey)
}

func (r *metadataCacheRepository) Put(ctx context.Context, in MetadataCacheEntry) error {
	return r.write.PutMetadataCache(ctx, sqlc.PutMetadataCacheParams{
		CacheKey:        in.CacheKey,
		Payload:         in.Payload,
		SourceUpdatedAt: nullString(in.SourceUpdatedAt),
		FetchedAt:       in.FetchedAt,
	})
}
