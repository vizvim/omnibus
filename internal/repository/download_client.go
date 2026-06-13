package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// ErrNoDownloadClientConfig is returned by DownloadClientRepository.Get when no config
// row has been stored yet. Callers treat this as "absent/empty" (configured == false)
// rather than a hard error.
var ErrNoDownloadClientConfig = errors.New("download client config not set")

// DownloadClientRow is the domain view of the singleton download_client_config row. The
// APIKey is included here (the service masks it before it reaches transport); the
// repository layer is trusted.
type DownloadClientRow struct {
	URL       string
	APIKey    string
	Category  string
	CreatedAt string
	UpdatedAt string
}

// DownloadClientConfigUpsert carries the mutable fields for the singleton config upsert.
type DownloadClientConfigUpsert struct {
	URL      string
	APIKey   string
	Category string
}

// DownloadClientRepository persists the singleton SABnzbd download-client config. Writes
// go through the single-writer pool, reads through the read pool (ADR 0002).
type DownloadClientRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewDownloadClientRepository binds a DownloadClientRepository to the read and write pools.
func NewDownloadClientRepository(d *db.DB) *DownloadClientRepository {
	return &DownloadClientRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

// Get returns the stored config. When no row exists yet it returns a zero-value row
// and ErrNoDownloadClientConfig so callers can detect the absent/empty case.
func (r *DownloadClientRepository) Get(ctx context.Context) (DownloadClientRow, error) {
	row, err := r.read.GetDownloadClientConfig(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DownloadClientRow{}, ErrNoDownloadClientConfig
		}
		return DownloadClientRow{}, err
	}
	return mapDownloadClientConfig(row), nil
}

func (r *DownloadClientRepository) Upsert(ctx context.Context, in DownloadClientConfigUpsert, nowISO string) (DownloadClientRow, error) {
	row, err := r.write.UpsertDownloadClientConfig(ctx, sqlc.UpsertDownloadClientConfigParams{
		Url:       in.URL,
		ApiKey:    nullString(in.APIKey),
		Category:  in.Category,
		CreatedAt: nowISO,
		UpdatedAt: nowISO,
	})
	if err != nil {
		return DownloadClientRow{}, err
	}
	return mapDownloadClientConfig(row), nil
}

func mapDownloadClientConfig(in sqlc.DownloadClientConfig) DownloadClientRow {
	return DownloadClientRow{
		URL:       in.Url,
		APIKey:    in.ApiKey.String,
		Category:  in.Category,
		CreatedAt: in.CreatedAt,
		UpdatedAt: in.UpdatedAt,
	}
}
