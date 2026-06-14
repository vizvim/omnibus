package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository/sqlc"
)

// Auth user_config keys. The optional-auth config (AUTH-01, ADR 0008) is stored as a
// small set of key-value rows in the existing user_config table rather than a dedicated
// table (no migration needed — D-02, mirrors the renaming config D-09). The password is
// stored ONLY as a bcrypt hash under auth.password_hash; plaintext is never persisted.
const (
	keyAuthMode         = "auth.mode"
	keyAuthEnabled      = "auth.enabled"
	keyAuthUsername     = "auth.username"
	keyAuthPasswordHash = "auth.password_hash"
)

// AuthConfigRow is the domain view of the auth config persisted across several user_config
// keys. Mode is stored as its string form ("off"/"on"/"bypass_local"); the password is the
// bcrypt hash (never plaintext). Absent keys are reported via Present so the service can
// apply the default (Off) only when nothing has ever been written.
type AuthConfigRow struct {
	Mode         string
	Username     string
	PasswordHash string

	// Present is false when NO auth key has ever been written (fresh DB). The service
	// uses this to return the default (Off) rather than coerced zero values.
	Present bool
}

// AuthConfigUpsert carries the auth fields to persist. PasswordHash, when non-empty, is
// the already-bcrypt-hashed credential; the service hashes plaintext before calling here.
// An empty PasswordHash means "leave the stored hash untouched" (edit-without-retype).
type AuthConfigUpsert struct {
	Mode     string
	Username string
	// PasswordHash is the bcrypt hash to persist. Empty means do not touch the stored hash.
	PasswordHash string
}

// AuthConfigRepository persists the auth config over user_config keys. Reads go through
// the read pool, writes through the single-writer pool (ADR 0002).
type AuthConfigRepository struct {
	read  *sqlc.Queries
	write *sqlc.Queries
}

// NewAuthConfigRepository binds an AuthConfigRepository to the read and write pools.
func NewAuthConfigRepository(d *db.DB) *AuthConfigRepository {
	return &AuthConfigRepository{read: sqlc.New(d.Read), write: sqlc.New(d.Write)}
}

// Get loads each auth key. When no auth key has been written yet the returned row has
// Present == false (the service then supplies the default-Off posture). The password hash
// is returned for credential verification but never propagates past the service's read view.
func (r *AuthConfigRepository) Get(ctx context.Context) (AuthConfigRow, error) {
	var out AuthConfigRow

	type field struct {
		key    string
		assign func(value string)
	}
	fields := []field{
		{keyAuthMode, func(v string) { out.Mode = v }},
		{keyAuthUsername, func(v string) { out.Username = v }},
		{keyAuthPasswordHash, func(v string) { out.PasswordHash = v }},
	}

	for _, f := range fields {
		value, present, err := r.getKey(ctx, f.key)
		if err != nil {
			return AuthConfigRow{}, err
		}
		if present {
			out.Present = true
			f.assign(value)
		}
	}

	return out, nil
}

// getKey loads a single user_config value, reporting absence via (value, present, err).
func (r *AuthConfigRepository) getKey(ctx context.Context, key string) (string, bool, error) {
	row, err := r.read.GetUserConfig(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	return row.Value, true, nil
}

// Upsert persists the auth mode + username. The password hash is written only when
// non-empty (an empty hash preserves the existing credential — edit-without-retype). The
// legacy auth.enabled boolean is kept in sync for any reader that checks it (true unless Off).
func (r *AuthConfigRepository) Upsert(ctx context.Context, in AuthConfigUpsert) (AuthConfigRow, error) {
	pairs := [][2]string{
		{keyAuthMode, in.Mode},
		{keyAuthUsername, in.Username},
		{keyAuthEnabled, boolToString(in.Mode != "" && in.Mode != "off")},
	}
	if in.PasswordHash != "" {
		pairs = append(pairs, [2]string{keyAuthPasswordHash, in.PasswordHash})
	}

	for _, p := range pairs {
		if err := r.write.SetUserConfig(ctx, sqlc.SetUserConfigParams{Key: p[0], Value: p[1]}); err != nil {
			return AuthConfigRow{}, err
		}
	}

	// Re-read the stored hash so the returned row reflects the persisted credential
	// (whether just-written or preserved).
	hash := in.PasswordHash
	if hash == "" {
		stored, _, err := r.getKey(ctx, keyAuthPasswordHash)
		if err != nil {
			return AuthConfigRow{}, err
		}
		hash = stored
	}

	return AuthConfigRow{
		Mode:         in.Mode,
		Username:     in.Username,
		PasswordHash: hash,
		Present:      true,
	}, nil
}
