-- name: UpsertIssue :one
-- Idempotent upsert keyed on the immutable comicvine_issue_id.
INSERT INTO issues (
  series_id, comicvine_issue_id, issue_number_raw, issue_number_sort,
  issue_number_qual, title, cover_date, store_date, release_date,
  status, created_at
) VALUES (
  ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
ON CONFLICT(comicvine_issue_id) DO UPDATE SET
  issue_number_raw  = excluded.issue_number_raw,
  issue_number_sort = excluded.issue_number_sort,
  issue_number_qual = excluded.issue_number_qual,
  title             = excluded.title,
  cover_date        = excluded.cover_date,
  store_date        = excluded.store_date,
  release_date      = excluded.release_date
RETURNING *;

-- name: ListIssuesBySeries :many
SELECT * FROM issues
WHERE series_id = ?
ORDER BY issue_number_sort, COALESCE(issue_number_qual, '');

-- name: CountIssuesBySeries :one
SELECT count(*) FROM issues WHERE series_id = ?;

-- name: UpdateIssueStatus :exec
UPDATE issues SET status = ?, search_attempts = ? WHERE id = ?;
