-- 0002_covers: move cover art from the filesystem into SQLite. A dedicated covers
-- table holds the blob + content-type keyed on (entity_type, entity_id), and the
-- now-superseded cover_path columns are dropped from series and issues. PRAGMAs are
-- applied at connection open by internal/db (ADR 0002), not in DDL.

CREATE TABLE covers (
  entity_type  TEXT NOT NULL,
  entity_id    INTEGER NOT NULL,
  image        BLOB NOT NULL,
  content_type TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  PRIMARY KEY (entity_type, entity_id)
);

ALTER TABLE series DROP COLUMN cover_path;
ALTER TABLE issues DROP COLUMN cover_path;
