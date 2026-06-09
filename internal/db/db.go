// Package db owns SQLite connection management: a single-writer write pool and a
// multi-connection read pool, both with WAL, busy_timeout, and foreign-key PRAGMAs
// applied to every connection. It also runs the embedded golang-migrate
// migrations. Repositories receive the *sql.DB pools; they never open
// their own connections.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" driver
)

// driverName is modernc's registered database/sql driver name (NOT "sqlite3", which
// is the cgo mattn driver).
const driverName = "sqlite"

// DB holds the read and write connection pools. Write is capped at a single
// connection so all writes serialize through one writer; Read may use
// many connections. Both pools point at the same file and carry the same PRAGMAs.
type DB struct {
	Read  *sql.DB
	Write *sql.DB
}

// dsn builds a modernc DSN that applies the WAL, busy_timeout, and foreign-key
// PRAGMAs on every connection in the pool via the `_pragma=` query-param form (one
// param per PRAGMA).
func dsn(path string) string {
	return fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)",
		path,
	)
}

// Open opens the read and write pools against the SQLite file at path. The caller is
// responsible for running Migrate before Open if the schema must exist.
func Open(ctx context.Context, path string) (*DB, error) {
	connStr := dsn(path)

	writeDB, err := sql.Open(driverName, connStr)
	if err != nil {
		return nil, fmt.Errorf("open write pool: %w", err)
	}
	// Single writer: every write serializes through one connection.
	writeDB.SetMaxOpenConns(1)

	readDB, err := sql.Open(driverName, connStr)
	if err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("open read pool: %w", err)
	}
	readMax := max(runtime.NumCPU(), 4)
	readDB.SetMaxOpenConns(readMax)

	if err := writeDB.PingContext(ctx); err != nil {
		_ = writeDB.Close()
		_ = readDB.Close()
		return nil, fmt.Errorf("ping write pool: %w", err)
	}
	if err := readDB.PingContext(ctx); err != nil {
		_ = writeDB.Close()
		_ = readDB.Close()
		return nil, fmt.Errorf("ping read pool: %w", err)
	}

	return &DB{Read: readDB, Write: writeDB}, nil
}

// openMigrationConn opens a dedicated single-connection *sql.DB for golang-migrate.
// It carries the same PRAGMAs as the runtime pools so DDL with FK references applies
// cleanly. Migrations must serialize, so MaxOpenConns is 1.
func openMigrationConn(ctx context.Context, path string) (*sql.DB, error) {
	conn, err := sql.Open(driverName, dsn(path))
	if err != nil {
		return nil, fmt.Errorf("open migration connection: %w", err)
	}
	conn.SetMaxOpenConns(1)
	if err := conn.PingContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ping migration connection: %w", err)
	}
	return conn, nil
}

// Close closes both pools, read first then write last, mirroring the shutdown order
// (in-flight reads drain before the writer is torn down).
func (d *DB) Close() error {
	var firstErr error
	if d.Read != nil {
		if err := d.Read.Close(); err != nil {
			firstErr = fmt.Errorf("close read pool: %w", err)
		}
	}
	if d.Write != nil {
		if err := d.Write.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("close write pool: %w", err)
		}
	}
	return firstErr
}
