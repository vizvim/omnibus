-- name: UpsertIssue :one
-- Idempotent upsert keyed on the immutable comicvine_issue_id.
INSERT INTO issues (
  series_id, comicvine_issue_id, issue_number_raw, issue_number_sort,
  issue_number_qual, title, cover_date, store_date, release_date,
  description, image_url, cv_last_updated, issue_type, alt_issue_number,
  page_count, status, created_at
) VALUES (
  ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
)
ON CONFLICT(comicvine_issue_id) DO UPDATE SET
  issue_number_raw  = excluded.issue_number_raw,
  issue_number_sort = excluded.issue_number_sort,
  issue_number_qual = excluded.issue_number_qual,
  title             = excluded.title,
  cover_date        = excluded.cover_date,
  store_date        = excluded.store_date,
  release_date      = excluded.release_date,
  description       = excluded.description,
  image_url         = excluded.image_url,
  cv_last_updated   = excluded.cv_last_updated,
  issue_type        = excluded.issue_type,
  alt_issue_number  = excluded.alt_issue_number,
  page_count        = excluded.page_count
RETURNING *;

-- name: ListIssuesBySeries :many
SELECT * FROM issues
WHERE series_id = ?
ORDER BY issue_number_sort, COALESCE(issue_number_qual, '');

-- name: CountIssuesBySeries :one
SELECT count(*) FROM issues WHERE series_id = ?;

-- name: GetIssueByID :one
SELECT * FROM issues WHERE id = ?;

-- name: UpdateIssueStatus :exec
UPDATE issues SET status = ?, search_attempts = ? WHERE id = ?;

-- name: ListWantedForAutoSearch :many
-- Bounded batch of Wanted issues eligible for auto-search: fewest search_attempts
-- first (then oldest), excluding issues that have reached the attempt cap (cold).
-- The cap is passed as the upper bound (D-08/D-09).
SELECT * FROM issues
WHERE status = 'Wanted' AND search_attempts < ?
ORDER BY search_attempts ASC, created_at ASC
LIMIT ?;

-- name: IncrementSearchAttempts :exec
-- Records a no-result auto-search attempt; the growing count spaces retries and
-- eventually crosses the cap so the issue goes cold (D-09).
UPDATE issues SET search_attempts = search_attempts + 1 WHERE id = ?;

-- name: ListWantedForMatch :many
-- All currently-Wanted issues with their normalized sort/qual, for RSS feed matching.
SELECT * FROM issues
WHERE status = 'Wanted'
ORDER BY series_id, issue_number_sort;

-- name: IncrementDownloadAttempts :exec
-- Records a failed-grab / replacement cycle for an issue (D-13). download_attempts is
-- SEPARATE from search_attempts: it counts dead downloads, not no-result searches. The
-- growing count crosses the cap and parks the issue in cool-off (D-14).
UPDATE issues SET download_attempts = download_attempts + 1 WHERE id = ?;

-- name: ResetDownloadAttempts :exec
-- Clears the download_attempts cool-off so a manual retry can re-run the pipeline
-- immediately (DL-07 / D-14): the user's explicit retry is the intended cap reset.
UPDATE issues SET download_attempts = 0 WHERE id = ?;

-- name: ListWantedForDownloadRetry :many
-- A bounded batch of issues eligible for an attempt-capped replacement, mirroring the
-- search backoff but on the SEPARATE download_attempts counter (D-13/D-14): fewest
-- download_attempts first (then oldest), excluding issues at or above the cap (cool-off).
SELECT * FROM issues
WHERE download_attempts < ?
ORDER BY download_attempts ASC, created_at ASC
LIMIT ?;
