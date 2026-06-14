// Package config loads omnibus configuration from an optional file overlaid by
// OMNIBUS_-prefixed environment variables (env wins), unmarshaled into a typed
// Config. Secrets (the ComicVine API key) are loaded env-first and redacted from
// log output.
package config

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// envPrefix is the prefix for environment overrides (OMNIBUS_HTTP_ADDR, ...).
const envPrefix = "OMNIBUS_"

// redacted is the masked placeholder emitted in LogValue for any present secret.
const redacted = "REDACTED"

// Config is the typed runtime configuration. koanf tags map snake_case keys (from
// file or the OMNIBUS_-prefixed, lowercased env vars) onto these fields.
type Config struct {
	HTTPAddr string `koanf:"http_addr"`
	DBPath   string `koanf:"db_path"`
	DataPath string `koanf:"data_path"`
	// LibraryPath is the root of the organized comic library — the destination
	// post-processing renames/moves completed downloads into (D-13). Distinct from
	// DataPath (working/runtime state): the library is the user's curated collection.
	// Default "/comics", env-overridable via OMNIBUS_LIBRARY_PATH. Non-secret.
	LibraryPath     string `koanf:"library_path"`
	LogLevel        string `koanf:"log_level"`
	LogFormat       string `koanf:"log_format"`
	ComicVineAPIKey string `koanf:"comicvine_api_key"`
	ComicVineRate   string `koanf:"comicvine_rate"`
	// RiverWorkers is the number of concurrent River worker goroutines. A single-user
	// self-hosted app needs little concurrency; default 2. Override with
	// OMNIBUS_RIVER_WORKERS.
	RiverWorkers int `koanf:"river_workers"`
	// RefreshIntervalHours is how often the scheduled stale-only refresh sweep runs.
	// Fixed sensible default (6h) this phase, env-overridable
	// (OMNIBUS_REFRESH_INTERVAL_HOURS), not a user-facing UI knob.
	RefreshIntervalHours int `koanf:"refresh_interval_hours"`
	// StalenessThresholdDays is how old a series' last_refreshed_at must be before the
	// sweep refreshes it. Fixed default (7 days), env-overridable
	// (OMNIBUS_STALENESS_THRESHOLD_DAYS), not a user-facing UI knob.
	StalenessThresholdDays int `koanf:"staleness_threshold_days"`
	// SabnzbdURL is the SABnzbd download-client base URL (OMNIBUS_SABNZBD_URL). SAB is
	// the download client, NOT an indexer (D-16) — it lives in config, not the DB. Empty
	// means no SAB configured (NZB grabs fail loudly).
	SabnzbdURL string `koanf:"sabnzbd_url"`
	// SabnzbdAPIKey is the SABnzbd API key (OMNIBUS_SABNZBD_API_KEY). Secret — redacted
	// in LogValue.
	SabnzbdAPIKey string `koanf:"sabnzbd_api_key"`
	// SabnzbdCategory is the SAB category NZBs are added under (default "comics").
	SabnzbdCategory string `koanf:"sabnzbd_category"`
	// AutoSearchIntervalHours is how often the scheduled auto-search sweep runs. Fixed
	// sensible default (6h) this phase, env-overridable
	// (OMNIBUS_AUTO_SEARCH_INTERVAL_HOURS), not a user-facing UI knob (D-11).
	AutoSearchIntervalHours int `koanf:"auto_search_interval_hours"`
	// AutoSearchBatchSize bounds how many Wanted issues a single auto-search sweep tick
	// enqueues (D-08). Fixed default (20), env-overridable.
	AutoSearchBatchSize int `koanf:"auto_search_batch_size"`
	// SearchAttemptCap is the auto-search attempt cap: an issue at/above it goes cold and
	// is skipped by auto-search (D-09). Fixed default (10), env-overridable.
	SearchAttemptCap int `koanf:"search_attempt_cap"`
	// RSSPollIntervalMinutes is how often each enabled RSS indexer's feed is polled for
	// auto-grab (D-15). Fixed default (30m), env-overridable.
	RSSPollIntervalMinutes int `koanf:"rss_poll_interval_minutes"`
	// AuthSessionSecret signs the optional-auth session cookie (HMAC-SHA256, AUTH-01).
	// Secret — redacted in LogValue, never logged. Sourced from OMNIBUS_AUTH_SESSION_SECRET;
	// when empty the app generates a persisted secret on first boot (see app wiring) so the
	// gate still works out of the box without operator setup. Changing it invalidates all
	// outstanding sessions.
	AuthSessionSecret string `koanf:"auth_session_secret"`
	// AuthTrustProxy, when true, lets the gate read the first X-Forwarded-For entry for the
	// local-bypass peer-IP decision (only safe behind an explicitly trusted reverse proxy,
	// D-05). Default false: the gate uses the real socket peer (non-spoofable).
	AuthTrustProxy bool `koanf:"auth_trust_proxy"`
}

