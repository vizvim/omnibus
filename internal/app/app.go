// Package app wires omnibus's runtime components and owns the server lifecycle:
// run migrations, open the DB pools, build repositories + the Connect handler, serve
// over h2c, and on context cancellation drain the HTTP server then close the DB pools
// in order (PLAT-08). It is separated from main so the lifecycle is testable.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/rs/cors"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"golang.org/x/sync/errgroup"

	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/config"
	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/transport"
)

// shutdownTimeout bounds the graceful-shutdown drain so the process exits promptly
// under orchestrator stop signals (deployment.md).
const shutdownTimeout = 15 * time.Second

// Run starts the server and blocks until ctx is canceled (e.g. SIGINT/SIGTERM) or a
// fatal error occurs. On shutdown it drains the HTTP server then closes the DB pools.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	// Migrations run before the server accepts traffic (ADR 0003).
	if err := db.Migrate(ctx, cfg.DBPath); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	database, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	// Repositories are constructed here and handed to the service layer (02-04).
	// Building them now proves the wiring and surfaces pool issues at startup.
	repos := buildRepositories(database)
	_ = repos

	srv := newServer(cfg, logger)

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		logger.Info("omnibus listening", slog.String("addr", cfg.HTTPAddr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	})

	// Shutdown goroutine: when the lifecycle context is canceled, drain the server
	// then close the DB pools (read first, write last) within the bounded deadline.
	g.Go(func() error {
		<-ctx.Done()
		logger.Info("shutdown signal received, draining")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("http shutdown", slog.Any("error", err))
		}
		if err := database.Close(); err != nil {
			logger.Error("close database", slog.Any("error", err))
		}
		logger.Info("shutdown complete")
		return nil
	})

	if err := g.Wait(); err != nil {
		_ = database.Close()
		return err
	}
	return nil
}

// repositories bundles the Phase-2 repositories the service layer will consume (02-04).
type repositories struct {
	Series        repository.SeriesRepository
	Issue         repository.IssueRepository
	Publisher     repository.PublisherRepository
	Arc           repository.ArcRepository
	MetadataCache repository.MetadataCacheRepository
	UserConfig    repository.UserConfigRepository
}

func buildRepositories(d *db.DB) repositories {
	return repositories{
		Series:        repository.NewSeriesRepository(d),
		Issue:         repository.NewIssueRepository(d),
		Publisher:     repository.NewPublisherRepository(d),
		Arc:           repository.NewArcRepository(d),
		MetadataCache: repository.NewMetadataCacheRepository(d),
		UserConfig:    repository.NewUserConfigRepository(d),
	}
}

// newServer builds the h2c-wrapped HTTP server hosting the SeriesService Connect
// handler with CORS scoped to the Vite dev origin.
func newServer(cfg config.Config, _ *slog.Logger) *http.Server {
	mux := http.NewServeMux()

	seriesHandler := transport.NewSeriesHandler()
	path, handler := omnibusv1connect.NewSeriesServiceHandler(seriesHandler)
	mux.Handle(path, handler)

	corsHandler := cors.New(cors.Options{
		AllowedOrigins: []string{"http://localhost:5173"},
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowedHeaders: []string{
			"Content-Type",
			"Connect-Protocol-Version",
			"Connect-Timeout-Ms",
			"Grpc-Timeout",
			"X-Grpc-Web",
			"X-User-Agent",
		},
		ExposedHeaders: []string{"Grpc-Status", "Grpc-Message", "Grpc-Status-Details-Bin"},
	}).Handler(mux)

	return &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           h2c.NewHandler(corsHandler, &http2.Server{}),
		ReadHeaderTimeout: 10 * time.Second,
	}
}
