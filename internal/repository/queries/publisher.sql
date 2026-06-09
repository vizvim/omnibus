-- name: UpsertPublisher :one
-- Idempotent on name (UNIQUE); records the CV publisher id when known.
INSERT INTO publishers (comicvine_publisher_id, name, created_at)
VALUES (?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
  comicvine_publisher_id = excluded.comicvine_publisher_id
RETURNING *;

-- name: GetPublisherByID :one
SELECT * FROM publishers WHERE id = ?;
