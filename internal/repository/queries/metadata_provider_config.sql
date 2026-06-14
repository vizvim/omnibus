-- name: GetMetadataProviderConfig :one
SELECT * FROM metadata_provider_config WHERE provider = ?;

-- name: UpsertMetadataProviderConfig :one
INSERT INTO metadata_provider_config (
  provider, api_key, created_at, updated_at
) VALUES (
  ?, ?, ?, ?
)
ON CONFLICT(provider) DO UPDATE SET
  api_key    = excluded.api_key,
  updated_at = excluded.updated_at
RETURNING *;
