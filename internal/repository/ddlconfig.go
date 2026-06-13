package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// keyEnableDDL is the user_config key the DDL enable toggle is persisted under (05-07).
// Like the renaming config (D-09), the DDL flag is stored as a single key-value row in
// the existing user_config table rather than a dedicated table — it is a lone boolean.
const keyEnableDDL = "enable_ddl"

// DDLConfigRow is the domain view of the DDL config persisted over a single user_config
// key. Enabled is stored as the "true"/"false" string. Present is false when the key has
// never been written (fresh DB) so the service can apply the default-OFF posture.
type DDLConfigRow struct {
	Enabled bool

	// Present is false when the enable_ddl key has never been written (fresh DB). The
	// service uses this to return the default (disabled) rather than a coerced false.
	Present bool
}

// DDLConfigRepository persists the DDL enable toggle over the user_config key
// enable_ddl. Reads go through the read pool, writes through the single-writer pool
// (ADR 0002).
type DDLConfigRepository interface {
	// Get loads the enable_ddl key. When it has never been written the returned row has
	// Present == false (the service then supplies the default-OFF posture).
	Get(ctx context.Context) (DDLConfigRow, error)
	// Upsert persists the enable flag as the enable_ddl user_config key.
	Upsert(ctx context.Context, enabled bool) (DDLConfigRow, error)
}

type ddlConfigRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewDDLConfigRepository binds a DDLConfigRepository to the read and write pools.
func NewDDLConfigRepository(d *db.DB) DDLConfigRepository {
	return &ddlConfigRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

func (r *ddlConfigRepository) Get(ctx context.Context) (DDLConfigRow, error) {
	row, err := r.read.GetUserConfig(ctx, keyEnableDDL)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DDLConfigRow{}, nil
		}
		return DDLConfigRow{}, err
	}
	return DDLConfigRow{Enabled: row.Value == boolTrue, Present: true}, nil
}

func (r *ddlConfigRepository) Upsert(ctx context.Context, enabled bool) (DDLConfigRow, error) {
	if err := r.write.SetUserConfig(ctx, sqlc.SetUserConfigParams{
		Key:   keyEnableDDL,
		Value: boolToString(enabled),
	}); err != nil {
		return DDLConfigRow{}, err
	}
	return DDLConfigRow{Enabled: enabled, Present: true}, nil
}
