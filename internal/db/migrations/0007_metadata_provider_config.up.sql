-- 0007_metadata_provider_config: vendor-agnostic, DB-backed metadata-provider config.
-- Each metadata provider (ComicVine today; others later) persists its config as a row keyed
-- by provider id, so adding a provider needs no schema change. The api_key is stored plaintext
-- (single-user box, like the download-client config precedent in 0004) but is never returned
-- in any API response and is never logged. OMNIBUS_COMICVINE_API_KEY seeds the "comicvine"
-- row on first boot; thereafter the DB is the source of truth and the live provider resolves
-- its key from this table per request (no in-memory cache).
--
-- PRAGMAs are applied at connection open by internal/db, not in DDL.

CREATE TABLE metadata_provider_config (
  provider   TEXT PRIMARY KEY,
  api_key    TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