// Load reads configuration. If filePath is non-empty it is parsed as YAML first;
// then OMNIBUS_-prefixed env vars overlay it; the result unmarshals into Config.
// Defaults are applied before any source so unset keys have sane values.
func Load(filePath string) (Config, error) {
	k := koanf.New(".")

	defaults := map[string]any{
		"http_addr":                  ":8080",
		"db_path":                    "/data/omnibus.db",
		"data_path":                  "/data",
		"library_path":               "/comics",
		"log_level":                  "info",
		"log_format":                 "json",
		"river_workers":              2,
		"refresh_interval_hours":     6,
		"staleness_threshold_days":   7,
		"sabnzbd_url":                "",
		"sabnzbd_api_key":            "",
		"sabnzbd_category":           "comics",
		"auto_search_interval_hours": 6,
		"auto_search_batch_size":     20,
		"search_attempt_cap":         10,
		"rss_poll_interval_minutes":  30,
		"auth_session_secret":        "",
		"auth_trust_proxy":           false,
	}
	if err := k.Load(confmap.Provider(defaults, "."), nil); err != nil {
		return Config{}, fmt.Errorf("load defaults: %w", err)
	}

	if filePath != "" {
		if err := k.Load(file.Provider(filePath), yaml.Parser()); err != nil {
			return Config{}, fmt.Errorf("load config file %q: %w", filePath, err)
		}
	}

	// Env overlay: OMNIBUS_HTTP_ADDR -> http_addr, OMNIBUS_COMICVINE_API_KEY -> comicvine_api_key.
	envProvider := env.Provider(envPrefix, ".", func(s string) string {
		return strings.ToLower(strings.TrimPrefix(s, envPrefix))
	})
	if err := k.Load(envProvider, nil); err != nil {
		return Config{}, fmt.Errorf("load env overlay: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}
	return cfg, nil
}

// LogValue implements slog.LogValuer so secrets (the ComicVine API key, the SABnzbd API
// key, and the auth session-signing secret) are never written to logs. Only non-secret
// fields are emitted; each secret is masked to REDACTED when present.
func (c Config) LogValue() slog.Value {
	key := ""
	if c.ComicVineAPIKey != "" {
		key = redacted
	}
	sabKey := ""
	if c.SabnzbdAPIKey != "" {
		sabKey = redacted
	}
	sessionSecret := ""
	if c.AuthSessionSecret != "" {
		sessionSecret = redacted
	}
	return slog.GroupValue(
		slog.String("http_addr", c.HTTPAddr),
		slog.String("db_path", c.DBPath),
		slog.String("data_path", c.DataPath),
		slog.String("library_path", c.LibraryPath),
		slog.String("log_level", c.LogLevel),
		slog.String("log_format", c.LogFormat),
		slog.String("comicvine_api_key", key),
		slog.String("sabnzbd_url", c.SabnzbdURL),
		slog.String("sabnzbd_api_key", sabKey),
		slog.String("sabnzbd_category", c.SabnzbdCategory),
		slog.String("auth_session_secret", sessionSecret),
		slog.Bool("auth_trust_proxy", c.AuthTrustProxy),
	)
}
