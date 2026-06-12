-- 0004_download_client_config: singleton DB-backed SABnzbd download-client config
-- (supersedes D-16: SAB config now persists in the DB and is editable at runtime via
-- DownloadClientService, while OMNIBUS_SABNZBD_* values seed this row on first boot).
-- The api_key is stored plaintext (D-17, single-user box, like indexers) but is never
-- returned in any API response and is rendered as a password input (T-ul5-01/T-ul5-03).
-- The CHECK(id = 1) constraint enforces a single config row.
--
-- PRAGMAs are applied at connection open by internal/db, not in DDL.

CREATE TABLE download_client_config (
  id         INTEGER PRIMARY KEY CHECK (id = 1),
  url        TEXT NOT NULL DEFAULT '',
  api_key    TEXT,
  category   TEXT NOT NULL DEFAULT 'comics',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
