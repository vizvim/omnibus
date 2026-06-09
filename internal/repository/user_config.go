package repository

import (
	"context"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// UserConfig is the domain view of a user_config row.
type UserConfig = sqlc.UserConfig

// UserConfigRepository persists typed user configuration keys (CFG-02).
type UserConfigRepository interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
	List(ctx context.Context) ([]UserConfig, error)
}

type userConfigRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewUserConfigRepository binds a UserConfigRepository to the read and write pools.
func NewUserConfigRepository(d *db.DB) UserConfigRepository {
	return &userConfigRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

func (r *userConfigRepository) Get(ctx context.Context, key string) (string, error) {
	row, err := r.read.GetUserConfig(ctx, key)
	if err != nil {
		return "", err
	}
	return row.Value, nil
}

func (r *userConfigRepository) Set(ctx context.Context, key, value string) error {
	return r.write.SetUserConfig(ctx, sqlc.SetUserConfigParams{Key: key, Value: value})
}

func (r *userConfigRepository) List(ctx context.Context) ([]UserConfig, error) {
	return r.read.ListUserConfig(ctx)
}
