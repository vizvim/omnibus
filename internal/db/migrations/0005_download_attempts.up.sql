-- 0005_download_attempts: add the issues.download_attempts counter (D-13) and admit a
-- 'download-progress' issue_events type (DL-08).
--
-- download_attempts counts failed-grab / replacement cycles for an issue and is kept
-- SEPARATE from the existing search_attempts column (which counts "no acceptable candidate
-- found"). A failing grab and an empty search are different failure modes with distinct caps
-- and reasons, so they get distinct counters. End-state additive migration (ADR 0003): the
-- column sits beside search_attempts with a 0 default so existing rows backfill cleanly.
--
-- The issue_events CHECK is widened to admit 'download-progress' so the poll job can append a
-- progress entry to the per-issue timeline (DL-08). SQLite cannot ALTER a CHECK in place, so
-- the table is rebuilt via the standard create-copy-swap pattern, preserving the FK, the
-- index, and existing rows.
--
-- PRAGMAs (WAL, busy_timeout, foreign_keys) are applied at connection open by
-- internal/db, not in DDL.

ALTER TABLE issues ADD COLUMN download_attempts INTEGER NOT NULL DEFAULT 0;

CREATE TABLE issue_events_new (
  id           INTEGER PRIMARY KEY,
  issue_id     INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  event_type   TEXT NOT NULL
                 CHECK (event_type IN ('searched','candidate-selected','snatched','download-progress','failed','downloaded','processed')),
  payload_json TEXT,
  occurred_at  TEXT NOT NULL
);
INSERT INTO issue_events_new (id, issue_id, event_type, payload_json, occurred_at)
  SELECT id, issue_id, event_type, payload_json, occurred_at FROM issue_events;
DROP TABLE issue_events;
ALTER TABLE issue_events_new RENAME TO issue_events;
CREATE INDEX idx_issue_events_issue ON issue_events(issue_id, occurred_at);
