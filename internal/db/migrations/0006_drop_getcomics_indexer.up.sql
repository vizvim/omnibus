-- 0006_drop_getcomics_indexer (up): GetComics is no longer an indexer kind (05-07). It
-- becomes a built-in static DDL source toggled by the enable_ddl user_config flag, never
-- a per-user indexer row. This migration:
--   1. deletes any existing kind='getcomics' indexer rows (idempotent — safe on zero rows), and
--   2. rebuilds the indexers table so the kind CHECK constraint is newznab-only.
--
-- SQLite cannot ALTER a CHECK constraint in place, so this follows the standard
-- create-new / copy / drop / rename table-rebuild idiom, preserving every column and the
-- idx_indexers_enabled index.

DELETE FROM indexers WHERE kind = 'getcomics';

CREATE TABLE indexers_new (
  id          INTEGER PRIMARY KEY,
  name        TEXT NOT NULL UNIQUE,
  kind        TEXT NOT NULL CHECK (kind IN ('newznab')),
  base_url    TEXT NOT NULL,
  api_key     TEXT,
  enabled     INTEGER NOT NULL DEFAULT 1,
  categories  TEXT,
  priority    INTEGER NOT NULL DEFAULT 0,
  use_for_rss INTEGER NOT NULL DEFAULT 1,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);

INSERT INTO indexers_new
  (id, name, kind, base_url, api_key, enabled, categories, priority, use_for_rss, created_at, updated_at)
SELECT
  id, name, kind, base_url, api_key, enabled, categories, priority, use_for_rss, created_at, updated_at
FROM indexers;

DROP TABLE indexers;
ALTER TABLE indexers_new RENAME TO indexers;
CREATE INDEX idx_indexers_enabled ON indexers(enabled);
