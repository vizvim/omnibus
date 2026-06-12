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

-- name: ListActiveDownloads :many
-- The active poll set: rows still in flight (Queued/Downloading) that the DownloadPoll
-- job polls SAB for each tick. Ordered oldest-updated-first so the most stale rows are
-- reconciled first within a bounded tick (D-01).
SELECT * FROM downloads
WHERE status IN ('Queued','Downloading')
ORDER BY updated_at;

-- name: UpdateDownloadStatus :one
-- Flip a download row status (and bump updated_at). All Phase-5 status writes route
-- through the download-status state machine then this query, never a raw caller UPDATE.
UPDATE downloads
SET status = ?, updated_at = ?
WHERE id = ?
RETURNING id, issue_id, provider, release_key, release_title, size_bytes, status, client_ref, created_at, updated_at;

-- name: InsertDownloadHistory :one
-- Append-only download history (DL-03): record what was grabbed, from where, the outcome,
-- and when. Never updated or deleted; it is the audit trail of every grab + terminal flip.
INSERT INTO download_history (
  issue_id, provider, release_key, result, detail, occurred_at
) VALUES (
  ?, ?, ?, ?, ?, ?
)
RETURNING id, issue_id, provider, release_key, result, detail, occurred_at;

-- name: GetLatestCompletedStorageForIssue :one
-- The storage path the poll loop captured when a download for this issue completed
-- (DL-03: written into download_history.detail with result='completed'). The post-process
-- orchestrator (05-03) reads it to locate the file on disk for validate->render->import.
-- Most-recent-first so a re-grabbed release uses the latest completion. detail may be NULL
-- if the client reported an empty storage path.
SELECT detail FROM download_history
WHERE issue_id = ? AND provider = ? AND release_key = ? AND result = 'completed'
ORDER BY occurred_at DESC, id DESC
LIMIT 1;

-- name: ListDeadReleaseKeysForIssue :many
-- The (provider, release_key) pairs already known-bad for an issue: rows with a
-- Failed/Blacklisted downloads status. The replacement search excludes these so a
-- retry cannot re-pick a release that already failed for this issue (D-12 loop-prevention:
-- dedup against the downloads table, NOT the blacklists table).
SELECT provider, release_key FROM downloads
WHERE issue_id = ? AND status IN ('Failed','Blacklisted');
