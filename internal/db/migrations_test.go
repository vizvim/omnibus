package db_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/db"
)

// allTables is every table the schema defines (the full schema shipped in 0001).
var allTables = []string{
	"blacklists", "covers", "download_history", "downloads", "issue_events", "issues",
	"job_history", "jobs", "metadata_cache", "publishers", "series",
	"story_arc_issues", "story_arcs", "user_config",
}

func tableNames(t *testing.T, d *sql.DB) []string {
	t.Helper()
	rows, err := d.QueryContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'schema_migrations'`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var names []string
	for rows.Next() {
		var n string
		require.NoError(t, rows.Scan(&n))
		names = append(names, n)
	}
	require.NoError(t, rows.Err())
	sort.Strings(names)
	return names
}

func TestMigrateUpCreatesAllTables(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "m.db")
	require.NoError(t, db.Migrate(context.Background(), path))

	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	require.ElementsMatch(t, allTables, tableNames(t, d.Read))
}

func TestMigrateDownRemovesTables(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "m.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	require.NoError(t, db.MigrateDown(context.Background(), path))

	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	require.Empty(t, tableNames(t, d.Read))
}

func TestMigrateUpDownUpRoundTrips(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "m.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	require.NoError(t, db.MigrateDown(context.Background(), path))
	require.NoError(t, db.Migrate(context.Background(), path))
	// Re-running Up is a no-op (ErrNoChange tolerated inside Migrate).
	require.NoError(t, db.Migrate(context.Background(), path))

	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	require.ElementsMatch(t, allTables, tableNames(t, d.Read))
}

func TestCompositeIssueNumberIndexDistinctness(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "m.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	ctx := context.Background()

	_, err = d.Write.ExecContext(ctx,
		`INSERT INTO series (comicvine_volume_id, name, created_at) VALUES (4050, 'S', '2026-01-01T00:00:00Z')`)
	require.NoError(t, err)
	var seriesID int64
	require.NoError(t, d.Read.QueryRowContext(ctx,
		`SELECT id FROM series WHERE comicvine_volume_id = 4050`).Scan(&seriesID))

	insertIssue := func(cvID int64, sort float64, qual any) error {
		_, e := d.Write.ExecContext(ctx,
			`INSERT INTO issues (series_id, comicvine_issue_id, issue_number_raw, issue_number_sort, issue_number_qual, status, created_at)
			 VALUES (?, ?, ?, ?, ?, 'Wanted', '2026-01-01T00:00:00Z')`,
			seriesID, cvID, "7", sort, qual)
		return e
	}

	// (series, 7.0, NULL) and (series, 7.0, 'INH') are distinct.
	require.NoError(t, insertIssue(1, 7.0, nil))
	require.NoError(t, insertIssue(2, 7.0, "INH"))

	// Duplicate (series, 7.0, NULL) is rejected by the composite unique index.
	err = insertIssue(3, 7.0, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "UNIQUE")
}
