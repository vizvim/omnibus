// Package repository defines the domain persistence interfaces and their
// sqlc-backed implementations. Each repository wraps the generated sqlc Queries,
// routing writes through the single-writer pool and reads through the read pool.
// The service layer depends only on these interfaces, never on sqlc or *sql.DB
// directly.
package repository

import (
	"context"
	"database/sql"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// Series is the domain view of a series row.
type Series = sqlc.Series

// SeriesUpsert carries the fields needed to insert or update a series, keyed on the
// immutable ComicVine volume id.
type SeriesUpsert struct {
	ComicvineVolumeID int64
	PublisherID       *int64
	Name              string
	StartYear         *int32
	Description       string
	Status            string
	TotalIssues       int32
	HaveIssues        int32
	SettingsJSON      string
	LastRefreshedAt   string
	CreatedAt         string
}

// SeriesRepository persists series, pinned to comicvine_volume_id.
type SeriesRepository interface {
	Upsert(ctx context.Context, in SeriesUpsert) (Series, error)
	GetByVolumeID(ctx context.Context, volumeID int64) (Series, error)
	GetByID(ctx context.Context, id int64) (Series, error)
	List(ctx context.Context, limit, offset int32) ([]Series, error)
	UpdateSettings(ctx context.Context, id int64, status, settingsJSON string) (Series, error)
	UpdateCounts(ctx context.Context, id int64, total, have int32) error
	UpdateLastRefreshed(ctx context.Context, id int64, refreshedAt string) error
}

type seriesRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewSeriesRepository binds a SeriesRepository to the read and write pools.
func NewSeriesRepository(d *db.DB) SeriesRepository {
	return &seriesRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

func (r *seriesRepository) Upsert(ctx context.Context, in SeriesUpsert) (Series, error) {
	return r.write.UpsertSeries(ctx, sqlc.UpsertSeriesParams{
		ComicvineVolumeID: in.ComicvineVolumeID,
		PublisherID:       nullInt64Ptr(in.PublisherID),
		Name:              in.Name,
		StartYear:         nullInt32Ptr(in.StartYear),
		Description:       nullString(in.Description),
		Status:            in.Status,
		TotalIssues:       int64(in.TotalIssues),
		HaveIssues:        int64(in.HaveIssues),
		SettingsJson:      nullString(in.SettingsJSON),
		LastRefreshedAt:   nullString(in.LastRefreshedAt),
		CreatedAt:         in.CreatedAt,
	})
}

func (r *seriesRepository) GetByVolumeID(ctx context.Context, volumeID int64) (Series, error) {
	return r.read.GetSeriesByVolumeID(ctx, volumeID)
}

func (r *seriesRepository) GetByID(ctx context.Context, id int64) (Series, error) {
	return r.read.GetSeriesByID(ctx, id)
}

func (r *seriesRepository) List(ctx context.Context, limit, offset int32) ([]Series, error) {
	return r.read.ListSeries(ctx, sqlc.ListSeriesParams{Limit: int64(limit), Offset: int64(offset)})
}

func (r *seriesRepository) UpdateSettings(ctx context.Context, id int64, status, settingsJSON string) (Series, error) {
	return r.write.UpdateSeriesSettings(ctx, sqlc.UpdateSeriesSettingsParams{
		Status:       status,
		SettingsJson: nullString(settingsJSON),
		ID:           id,
	})
}

func (r *seriesRepository) UpdateCounts(ctx context.Context, id int64, total, have int32) error {
	return r.write.UpdateSeriesCounts(ctx, sqlc.UpdateSeriesCountsParams{
		TotalIssues: int64(total),
		HaveIssues:  int64(have),
		ID:          id,
	})
}

func (r *seriesRepository) UpdateLastRefreshed(ctx context.Context, id int64, refreshedAt string) error {
	return r.write.UpdateSeriesLastRefreshed(ctx, sqlc.UpdateSeriesLastRefreshedParams{
		LastRefreshedAt: nullString(refreshedAt),
		ID:              id,
	})
}

// nullString maps "" to a NULL column and any other value to a present string.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullInt64Ptr(p *int64) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *p, Valid: true}
}

func nullInt32Ptr(p *int32) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*p), Valid: true}
}

// nullInt64 maps 0 to NULL and any other value to a present int (CV ids are positive).
func nullInt64(v int64) sql.NullInt64 {
	if v == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}
