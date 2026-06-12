-- name: DeleteIssueCredits :exec
-- Removes all credit rows for an issue, so Replace can rebuild them idempotently.
DELETE FROM issue_credits WHERE issue_id = ?;

-- name: InsertIssueCredit :exec
-- Inserts one normalized credit; duplicate (issue_id, role, name) rows are no-ops.
INSERT INTO issue_credits (issue_id, role, name, cv_person_id)
VALUES (?, ?, ?, ?)
ON CONFLICT DO NOTHING;

-- name: ListIssueCredits :many
SELECT * FROM issue_credits WHERE issue_id = ? ORDER BY role, name;
