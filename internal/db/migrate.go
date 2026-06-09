package db

import (
	"context"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite" // non-cgo "sqlite" driver
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// newMigrator builds a golang-migrate instance backed by the embedded migration
// files (iofs source) and the non-cgo modernc "sqlite" database driver. The caller
// must Close it. We construct the database driver from a *sql.DB opened with the
// modernc driver so migrations and runtime share the same pure-Go SQLite engine.
func newMigrator(ctx context.Context, path string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("build iofs migration source: %w", err)
	}

	conn, err := openMigrationConn(ctx, path)
	if err != nil {
		return nil, err
	}

	driver, err := sqlite.WithInstance(conn, &sqlite.Config{})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("build sqlite migrate driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "sqlite", driver)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("build migrator: %w", err)
	}
	return m, nil
}

// Migrate applies all pending up migrations to the SQLite file at path, tolerating
// ErrNoChange (already at the latest version). It runs at startup before the server
// accepts traffic.
func Migrate(ctx context.Context, path string) error {
	m, err := newMigrator(ctx, path)
	if err != nil {
		return err
	}
	defer closeMigrator(m)

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// MigrateDown rolls every migration back down (used by tests to prove
// reversibility). It tolerates ErrNoChange.
func MigrateDown(ctx context.Context, path string) error {
	m, err := newMigrator(ctx, path)
	if err != nil {
		return err
	}
	defer closeMigrator(m)

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("roll back migrations: %w", err)
	}
	return nil
}

func closeMigrator(m *migrate.Migrate) {
	srcErr, dbErr := m.Close()
	_ = srcErr
	_ = dbErr
}
