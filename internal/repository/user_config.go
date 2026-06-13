package repository

import (
	"context"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// UserConfig is the domain view of a user_config row.
type UserConfig = sqlc.UserConfig

// UserConfigRepository persists typed user configuration keys.
type UserConfigRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewUserConfigRepository binds a UserConfigRepository to the read and write pools.
func NewUserConfigRepository(d *db.DB) *UserConfigRepository {
	return &UserConfigRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

func (r *UserConfigRepository) Get(ctx context.Context, key string) (string, error) {
	row, err := r.read.GetUserConfig(ctx, key)
	if err != nil {
		return "", err
	}
	return row.Value, nil
}

func (r *UserConfigRepository) Set(ctx context.Context, key, value string) error {
	return r.write.SetUserConfig(ctx, sqlc.SetUserConfigParams{Key: key, Value: value})
}

func (r *UserConfigRepository) List(ctx context.Context) ([]UserConfig, error) {
	return r.read.ListUserConfig(ctx)
}
