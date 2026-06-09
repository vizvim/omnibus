-- name: UpsertSeries :one
-- Idempotent upsert keyed on the immutable comicvine_volume_id.
INSERT INTO series (
  comicvine_volume_id, publisher_id, name, start_year, description,
  status, total_issues, have_issues, settings_json,
  last_refreshed_at, created_at
) VALUES (
  ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
ON CONFLICT(comicvine_volume_id) DO UPDATE SET
  publisher_id      = excluded.publisher_id,
  name              = excluded.name,
  start_year        = excluded.start_year,
  description       = excluded.description,
  settings_json     = excluded.settings_json,
  last_refreshed_at = excluded.last_refreshed_at
RETURNING *;

-- name: GetSeriesByVolumeID :one
SELECT * FROM series WHERE comicvine_volume_id = ?;

-- name: GetSeriesByID :one
SELECT * FROM series WHERE id = ?;

-- name: ListSeries :many
SELECT * FROM series
ORDER BY name
LIMIT ? OFFSET ?;

-- name: UpdateSeriesSettings :one
UPDATE series SET status = ?, settings_json = ? WHERE id = ?
RETURNING *;

-- name: UpdateSeriesCounts :exec
UPDATE series SET total_issues = ?, have_issues = ? WHERE id = ?;

-- name: UpdateSeriesLastRefreshed :exec
UPDATE series SET last_refreshed_at = ? WHERE id = ?;

-- name: ListStaleSeries :many
-- Active series never refreshed (NULL) or last refreshed before the cutoff, oldest
-- first (NULLs lead). Bounded by LIMIT to keep each sweep tick small. Paused/Ended
-- series are excluded: they are not being watched for freshness.
SELECT * FROM series
WHERE status = 'Active'
  AND (last_refreshed_at IS NULL OR last_refreshed_at < ?)
ORDER BY last_refreshed_at IS NOT NULL, last_refreshed_at
LIMIT ?;
