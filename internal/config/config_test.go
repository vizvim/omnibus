package config_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/config"
	"github.com/vizvim/omnibus/internal/observability"
)

func TestDefaultsApplied(t *testing.T) {
	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Equal(t, ":8080", cfg.HTTPAddr)
	require.Equal(t, "/data/omnibus.db", cfg.DBPath)
	require.Equal(t, "/data", cfg.DataPath)
	// Auto-search / RSS cadence defaults (fixed code defaults, D-11).
	require.Equal(t, 6, cfg.AutoSearchIntervalHours)
	require.Equal(t, 20, cfg.AutoSearchBatchSize)
	require.Equal(t, 10, cfg.SearchAttemptCap)
	require.Equal(t, 30, cfg.RSSPollIntervalMinutes)
}

func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("http_addr: \":9000\"\nlog_level: info\n"), 0o600))

	t.Setenv("OMNIBUS_HTTP_ADDR", ":7777")

	cfg, err := config.Load(cfgPath)
	require.NoError(t, err)
	require.Equal(t, ":7777", cfg.HTTPAddr, "OMNIBUS_ env var overrides the file value")
	require.Equal(t, "info", cfg.LogLevel, "file value used when no env override")
}

func TestComicVineKeyFromEnv(t *testing.T) {
	t.Setenv("OMNIBUS_COMICVINE_API_KEY", "super-secret-key")
	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Equal(t, "super-secret-key", cfg.ComicVineAPIKey)
}

func TestSecretNeverLogged(t *testing.T) {
	const secret = "TOPSECRET-cv-key-12345"
	t.Setenv("OMNIBUS_COMICVINE_API_KEY", secret)
	cfg, err := config.Load("")
	require.NoError(t, err)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	// Logging the config (via its LogValue redaction) must never emit the secret.
	logger.Info("loaded config", slog.Any("config", cfg))

	require.NotContains(t, buf.String(), secret, "ComicVine API key must never appear in logs")
	require.Contains(t, buf.String(), "REDACTED", "secret should be masked")
}

func TestSabnzbdKeyNeverLogged(t *testing.T) {
	const sabSecret = "TOPSECRET-sab-key-67890"
	t.Setenv("OMNIBUS_SABNZBD_API_KEY", sabSecret)
	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Equal(t, sabSecret, cfg.SabnzbdAPIKey)
	require.Equal(t, "comics", cfg.SabnzbdCategory, "default category")

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Info("loaded config", slog.Any("config", cfg))

	require.NotContains(t, buf.String(), sabSecret, "SABnzbd API key must never appear in logs")
	require.Contains(t, buf.String(), "REDACTED", "secret should be masked")
}

func TestAuthSessionSecretNeverLogged(t *testing.T) {
	const sessionSecret = "TOPSECRET-session-signing-abcdef"
	t.Setenv("OMNIBUS_AUTH_SESSION_SECRET", sessionSecret)
	cfg, err := config.Load("")
	require.NoError(t, err)
	require.Equal(t, sessionSecret, cfg.AuthSessionSecret)

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	logger.Info("loaded config", slog.Any("config", cfg))

	require.NotContains(t, buf.String(), sessionSecret, "auth session secret must never appear in logs")
	require.Contains(t, buf.String(), "REDACTED", "secret should be masked")
}

func TestNewLoggerProducesValidLogger(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		cfg := config.Config{LogLevel: "debug", LogFormat: format}
		logger := observability.NewLogger(cfg, os.Stderr)
		require.NotNil(t, logger)
	}
}
