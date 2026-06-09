-- name: CreateDownload :one
-- Idempotent upsert on the existing UNIQUE(provider, release_key, issue_id) index:
-- a duplicate grab of the same release for the same issue updates in place (T-4-02).
INSERT INTO downloads (
  issue_id, provider, release_key, release_title, size_bytes, status, client_ref,
  created_at, updated_at
) VALUES (
  ?, ?, ?, ?, ?, ?, ?, ?, ?
)
ON CONFLICT(provider, release_key, issue_id) DO UPDATE SET
  release_title = excluded.release_title,
  size_bytes    = excluded.size_bytes,
  status        = excluded.status,
  client_ref    = excluded.client_ref,
  updated_at    = excluded.updated_at
RETURNING *;

-- name: GetDownloadByID :one
SELECT * FROM downloads WHERE id = ?;

-- name: ListDownloadsByIssue :many
SELECT * FROM downloads WHERE issue_id = ? ORDER BY created_at;
