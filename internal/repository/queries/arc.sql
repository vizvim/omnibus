-- name: UpsertArc :one
-- Idempotent on the immutable comicvine_arc_id.
INSERT INTO story_arcs (comicvine_arc_id, name, created_at)
VALUES (?, ?, ?)
ON CONFLICT(comicvine_arc_id) DO UPDATE SET
  name = excluded.name
RETURNING *;

-- name: LinkArcIssue :exec
INSERT INTO story_arc_issues (arc_id, issue_id, position)
VALUES (?, ?, ?)
ON CONFLICT(arc_id, issue_id) DO UPDATE SET
  position = excluded.position;

-- name: ListArcsBySeries :many
SELECT DISTINCT sa.*
FROM story_arcs sa
JOIN story_arc_issues sai ON sai.arc_id = sa.id
JOIN issues i ON i.id = sai.issue_id
WHERE i.series_id = ?
ORDER BY sa.name;
