-- 0005_download_attempts (down): drop the download_attempts counter (D-13) and revert the
-- issue_events CHECK to the 0001 type set (rebuild copy-swap; rows with the new
-- 'download-progress' type are dropped to satisfy the narrower CHECK).
ALTER TABLE issues DROP COLUMN download_attempts;

CREATE TABLE issue_events_old (
  id           INTEGER PRIMARY KEY,
  issue_id     INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  event_type   TEXT NOT NULL
                 CHECK (event_type IN ('searched','candidate-selected','snatched','failed','downloaded','processed')),
  payload_json TEXT,
  occurred_at  TEXT NOT NULL
);
INSERT INTO issue_events_old (id, issue_id, event_type, payload_json, occurred_at)
  SELECT id, issue_id, event_type, payload_json, occurred_at FROM issue_events
  WHERE event_type <> 'download-progress';
DROP TABLE issue_events;
ALTER TABLE issue_events_old RENAME TO issue_events;
CREATE INDEX idx_issue_events_issue ON issue_events(issue_id, occurred_at);
