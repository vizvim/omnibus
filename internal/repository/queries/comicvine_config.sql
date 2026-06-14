-- name: GetComicVineConfig :one
SELECT * FROM comicvine_config WHERE id = 1;

-- name: UpsertComicVineConfig :one
INSERT INTO comicvine_config (
  id, api_key, created_at, updated_at
) VALUES (
  1, ?, ?, ?
)
ON CONFLICT(id) DO UPDATE SET
  api_key    = excluded.api_key,
  updated_at = excluded.updated_at
RETURNING *;
