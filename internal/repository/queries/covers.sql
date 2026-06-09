-- name: UpsertCover :exec
-- Idempotent upsert of a cover blob keyed on (entity_type, entity_id).
INSERT INTO covers (entity_type, entity_id, image, content_type, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(entity_type, entity_id) DO UPDATE SET
  image        = excluded.image,
  content_type = excluded.content_type,
  updated_at   = excluded.updated_at;

-- name: GetCover :one
SELECT image, content_type FROM covers WHERE entity_type = ? AND entity_id = ?;

-- name: CoverExists :one
SELECT EXISTS(SELECT 1 FROM covers WHERE entity_type = ? AND entity_id = ?);
