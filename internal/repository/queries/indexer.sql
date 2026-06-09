-- name: CreateIndexer :one
INSERT INTO indexers (
  name, kind, base_url, api_key, enabled, categories, priority, use_for_rss,
  created_at, updated_at
) VALUES (
  ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
RETURNING *;

-- name: GetIndexer :one
SELECT * FROM indexers WHERE id = ?;

-- name: ListIndexers :many
SELECT * FROM indexers
ORDER BY priority, name;

-- name: ListEnabledIndexers :many
SELECT * FROM indexers
WHERE enabled = 1
ORDER BY priority, name;

-- name: UpdateIndexer :one
UPDATE indexers SET
  name        = ?,
  kind        = ?,
  base_url    = ?,
  api_key     = ?,
  enabled     = ?,
  categories  = ?,
  priority    = ?,
  use_for_rss = ?,
  updated_at  = ?
WHERE id = ?
RETURNING *;

-- name: DeleteIndexer :exec
DELETE FROM indexers WHERE id = ?;
