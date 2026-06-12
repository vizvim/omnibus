-- name: InsertIssueEvent :one
INSERT INTO issue_events (issue_id, event_type, payload_json, occurred_at)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: ListIssueEventsByIssue :many
SELECT * FROM issue_events WHERE issue_id = ? ORDER BY occurred_at, id;
