-- 0003_indexers: DB-backed indexer records (D-16). Newznab/GetComics sources are
-- configured in the DB rather than config so they can be UI-managed (SRCH-09). The
-- api_key is stored plaintext (D-17, single-user box) but is never returned in any
-- API response and is rendered as a password input (T-4-01).
--
-- PRAGMAs are applied at connection open by internal/db, not in DDL.

CREATE TABLE indexers (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  kind        TEXT NOT NULL CHECK (kind IN ('newznab','getcomics')),
  base_url    TEXT NOT NULL,
  api_key     TEXT,
  enabled     INTEGER NOT NULL DEFAULT 1,
  categories  TEXT,
  priority    INTEGER NOT NULL DEFAULT 0,
  use_for_rss INTEGER NOT NULL DEFAULT 1,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);
CREATE INDEX idx_indexers_enabled ON indexers(enabled);
