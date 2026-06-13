// Package auth will own the optional single-user auth gate (AUTH-01): bcrypt
// hash/verify over the existing user_config table, mirroring RenameConfigService.
//
// This file is a Wave-0 RED scaffold. The internal/auth package does not exist yet
// (Plan 02 creates it), so these tests skip with a clear TODO instead of referencing
// not-yet-written types — keeping the suite compiling GREEN. Plan 02 converts each
// t.Skip into a real assertion against the new service.
//
// The blank import of golang.org/x/crypto/bcrypt is intentional: it pins x/crypto as a
// DIRECT module dependency now (AUTH-01's hash source, D-06) so `go mod tidy` keeps it in
// the require block before Plan 02 writes the production bcrypt usage.
package auth

import (
	"testing"

	_ "golang.org/x/crypto/bcrypt" // direct-dep anchor for AUTH-01 bcrypt (Plan 02)
)

// TestVerifyCredentials will assert that a correct username + password verifies against
// the stored bcrypt hash and a wrong password does not.
func TestVerifyCredentials(t *testing.T) {
	t.Parallel()
	t.Skip("Wave 0 scaffold — implemented in Plan 02 (AUTH-01 bcrypt verify)")
}

// TestUpdateConfigEmptyPasswordKeepsHash will assert that updating auth config with an
// empty password field preserves the existing password hash (edit-without-retype).
func TestUpdateConfigEmptyPasswordKeepsHash(t *testing.T) {
	t.Parallel()
	t.Skip("Wave 0 scaffold — implemented in Plan 02 (AUTH-01 update preserves hash)")
}

// TestGetConfigFreshDBDefault will assert that a fresh DB returns auth disabled by
// default (auth is opt-in).
func TestGetConfigFreshDBDefault(t *testing.T) {
	t.Parallel()
	t.Skip("Wave 0 scaffold — implemented in Plan 02 (AUTH-01 fresh-DB default off)")
}
