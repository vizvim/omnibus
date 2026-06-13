-- 0006_drop_getcomics_indexer (down): restore the kind CHECK to permit getcomics again,
-- reverting the newznab-only constraint. Rebuilds the indexers table back to the
-- pre-0006 CHECK(kind IN ('newznab','getcomics')), preserving every column + the
-- idx_indexers_enabled index. (Dropped getcomics rows are NOT restored — the up
-- migration's DELETE is not reversible.)

CREATE TABLE indexers_new (
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

INSERT INTO indexers_new
  (id, name, kind, base_url, api_key, enabled, categories, priority, use_for_rss, created_at, updated_at)
SELECT
  id, name, kind, base_url, api_key, enabled, categories, priority, use_for_rss, created_at, updated_at
FROM indexers;

DROP TABLE indexers;
ALTER TABLE indexers_new RENAME TO indexers;
CREATE INDEX idx_indexers_enabled ON indexers(enabled);
