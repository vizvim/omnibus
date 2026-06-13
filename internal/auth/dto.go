package auth

import "github.com/vizvim/omnibus/internal/repository"

// Auth modes (D-01), the domain-string form persisted under the auth.mode user_config key
// and mapped to/from the AuthMode proto enum at the transport boundary.
const (
	// ModeOff disables the gate — every request passes (default on a fresh install, D-01).
	ModeOff = "off"
	// ModeOn gates every request: a valid session cookie is required (D-03/D-04).
	ModeOn = "on"
	// ModeBypassLocal gates remote requests but lets loopback/RFC1918 peers through,
	// keyed on the real socket peer IP (D-05).
	ModeBypassLocal = "bypass_local"
)

// Config is the domain READ view of the auth config the transport layer consumes. It
// deliberately omits the password hash: the credential is a secret (D-02), so only
// Configured (whether a hash is stored) is exposed — mirroring DownloadClientConfig's
// write-only APIKey. The username is not a secret and is returned verbatim.
type Config struct {
	Mode     string
	Username string
	// Configured is true when a password hash is stored. The hash itself is NEVER part of
	// this view.
	Configured bool
}

// Input carries the mutable fields for an update. Password is write-only: a non-empty
// value is bcrypt-hashed and persisted; an empty value preserves the stored hash
// (edit-without-retype, mirrors DownloadClientForm masking).
type Input struct {
	Mode     string
	Username string
	// Password is the plaintext password to hash, or empty to keep the existing hash. It
	// is never stored or returned in plaintext.
	Password string
}

// DefaultAuthConfig returns the default-Off posture a fresh install Gets. Auth is opt-in
// (D-01, ADR 0008): the gate passes everything until the user explicitly enables it.
func DefaultAuthConfig() Config {
	return Config{Mode: ModeOff, Username: "", Configured: false}
}

// configFromRow maps a persisted repository row to the domain READ view, dropping the
// password hash (it never crosses into the read view — only Configured reflects it).
func configFromRow(r repository.AuthConfigRow) Config {
	mode := r.Mode
	if mode == "" {
		mode = ModeOff
	}
	return Config{
		Mode:       mode,
		Username:   r.Username,
		Configured: r.PasswordHash != "",
	}
}
