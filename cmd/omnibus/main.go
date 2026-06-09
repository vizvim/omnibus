// Command omnibus is the single entrypoint for the omnibus server. It wires config,
// the SQLite pools (after running migrations), the SeriesService Connect handler over
// h2c, structured slog logging, and a signal-driven graceful shutdown that drains the
// HTTP server then closes the DB pools.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/vizvim/omnibus/internal/app"
	"github.com/vizvim/omnibus/internal/config"
	"github.com/vizvim/omnibus/internal/observability"
)

func main() {
	os.Exit(run())
}

// run owns the process body and returns an exit code. main only calls os.Exit so
// that every defer here runs before the process exits (gocritic exitAfterDefer).
func run() int {
	cfg, err := config.Load(os.Getenv("OMNIBUS_CONFIG_FILE"))
	if err != nil {
		// No logger yet; write to stderr via a minimal bootstrap logger.
		observability.NewLogger(config.Config{}, os.Stderr).Error("load config", "error", err)
		return 1
	}

	logger := observability.NewLogger(cfg, os.Stderr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg, logger); err != nil {
		logger.Error("server exited with error", "error", err)
		return 1
	}
	return 0
}
