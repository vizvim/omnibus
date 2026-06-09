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

// NewLogger returns a *slog.Logger writing to w. The handler is JSON unless
// cfg.LogFormat is "text"; the level is parsed from cfg.LogLevel (default info).
func NewLogger(cfg config.Config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(cfg.LogLevel)}

	var handler slog.Handler
	if strings.EqualFold(cfg.LogFormat, "text") {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
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
