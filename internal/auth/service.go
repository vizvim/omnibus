// Package auth owns the optional single-user auth gate (AUTH-01, ADR 0008): it persists
// the three-state auth setting + bcrypt-hashed credential as user_config keys (mirroring
// RenameConfigService, D-02), verifies credentials with bcrypt, and signs/verifies a
// stateless HMAC session token the Login handler issues as a cookie. It depends only on
// the repository interface and slog. Enforcement (the fail-closed gate + local-bypass)
// lives in the transport http.Handler middleware, which calls this service.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/vizvim/omnibus/internal/repository"
)

// sessionTTL bounds how long an issued session token is valid before VerifySession
// rejects it as expired. A bounded lifetime caps the window of a stolen cookie (V3).
const sessionTTL = 30 * 24 * time.Hour

// ErrInvalidSession is returned by VerifySession when a token is malformed, tampered, or
// expired. The gate treats any such error as "no valid session" and fails closed (D-04).
var ErrInvalidSession = errors.New("invalid session token")

// ErrMissingCredential is returned when auth is enabled (mode != Off) but no password
// hash has ever been stored — a misconfiguration that must fail closed (D-04), never
// silently fail open. Mapped to InvalidArgument by transport.
var ErrMissingCredential = errors.New("a username and password must be configured before enabling auth")

// ErrMissingUsername is returned when auth is enabled without a username. Mapped to
// InvalidArgument by transport.
var ErrMissingUsername = errors.New("a username is required when auth is enabled")

// Deps are the Service's injected collaborators.
type Deps struct {
	Repos  *repository.Repositories
	Logger *slog.Logger
	// SessionSecret signs the stateless session token (HMAC-SHA256). It must be non-empty
	// when auth is used; the app layer sources it from config (redacted in logs).
	SessionSecret string
}

// Service implements AuthService domain logic over AuthConfigRepository.
type Service struct {
	repo   *repository.AuthConfigRepository
	logger *slog.Logger
	secret []byte
}

// New constructs a Service.
func New(d Deps) *Service {
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repo: d.Repos.AuthConfig, logger: logger, secret: []byte(d.SessionSecret)}
}

// GetConfig returns the auth config READ view. On a fresh DB (no auth key written yet) it
// returns the D-01 default (Off, not configured). The password hash is never part of the
// returned Config — only Configured reflects whether a credential is stored.
func (s *Service) GetConfig(ctx context.Context) (Config, error) {
	row, err := s.repo.Get(ctx)
	if err != nil {
		return Config{}, fmt.Errorf("get auth config: %w", err)
	}
	if !row.Present {
		return DefaultAuthConfig(), nil
	}
	return configFromRow(row), nil
}

// UpdateConfig validates and persists the auth config. A non-empty password is
// bcrypt-hashed (DefaultCost) and stored; an empty password preserves the existing hash
// (edit-without-retype, D-02). Enabling auth (mode != Off) without a stored credential —
// either no username or no hash after this update — is rejected (misconfig -> fail-closed,
// D-04). The returned Config never carries the hash.
func (s *Service) UpdateConfig(ctx context.Context, in Input) (Config, error) {
	mode := normalizeMode(in.Mode)
	username := strings.TrimSpace(in.Username)

	// Load the existing row so we can reason about the credential after this update
	// (empty password keeps the prior hash).
	existing, err := s.repo.Get(ctx)
	if err != nil {
		return Config{}, fmt.Errorf("get auth config: %w", err)
	}

	var hashToStore string
	if in.Password != "" {
		hashed, hashErr := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if hashErr != nil {
			return Config{}, fmt.Errorf("hash password: %w", hashErr)
		}
		hashToStore = string(hashed)
	}

	// The effective hash after this update: the new one if provided, else the stored one.
	effectiveHash := hashToStore
	if effectiveHash == "" {
		effectiveHash = existing.PasswordHash
	}

	// Fail-closed validation: enabling auth requires a username AND a stored credential.
	if mode != ModeOff {
		if username == "" {
			return Config{}, ErrMissingUsername
		}
		if effectiveHash == "" {
			return Config{}, ErrMissingCredential
		}
	}

	row, err := s.repo.Upsert(ctx, repository.AuthConfigUpsert{
		Mode:         mode,
		Username:     username,
		PasswordHash: hashToStore, // empty => repository preserves the existing hash
	})
	if err != nil {
		return Config{}, fmt.Errorf("update auth config: %w", err)
	}
	return configFromRow(row), nil
}

// VerifyCredentials returns true only when username matches the stored username AND the
// password matches the stored bcrypt hash. Any mismatch — wrong user, wrong password, or
// no credential configured — returns false (never an error oracle for the caller). A
// repository read error is surfaced so the gate can fail closed.
func (s *Service) VerifyCredentials(ctx context.Context, username, password string) (bool, error) {
	row, err := s.repo.Get(ctx)
	if err != nil {
		return false, fmt.Errorf("get auth config: %w", err)
	}
	if !row.Present || row.PasswordHash == "" {
		return false, nil
	}
	if username != row.Username {
		return false, nil
	}
	if err := bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte(password)); err != nil {
		// A hash mismatch is the expected "wrong password" path, not a surfaced error: the
		// caller (Login/gate) must not distinguish it from "no such user" (no oracle, D-04).
		return false, nil //nolint:nilerr // mismatch is a normal false result, not an error to bubble
	}
	return true, nil
}

// SignSession produces a stateless, HMAC-SHA256-signed session token over
// "username|expiry" using the server secret (no DB session table — D-03/Open Q1). The
// token is "<base64(username)>.<expiryUnix>.<base64(hmac)>". VerifySession is its inverse.
func (s *Service) SignSession(username string) string {
	expiry := time.Now().Add(sessionTTL).Unix()
	return s.signWithExpiry(username, expiry)
}

// signWithExpiry is the deterministic core of SignSession, factored out so tests can pin
// an expiry (e.g. an already-elapsed one to assert expiry rejection).
func (s *Service) signWithExpiry(username string, expiryUnix int64) string {
	userB64 := base64.RawURLEncoding.EncodeToString([]byte(username))
	payload := userB64 + "." + strconv.FormatInt(expiryUnix, 10)
	mac := s.mac(payload)
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac)
}

// VerifySession validates a token's HMAC and expiry, returning the embedded username on
// success. A malformed, tampered, or expired token yields ErrInvalidSession — the gate
// translates any error into a 401 (fail-closed, D-04).
func (s *Service) VerifySession(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", ErrInvalidSession
	}
	payload := parts[0] + "." + parts[1]
	wantMAC := s.mac(payload)
	gotMAC, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", ErrInvalidSession
	}
	// Constant-time compare so a tampered signature cannot be probed byte-by-byte.
	if !hmac.Equal(gotMAC, wantMAC) {
		return "", ErrInvalidSession
	}
	expiryUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", ErrInvalidSession
	}
	if time.Now().Unix() >= expiryUnix {
		return "", ErrInvalidSession
	}
	username, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", ErrInvalidSession
	}
	return string(username), nil
}

// mac computes HMAC-SHA256 of payload under the server secret.
func (s *Service) mac(payload string) []byte {
	h := hmac.New(sha256.New, s.secret)
	h.Write([]byte(payload))
	return h.Sum(nil)
}

// normalizeMode coerces an unknown/blank mode to Off (the safe default) and accepts the
// three known modes verbatim.
func normalizeMode(m string) string {
	switch strings.TrimSpace(strings.ToLower(m)) {
	case ModeOn:
		return ModeOn
	case ModeBypassLocal:
		return ModeBypassLocal
	default:
		return ModeOff
	}
}
