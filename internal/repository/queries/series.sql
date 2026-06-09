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
