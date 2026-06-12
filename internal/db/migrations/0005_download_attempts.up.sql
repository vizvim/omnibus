-- 0005_download_attempts: add the issues.download_attempts counter (D-13).
-- This counts failed-grab / replacement cycles for an issue and is kept SEPARATE from
-- the existing search_attempts column (which counts "no acceptable candidate found").
-- A failing grab and an empty search are different failure modes with distinct caps and
-- reasons, so they get distinct counters. End-state additive migration (ADR 0003): the
-- column sits beside search_attempts with a 0 default so existing rows backfill cleanly.
--
-- PRAGMAs (WAL, busy_timeout, foreign_keys) are applied at connection open by
-- internal/db, not in DDL.

ALTER TABLE issues ADD COLUMN download_attempts INTEGER NOT NULL DEFAULT 0;
