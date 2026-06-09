-- name: GetUserConfig :one
SELECT * FROM user_config WHERE key = ?;

-- name: SetUserConfig :exec
INSERT INTO user_config (key, value)
VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value;

-- name: ListUserConfig :many
SELECT * FROM user_config ORDER BY key;
