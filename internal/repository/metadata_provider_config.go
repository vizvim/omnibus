package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// ErrNoMetadataProviderConfig is returned by MetadataProviderConfigRepository.Get when no
// config row has been stored for the requested provider yet. Callers treat this as
// "absent/empty" (configured == false) rather than a hard error.
var ErrNoMetadataProviderConfig = errors.New("metadata provider config not set")

// MetadataProviderConfigRow is the domain view of a metadata_provider_config row. The APIKey
// is included here (the service masks it before it reaches transport); the repository layer
// is trusted.
type MetadataProviderConfigRow struct {
	Provider  string
	APIKey    string
	CreatedAt string
	UpdatedAt string
}

// MetadataProviderConfigUpsert carries the mutable fields for a per-provider config upsert.
type MetadataProviderConfigUpsert struct {
	Provider string
	APIKey   string
}

// MetadataProviderConfigRepository persists per-provider metadata-provider config rows
// (ComicVine today; others later). Writes go through the single-writer pool, reads through
// the read pool (ADR 0002).
type MetadataProviderConfigRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewMetadataProviderConfigRepository binds the repository to the read and write pools.
func NewMetadataProviderConfigRepository(d *db.DB) *MetadataProviderConfigRepository {
	return &MetadataProviderConfigRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

// Get returns the stored config for the given provider. When no row exists it returns a
// zero-value row and ErrNoMetadataProviderConfig so callers can detect the absent/empty case.
func (r *MetadataProviderConfigRepository) Get(ctx context.Context, provider string) (MetadataProviderConfigRow, error) {
	row, err := r.read.GetMetadataProviderConfig(ctx, provider)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return MetadataProviderConfigRow{}, ErrNoMetadataProviderConfig
		}
		return MetadataProviderConfigRow{}, err
	}
	return mapMetadataProviderConfig(row), nil
}

// Upsert inserts or updates the config row for in.Provider, preserving created_at on update.
func (r *MetadataProviderConfigRepository) Upsert(ctx context.Context, in MetadataProviderConfigUpsert, nowISO string) (MetadataProviderConfigRow, error) {
	row, err := r.write.UpsertMetadataProviderConfig(ctx, sqlc.UpsertMetadataProviderConfigParams{
		Provider:  in.Provider,
		ApiKey:    nullString(in.APIKey),
		CreatedAt: nowISO,
		UpdatedAt: nowISO,
	})
	if err != nil {
		return MetadataProviderConfigRow{}, err
	}
	return mapMetadataProviderConfig(row), nil
}

func mapMetadataProviderConfig(in sqlc.MetadataProviderConfig) MetadataProviderConfigRow {
	return MetadataProviderConfigRow{
		Provider:  in.Provider,
		APIKey:    in.ApiKey.String,
		CreatedAt: in.CreatedAt,
		UpdatedAt: in.UpdatedAt,
	}
}
