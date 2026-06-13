package repository

import (
	"context"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// MetadataCache is the domain view of a metadata_cache row.
type MetadataCache = sqlc.MetadataCache

// MetadataCacheEntry carries a cache write.
type MetadataCacheEntry struct {
	CacheKey        string
	Payload         string
	SourceUpdatedAt string
	FetchedAt       string
}

// MetadataCacheRepository persists cached provider responses.
type MetadataCacheRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewMetadataCacheRepository binds a MetadataCacheRepository to the read and write pools.
func NewMetadataCacheRepository(d *db.DB) *MetadataCacheRepository {
	return &MetadataCacheRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

func (r *MetadataCacheRepository) Get(ctx context.Context, cacheKey string) (MetadataCache, error) {
	return r.read.GetMetadataCache(ctx, cacheKey)
}

func (r *MetadataCacheRepository) Put(ctx context.Context, in MetadataCacheEntry) error {
	return r.write.PutMetadataCache(ctx, sqlc.PutMetadataCacheParams{
		CacheKey:        in.CacheKey,
		Payload:         in.Payload,
		SourceUpdatedAt: nullString(in.SourceUpdatedAt),
		FetchedAt:       in.FetchedAt,
	})
}
