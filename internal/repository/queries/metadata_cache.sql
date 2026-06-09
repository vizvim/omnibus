-- name: GetMetadataCache :one
SELECT * FROM metadata_cache WHERE cache_key = ?;

-- name: PutMetadataCache :exec
-- Idempotent on cache_key (UNIQUE); refreshes payload + freshness markers.
INSERT INTO metadata_cache (cache_key, payload, source_updated_at, fetched_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(cache_key) DO UPDATE SET
  payload           = excluded.payload,
  source_updated_at = excluded.source_updated_at,
  fetched_at        = excluded.fetched_at;
