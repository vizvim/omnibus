// Package auth tests the optional single-user auth gate (AUTH-01): bcrypt hash/verify and
// the three-state config persisted over the existing user_config table, mirroring
// RenameConfigService. These were Wave-0 RED scaffolds (Plan 01); Plan 02 converts them
// into real assertions against the new service.
package auth_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/vizvim/omnibus/internal/auth"
	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository"
)

func newService(t *testing.T) *auth.Service {
	t.Helper()
	path := filepath.Join(t.TempDir(), "svc.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	repos := repository.NewRepositories(d)
	return auth.New(auth.Deps{Repos: repos, SessionSecret: "test-secret"})
}

// TestGetConfigFreshDBDefault asserts a fresh DB returns auth Off and not-configured —
// auth is opt-in (D-01).
func TestGetConfigFreshDBDefault(t *testing.T) {
	t.Parallel()
	svc := newService(t)

	got, err := svc.GetConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, auth.ModeOff, got.Mode, "auth is opt-in: a fresh DB defaults to Off")
	require.False(t, got.Configured, "no credential stored on a fresh DB")
	require.Equal(t, auth.DefaultAuthConfig(), got)
}

// TestVerifyCredentials asserts a correct username+password verifies against the stored
// bcrypt hash and a wrong password (or wrong user) does not.
func TestVerifyCredentials(t *testing.T) {
	t.Parallel()
	svc := newService(t)
	ctx := context.Background()

	// Configure a credential while keeping the gate Off (just storing the hash).
	_, err := svc.UpdateConfig(ctx, auth.Input{Mode: auth.ModeOff, Username: "admin", Password: "s3cret-pass"})
	require.NoError(t, err)

	ok, err := svc.VerifyCredentials(ctx, "admin", "s3cret-pass")
	require.NoError(t, err)
	require.True(t, ok, "correct username + password verifies")

	ok, err = svc.VerifyCredentials(ctx, "admin", "wrong")
	require.NoError(t, err)
	require.False(t, ok, "wrong password does not verify")

	ok, err = svc.VerifyCredentials(ctx, "someone-else", "s3cret-pass")
	require.NoError(t, err)
	require.False(t, ok, "wrong username does not verify")
}

// TestVerifyCredentialsNoCredential asserts verification fails (without error) when no
// credential is configured — there is nothing to match against.
func TestVerifyCredentialsNoCredential(t *testing.T) {
	t.Parallel()
	svc := newService(t)

	ok, err := svc.VerifyCredentials(context.Background(), "admin", "anything")
	require.NoError(t, err)
	require.False(t, ok)
}

// TestUpdateConfigStoresBcryptHashNotPlaintext asserts the stored credential is a bcrypt
// hash of the password (never plaintext) and the read view never exposes it.
func TestUpdateConfigStoresBcryptHashNotPlaintext(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "svc.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	repos := repository.NewRepositories(d)
	svc := auth.New(auth.Deps{Repos: repos, SessionSecret: "test-secret"})
	ctx := context.Background()

	cfg, err := svc.UpdateConfig(ctx, auth.Input{Mode: auth.ModeOff, Username: "admin", Password: "hunter2hunter2"})
	require.NoError(t, err)
	require.True(t, cfg.Configured, "a stored credential reports Configured")

	// Read the raw stored hash directly from the repo and assert it is a valid bcrypt hash
	// of the password — not the plaintext.
	row, err := repos.AuthConfig.Get(ctx)
	require.NoError(t, err)
	require.NotEqual(t, "hunter2hunter2", row.PasswordHash, "plaintext is never stored")
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte("hunter2hunter2")),
		"stored value is a bcrypt hash of the password")
}

// TestUpdateConfigEmptyPasswordKeepsHash asserts updating with an empty password preserves
// the existing hash (edit-without-retype), mirroring the DownloadClientForm masking rule.
func TestUpdateConfigEmptyPasswordKeepsHash(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "svc.db")
	require.NoError(t, db.Migrate(context.Background(), path))
	d, err := db.Open(context.Background(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	repos := repository.NewRepositories(d)
	svc := auth.New(auth.Deps{Repos: repos, SessionSecret: "test-secret"})
	ctx := context.Background()

	_, err = svc.UpdateConfig(ctx, auth.Input{Mode: auth.ModeOn, Username: "admin", Password: "original-pw"})
	require.NoError(t, err)
	before, err := repos.AuthConfig.Get(ctx)
	require.NoError(t, err)

	// Update again with an empty password but a changed username.
	cfg, err := svc.UpdateConfig(ctx, auth.Input{Mode: auth.ModeOn, Username: "admin2", Password: ""})
	require.NoError(t, err)
	require.True(t, cfg.Configured)
	require.Equal(t, "admin2", cfg.Username)

	after, err := repos.AuthConfig.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, before.PasswordHash, after.PasswordHash, "empty password keeps the stored hash")

	// The preserved credential still verifies against the original password.
	ok, err := svc.VerifyCredentials(ctx, "admin2", "original-pw")
	require.NoError(t, err)
	require.True(t, ok)
}

// TestUpdateConfigEnableWithoutCredentialRejected asserts enabling auth (mode != Off) with
// no stored credential fails closed (misconfig -> error), never silently fail-open (D-04).
func TestUpdateConfigEnableWithoutCredentialRejected(t *testing.T) {
	t.Parallel()
	svc := newService(t)
	ctx := context.Background()

	// No password ever stored: enabling On must be rejected.
	_, err := svc.UpdateConfig(ctx, auth.Input{Mode: auth.ModeOn, Username: "admin", Password: ""})
	require.ErrorIs(t, err, auth.ErrMissingCredential)

	// Enabling without a username is also rejected.
	_, err = svc.UpdateConfig(ctx, auth.Input{Mode: auth.ModeOn, Username: "", Password: "pw-with-no-user"})
	require.ErrorIs(t, err, auth.ErrMissingUsername)
}

// TestUpdateConfigRoundTrips asserts a persisted config Gets back its stored values.
func TestUpdateConfigRoundTrips(t *testing.T) {
	t.Parallel()
	svc := newService(t)
	ctx := context.Background()

	_, err := svc.UpdateConfig(ctx, auth.Input{Mode: auth.ModeBypassLocal, Username: "admin", Password: "round-trip-pw"})
	require.NoError(t, err)

	got, err := svc.GetConfig(ctx)
	require.NoError(t, err)
	require.Equal(t, auth.ModeBypassLocal, got.Mode)
	require.Equal(t, "admin", got.Username)
	require.True(t, got.Configured)
}
