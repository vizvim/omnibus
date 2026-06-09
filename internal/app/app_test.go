package app_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/app"
	"github.com/vizvim/omnibus/internal/config"
)

func TestGracefulShutdown(t *testing.T) {
	cfg := config.Config{
		HTTPAddr:  "127.0.0.1:0", // ephemeral port — avoid clashing with a real server
		DBPath:    filepath.Join(t.TempDir(), "app.db"),
		LogLevel:  "error",
		LogFormat: "text",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- app.Run(ctx, cfg, logger) }()

	// Give Run a moment to migrate + open + start serving, then signal shutdown.
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err, "Run drains and returns cleanly on context cancellation")
	case <-time.After(20 * time.Second):
		t.Fatal("Run did not return within the shutdown deadline")
	}
}
