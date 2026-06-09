package db_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
)

// openTestDB opens a migrated temp on-disk DB (modernc needs a real file for WAL).
func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestPragmasApplied(t *testing.T) {
	t.Parallel()
	d := openTestDB(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		pool *sql.DB
	}{
		{"read", d.Read},
		{"write", d.Write},
	} {
		var journalMode string
		require.NoError(t, tc.pool.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode))
		require.Equal(t, "wal", journalMode, "%s pool journal_mode", tc.name)

		var foreignKeys int
		require.NoError(t, tc.pool.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys))
		require.Equal(t, 1, foreignKeys, "%s pool foreign_keys", tc.name)

		var busyTimeout int
		require.NoError(t, tc.pool.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout))
		require.Equal(t, 5000, busyTimeout, "%s pool busy_timeout", tc.name)
	}
}

func TestWritePoolSingleConn(t *testing.T) {
	t.Parallel()
	d := openTestDB(t)
	require.Equal(t, 1, d.Write.Stats().MaxOpenConnections)
}

func TestConcurrentReaderWriterNoBusy(t *testing.T) {
	t.Parallel()
	d := openTestDB(t)
	ctx := context.Background()

	_, err := d.Write.ExecContext(ctx,
		`INSERT INTO publishers (name, created_at) VALUES ('Marvel', '2026-01-01T00:00:00Z')`)
	require.NoError(t, err)

	var wg sync.WaitGroup
	errs := make(chan error, 200)
	for range 100 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, e := d.Write.ExecContext(ctx,
				`UPDATE publishers SET name = 'Marvel' WHERE name = 'Marvel'`)
			errs <- e
		}()
		go func() {
			defer wg.Done()
			var n int
			errs <- d.Read.QueryRowContext(ctx, `SELECT count(*) FROM publishers`).Scan(&n)
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
}

func TestForeignKeyEnforced(t *testing.T) {
	t.Parallel()
	d := openTestDB(t)
	ctx := context.Background()

	// issues.series_id references series(id); inserting with a missing parent must fail.
	_, err := d.Write.ExecContext(ctx,
		`INSERT INTO issues (series_id, comicvine_issue_id, issue_number_raw, issue_number_sort, status, created_at)
		 VALUES (99999, 1, '1', 1.0, 'Wanted', '2026-01-01T00:00:00Z')`)
	require.Error(t, err)
	require.Contains(t, err.Error(), "FOREIGN KEY")
}
