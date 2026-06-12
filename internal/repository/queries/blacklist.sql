-- name: AddBlacklist :exec
-- Per-issue blacklist of a specific release (D-11): issue_id is set, series_id is NULL
-- (series-wide blacklist is deferred). The UNIQUE(provider, release_key, issue_id)
-- constraint makes a duplicate blacklist of the same release for the same issue a no-op.
INSERT INTO blacklists (
  issue_id, series_id, provider, release_key, reason, created_at
) VALUES (
  ?, NULL, ?, ?, ?, ?
)
ON CONFLICT(provider, release_key, issue_id) DO NOTHING;

-- name: ListBlacklistForIssue :many
-- The (provider, release_key) pairs a user has blacklisted for an issue (D-11). The
-- replacement search unions these with the dead download keys to build its BlacklistSet.
SELECT provider, release_key FROM blacklists
WHERE issue_id = ?;
