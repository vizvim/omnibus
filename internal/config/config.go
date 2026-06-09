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

// Config is the typed runtime configuration. koanf tags map snake_case keys (from
// file or the OMNIBUS_-prefixed, lowercased env vars) onto these fields.
type Config struct {
	HTTPAddr        string `koanf:"http_addr"`
	DBPath          string `koanf:"db_path"`
	DataPath        string `koanf:"data_path"`
	LogLevel        string `koanf:"log_level"`
	LogFormat       string `koanf:"log_format"`
	ComicVineAPIKey string `koanf:"comicvine_api_key"`
	ComicVineRate   string `koanf:"comicvine_rate"`
}

// Load reads configuration. If filePath is non-empty it is parsed as YAML first;
// then OMNIBUS_-prefixed env vars overlay it; the result unmarshals into Config.
// Defaults are applied before any source so unset keys have sane values.
func Load(filePath string) (Config, error) {
	k := koanf.New(".")

	defaults := map[string]any{
		"http_addr":  ":8080",
		"db_path":    "/data/omnibus.db",
		"data_path":  "/data",
		"log_level":  "info",
		"log_format": "json",
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

// LogValue implements slog.LogValuer so the ComicVine API key is never written to
// logs. Only non-secret fields are emitted; the key is masked.
func (c Config) LogValue() slog.Value {
	key := ""
	if c.ComicVineAPIKey != "" {
		key = "REDACTED"
	}
	return slog.GroupValue(
		slog.String("http_addr", c.HTTPAddr),
		slog.String("db_path", c.DBPath),
		slog.String("data_path", c.DataPath),
		slog.String("log_level", c.LogLevel),
		slog.String("log_format", c.LogFormat),
		slog.String("comicvine_api_key", key),
	)
}
