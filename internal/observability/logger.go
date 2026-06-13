// Package observability builds the injected *slog.Logger for omnibus. The logger is
// constructed once at startup and passed explicitly to collaborators — never via
// slog.Default() globals. JSON in prod, text in dev.
package observability

import (
	"io"
	"log/slog"
	"strings"

	"github.com/vizvim/omnibus/internal/config"
)

// NewLogger returns a *slog.Logger writing to w. The handler is selected by
// cfg.LogFormat: "console" for a colorized human-friendly handler (local dev), "text"
// for slog's text handler, anything else (default) for JSON. The level is parsed from
// cfg.LogLevel (default info).
func NewLogger(cfg config.Config, w io.Writer) *slog.Logger {
	level := parseLevel(cfg.LogLevel)

	var handler slog.Handler
	switch strings.ToLower(cfg.LogFormat) {
	case "console":
		handler = NewConsoleHandler(w, &ConsoleOptions{Level: level})
	case "text":
		handler = slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	default:
		handler = slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	}
	return slog.New(handler)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
