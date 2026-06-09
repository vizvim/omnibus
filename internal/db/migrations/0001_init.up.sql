-- 0001_init: the full initial schema realized as the first golang-migrate migration.
-- PRAGMAs (WAL, busy_timeout, foreign_keys) are applied at connection open by
-- internal/db, not in DDL.

CREATE TABLE publishers (
  id                     INTEGER PRIMARY KEY,
  comicvine_publisher_id INTEGER UNIQUE,
  name                   TEXT NOT NULL,
  created_at             TEXT NOT NULL,
  UNIQUE(name)
);

CREATE TABLE series (
  id                  INTEGER PRIMARY KEY,
  comicvine_volume_id INTEGER NOT NULL UNIQUE,
  publisher_id        INTEGER REFERENCES publishers(id),
  name                TEXT NOT NULL,
  start_year          INTEGER,
  description         TEXT,
  status              TEXT NOT NULL DEFAULT 'Active'
                        CHECK (status IN ('Active','Paused','Ended')),
  cover_path          TEXT,
  total_issues        INTEGER NOT NULL DEFAULT 0,
  have_issues         INTEGER NOT NULL DEFAULT 0,
  settings_json       TEXT,
  last_refreshed_at   TEXT,
  created_at          TEXT NOT NULL
);
CREATE INDEX idx_series_publisher ON series(publisher_id);

CREATE TABLE issues (
  id                  INTEGER PRIMARY KEY,
  series_id           INTEGER NOT NULL REFERENCES series(id) ON DELETE CASCADE,
  comicvine_issue_id  INTEGER NOT NULL UNIQUE,
  issue_number_raw    TEXT NOT NULL,
  issue_number_sort   REAL NOT NULL,
  issue_number_qual   TEXT,
  title               TEXT,
  cover_date          TEXT,
  store_date          TEXT,
  release_date        TEXT,
  status              TEXT NOT NULL DEFAULT 'Wanted'
                        CHECK (status IN ('Wanted','Snatched','Downloaded','Archived','Skipped','Failed','Ignored')),
  search_attempts     INTEGER NOT NULL DEFAULT 0,
  location            TEXT,
  cover_path          TEXT,
  created_at          TEXT NOT NULL
);
CREATE INDEX idx_issues_series ON issues(series_id);
CREATE INDEX idx_issues_status ON issues(status);
CREATE UNIQUE INDEX uq_issues_series_number
  ON issues(series_id, issue_number_sort, COALESCE(issue_number_qual,''));

CREATE TABLE story_arcs (
  id               INTEGER PRIMARY KEY,
  comicvine_arc_id INTEGER UNIQUE,
  name             TEXT NOT NULL,
  created_at       TEXT NOT NULL
);

CREATE TABLE story_arc_issues (
  arc_id   INTEGER NOT NULL REFERENCES story_arcs(id) ON DELETE CASCADE,
  issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  position INTEGER NOT NULL,
  PRIMARY KEY (arc_id, issue_id)
);

CREATE TABLE downloads (
  id            INTEGER PRIMARY KEY,
  issue_id      INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  provider      TEXT NOT NULL,
  release_key   TEXT NOT NULL,
  release_title TEXT,
  size_bytes    INTEGER,
  status        TEXT NOT NULL DEFAULT 'Queued'
                  CHECK (status IN ('Queued','Downloading','Completed','Failed','Blacklisted')),
  client_ref    TEXT,
  created_at    TEXT NOT NULL,
  updated_at    TEXT NOT NULL,
  UNIQUE(provider, release_key, issue_id)
);
CREATE INDEX idx_downloads_issue ON downloads(issue_id);

CREATE TABLE download_history (
  id          INTEGER PRIMARY KEY,
  issue_id    INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  provider    TEXT NOT NULL,
  release_key TEXT NOT NULL,
  result      TEXT NOT NULL,
  detail      TEXT,
  occurred_at TEXT NOT NULL
);
CREATE INDEX idx_dlhist_issue ON download_history(issue_id);

CREATE TABLE blacklists (
  id          INTEGER PRIMARY KEY,
  issue_id    INTEGER REFERENCES issues(id) ON DELETE CASCADE,
  series_id   INTEGER REFERENCES series(id) ON DELETE CASCADE,
  provider    TEXT NOT NULL,
  release_key TEXT NOT NULL,
  reason      TEXT,
  created_at  TEXT NOT NULL,
  UNIQUE(provider, release_key, issue_id)
);

CREATE TABLE metadata_cache (
  id                INTEGER PRIMARY KEY,
  cache_key         TEXT NOT NULL UNIQUE,
  payload           TEXT NOT NULL,
  source_updated_at TEXT,
  fetched_at        TEXT NOT NULL
);

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

CREATE TABLE user_config (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE issue_events (
  id           INTEGER PRIMARY KEY,
  issue_id     INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
  event_type   TEXT NOT NULL
                 CHECK (event_type IN ('searched','candidate-selected','snatched','failed','downloaded','processed')),
  payload_json TEXT,
  occurred_at  TEXT NOT NULL
);
CREATE INDEX idx_issue_events_issue ON issue_events(issue_id, occurred_at);
