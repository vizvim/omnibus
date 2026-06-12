package repository

import (
	"context"
	"math"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// IndexerRow is the domain view of an indexer row. Booleans and optional columns are
// exposed as Go-native types so the service layer never touches sql.Null* or the
// integer-encoded SQLite booleans. ApiKey is included here (the service masks it before
// it reaches transport); the repository layer is trusted.
type IndexerRow struct {
	ID         int64
	Name       string
	Kind       string
	BaseURL    string
	APIKey     string
	Enabled    bool
	Categories string
	Priority   int32
	UseForRSS  bool
	CreatedAt  string
	UpdatedAt  string
}

// IndexerUpsert carries the mutable fields for create/update.
type IndexerUpsert struct {
	Name       string
	Kind       string
	BaseURL    string
	APIKey     string
	Enabled    bool
	Categories string
	Priority   int32
	UseForRSS  bool
}

// IndexerRepository persists DB-backed indexer records (D-16). Writes go through the
// single-writer pool, reads through the read pool (ADR 0002).
type IndexerRepository interface {
	Create(ctx context.Context, in IndexerUpsert, nowISO string) (IndexerRow, error)
	Get(ctx context.Context, id int64) (IndexerRow, error)
	List(ctx context.Context) ([]IndexerRow, error)
	ListEnabled(ctx context.Context) ([]IndexerRow, error)
	Update(ctx context.Context, id int64, in IndexerUpsert, nowISO string) (IndexerRow, error)
	Delete(ctx context.Context, id int64) error
}

type indexerRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewIndexerRepository binds an IndexerRepository to the read and write pools.
func NewIndexerRepository(d *db.DB) IndexerRepository {
	return &indexerRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

func (r *indexerRepository) Create(ctx context.Context, in IndexerUpsert, nowISO string) (IndexerRow, error) {
	row, err := r.write.CreateIndexer(ctx, sqlc.CreateIndexerParams{
		Name:       in.Name,
		Kind:       in.Kind,
		BaseUrl:    in.BaseURL,
		ApiKey:     nullString(in.APIKey),
		Enabled:    boolToInt(in.Enabled),
		Categories: nullString(in.Categories),
		Priority:   int64(in.Priority),
		UseForRss:  boolToInt(in.UseForRSS),
		CreatedAt:  nowISO,
		UpdatedAt:  nowISO,
	})
	if err != nil {
		return IndexerRow{}, err
	}
	return mapIndexer(row), nil
}

func (r *indexerRepository) Get(ctx context.Context, id int64) (IndexerRow, error) {
	row, err := r.read.GetIndexer(ctx, id)
	if err != nil {
		return IndexerRow{}, err
	}
	return mapIndexer(row), nil
}

func (r *indexerRepository) List(ctx context.Context) ([]IndexerRow, error) {
	rows, err := r.read.ListIndexers(ctx)
	if err != nil {
		return nil, err
	}
	return mapIndexers(rows), nil
}

func (r *indexerRepository) ListEnabled(ctx context.Context) ([]IndexerRow, error) {
	rows, err := r.read.ListEnabledIndexers(ctx)
	if err != nil {
		return nil, err
	}
	return mapIndexers(rows), nil
}

func (r *indexerRepository) Update(ctx context.Context, id int64, in IndexerUpsert, nowISO string) (IndexerRow, error) {
	row, err := r.write.UpdateIndexer(ctx, sqlc.UpdateIndexerParams{
		Name:       in.Name,
		Kind:       in.Kind,
		BaseUrl:    in.BaseURL,
		ApiKey:     nullString(in.APIKey),
		Enabled:    boolToInt(in.Enabled),
		Categories: nullString(in.Categories),
		Priority:   int64(in.Priority),
		UseForRss:  boolToInt(in.UseForRSS),
		UpdatedAt:  nowISO,
		ID:         id,
	})
	if err != nil {
		return IndexerRow{}, err
	}
	return mapIndexer(row), nil
}

func (r *indexerRepository) Delete(ctx context.Context, id int64) error {
	return r.write.DeleteIndexer(ctx, id)
}

func mapIndexer(in sqlc.Indexer) IndexerRow {
	return IndexerRow{
		ID:         in.ID,
		Name:       in.Name,
		Kind:       in.Kind,
		BaseURL:    in.BaseUrl,
		APIKey:     in.ApiKey.String,
		Enabled:    in.Enabled != 0,
		Categories: in.Categories.String,
		Priority:   indexerInt32(in.Priority),
		UseForRSS:  in.UseForRss != 0,
		CreatedAt:  in.CreatedAt,
		UpdatedAt:  in.UpdatedAt,
	}
}

func mapIndexers(in []sqlc.Indexer) []IndexerRow {
	out := make([]IndexerRow, 0, len(in))
	for _, row := range in {
		out = append(out, mapIndexer(row))
	}
	return out
}

// boolToInt encodes a Go bool as the 0/1 integer SQLite stores for boolean columns.
func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// indexerInt32 clamps an int64 priority into int32 range (priorities are small; the
// clamp keeps gosec G115 satisfied and is defensive).
func indexerInt32(v int64) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}
