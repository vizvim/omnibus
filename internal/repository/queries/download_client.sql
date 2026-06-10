-- name: GetDownloadClientConfig :one
SELECT * FROM download_client_config WHERE id = 1;

-- name: UpsertDownloadClientConfig :one
INSERT INTO download_client_config (
  id, url, api_key, category, created_at, updated_at
) VALUES (
  1, ?, ?, ?, ?, ?
)
ON CONFLICT(id) DO UPDATE SET
  url        = excluded.url,
  api_key    = excluded.api_key,
  category   = excluded.category,
  updated_at = excluded.updated_at
RETURNING *;
