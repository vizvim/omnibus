-- 0003_drop_handrolled_jobs down: recreate the hand-owned jobs, idx_jobs_state, and
-- job_history exactly as 0001_init.up.sql defined them (same columns, CHECK
-- constraints, FK, and index) so the migration reverses cleanly. jobs is recreated
-- before job_history because job_history's FK references jobs(id).
CREATE TABLE jobs (
  id           INTEGER PRIMARY KEY,
  kind         TEXT NOT NULL,
  args_json    TEXT,
  state        TEXT NOT NULL DEFAULT 'queued'
                 CHECK (state IN ('queued','running','succeeded','failed','cancelled')),
  attempts     INTEGER NOT NULL DEFAULT 0,
  scheduled_at TEXT,
  started_at   TEXT,
  finished_at  TEXT,
  error        TEXT,
  created_at   TEXT NOT NULL
);
CREATE INDEX idx_jobs_state ON jobs(state);

CREATE TABLE job_history (
  id          INTEGER PRIMARY KEY,
  job_id      INTEGER REFERENCES jobs(id) ON DELETE SET NULL,
  kind        TEXT NOT NULL,
  state       TEXT NOT NULL,
  detail      TEXT,
  occurred_at TEXT NOT NULL
);
