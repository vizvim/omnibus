package observability_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/observability"
)

// newTestLogger builds a logger over a ConsoleHandler with color forced off so output is
// deterministic and assertable.
func newTestLogger(buf *bytes.Buffer, level slog.Level) *slog.Logger {
	noColor := true
	h := observability.NewConsoleHandler(buf, &observability.ConsoleOptions{
		Level:   level,
		NoColor: &noColor,
		// Zero out the time by using a format the test records (with zero time) skip.
	})
	return slog.New(h)
}

func TestConsoleHandlerBasicLine(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf, slog.LevelInfo)

	logger.Info("hello world", slog.String("user", "kyle"), slog.Int("count", 3))

	out := buf.String()
	require.Contains(t, out, "INF")
	require.Contains(t, out, "hello world")
	require.Contains(t, out, "user=kyle")
	require.Contains(t, out, "count=3")
	require.True(t, strings.HasSuffix(out, "\n"))
}

func TestConsoleHandlerLevels(t *testing.T) {
	cases := []struct {
		level slog.Level
		code  string
	}{
		{slog.LevelDebug, "DBG"},
		{slog.LevelInfo, "INF"},
		{slog.LevelWarn, "WRN"},
		{slog.LevelError, "ERR"},
	}
	for _, tc := range cases {
		var buf bytes.Buffer
		logger := newTestLogger(&buf, slog.LevelDebug)
		logger.Log(context.Background(), tc.level, "msg")
		require.Contains(t, buf.String(), tc.code)
	}
}

func TestConsoleHandlerRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf, slog.LevelWarn)

	logger.Info("suppressed")
	require.Empty(t, buf.String())

	logger.Warn("shown")
	require.Contains(t, buf.String(), "shown")
}

func TestConsoleHandlerQuotesValuesWithSpaces(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf, slog.LevelInfo)

	logger.Info("msg", slog.String("title", "Saga Vol 1"), slog.String("empty", ""))

	out := buf.String()
	require.Contains(t, out, `title="Saga Vol 1"`)
	require.Contains(t, out, `empty=""`)
}

func TestConsoleHandlerWithAttrsAndGroup(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf, slog.LevelInfo).
		With(slog.String("service", "search")).
		WithGroup("req")

	logger.Info("handled", slog.String("id", "42"))

	out := buf.String()
	require.Contains(t, out, "service=search")
	require.Contains(t, out, "req.id=42")
}

func TestConsoleHandlerInlineGroup(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf, slog.LevelInfo)

	logger.Info("msg", slog.Group("http", slog.Int("status", 200), slog.String("method", "GET")))

	out := buf.String()
	require.Contains(t, out, "http.status=200")
	require.Contains(t, out, "http.method=GET")
}

func TestConsoleHandlerErrorValue(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf, slog.LevelInfo)

	logger.Error("boom", slog.Any("error", errors.New("disk full")))

	require.Contains(t, buf.String(), `error="disk full"`)
}

func TestConsoleHandlerColorOutput(t *testing.T) {
	var buf bytes.Buffer
	noColor := false
	h := observability.NewConsoleHandler(&buf, &observability.ConsoleOptions{NoColor: &noColor})
	slog.New(h).Info("colored")

	require.Contains(t, buf.String(), "\033[", "color output should contain ANSI escape codes")
}

func TestConsoleHandlerResolvesLogValuer(t *testing.T) {
	var buf bytes.Buffer
	logger := newTestLogger(&buf, slog.LevelInfo)

	logger.Info("cfg", slog.Any("config", redactable{}))

	out := buf.String()
	require.Contains(t, out, "secret=REDACTED")
	require.NotContains(t, out, "supersecret")
}

// redactable mimics a config type that masks a secret via slog.LogValuer.
type redactable struct{}

func (redactable) LogValue() slog.Value {
	return slog.GroupValue(slog.String("secret", "REDACTED"))
}
