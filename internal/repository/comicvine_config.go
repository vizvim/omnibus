package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// ErrNoComicVineConfig is returned by ComicVineConfigRepository.Get when no config row has
// been stored yet. Callers treat this as "absent/empty" (configured == false) rather than a
// hard error.
var ErrNoComicVineConfig = errors.New("comicvine config not set")

// ComicVineConfigRow is the domain view of the singleton comicvine_config row. The APIKey is
// included here (the service masks it before it reaches transport); the repository layer is
// trusted.
type ComicVineConfigRow struct {
	APIKey    string
	CreatedAt string
	UpdatedAt string
}

// ComicVineConfigUpsert carries the mutable fields for the singleton config upsert.
type ComicVineConfigUpsert struct {
	APIKey string
}

// ComicVineConfigRepository persists the singleton ComicVine metadata-provider config.
// Writes go through the single-writer pool, reads through the read pool (ADR 0002).
type ComicVineConfigRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewComicVineConfigRepository binds a ComicVineConfigRepository to the read and write pools.
func NewComicVineConfigRepository(d *db.DB) *ComicVineConfigRepository {
	return &ComicVineConfigRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

// Get returns the stored config. When no row exists yet it returns a zero-value row and
// ErrNoComicVineConfig so callers can detect the absent/empty case.
func (r *ComicVineConfigRepository) Get(ctx context.Context) (ComicVineConfigRow, error) {
	row, err := r.read.GetComicVineConfig(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ComicVineConfigRow{}, ErrNoComicVineConfig
		}
		return ComicVineConfigRow{}, err
	}
	return mapComicVineConfig(row), nil
}

func (r *ComicVineConfigRepository) Upsert(ctx context.Context, in ComicVineConfigUpsert, nowISO string) (ComicVineConfigRow, error) {
	row, err := r.write.UpsertComicVineConfig(ctx, sqlc.UpsertComicVineConfigParams{
		ApiKey:    nullString(in.APIKey),
		CreatedAt: nowISO,
		UpdatedAt: nowISO,
	})
	if err != nil {
		return ComicVineConfigRow{}, err
	}
	return mapComicVineConfig(row), nil
}

func mapComicVineConfig(in sqlc.ComicvineConfig) ComicVineConfigRow {
	return ComicVineConfigRow{
		APIKey:    in.ApiKey.String,
		CreatedAt: in.CreatedAt,
		UpdatedAt: in.UpdatedAt,
	}
}
