-- 0007_comicvine_config: singleton DB-backed ComicVine metadata-provider config.
-- ComicVine is the only metadata provider today; the api_key now persists in the DB and is
-- editable at runtime via MetadataProviderService, while OMNIBUS_COMICVINE_API_KEY seeds
-- this row on first boot. The api_key is stored plaintext (single-user box, like the
-- download-client config precedent in 0004) but is never returned in any API response and
-- is never logged. The CHECK(id = 1) constraint enforces a single config row.
--
-- PRAGMAs are applied at connection open by internal/db, not in DDL.

CREATE TABLE comicvine_config (
  id         INTEGER PRIMARY KEY CHECK (id = 1),
  api_key    TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
