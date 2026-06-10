// Package app wires omnibus's runtime components and owns the server lifecycle:
// run migrations, open the DB pools, build the gateway + repositories + series service,
// start the River job engine, serve the SeriesService + cover handler over h2c, and on
// context cancellation drain the HTTP server, the job engine (Stop), then the DB pools
// in order. It is separated from main so the lifecycle is testable.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/riverqueue/river"
	"github.com/rs/cors"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/config"
	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/jobs"
	"github.com/vizvim/omnibus/internal/provider/download"
	indexerprovider "github.com/vizvim/omnibus/internal/provider/indexer"
	"github.com/vizvim/omnibus/internal/provider/metadata"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/service/indexer"
	jobsservice "github.com/vizvim/omnibus/internal/service/jobs"
	"github.com/vizvim/omnibus/internal/service/search"
	"github.com/vizvim/omnibus/internal/service/series"
	"github.com/vizvim/omnibus/internal/transport"
)

const (
	// shutdownTimeout bounds the graceful-shutdown drain.
	shutdownTimeout = 15 * time.Second
	// defaultCVRate is the fallback ComicVine pace (~1 req / 2s, Mylar3's floor).
	defaultCVRate = 2 * time.Second
	// attemptCap bounds Failed->Wanted re-search loops.
	attemptCap = 5
	// metadataTTL is the metadata_cache freshness window.
	metadataTTL = 24 * time.Hour
)

// Run starts the server and blocks until ctx is canceled (e.g. SIGINT/SIGTERM) or a
// fatal error occurs. On shutdown it drains the HTTP server, the River job engine,
// then closes the DB pools.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	// Migrations run before the server accepts traffic.
	if err := db.Migrate(ctx, cfg.DBPath); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	database, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	repos := repository.NewRepositories(database)

	// The gateway is the single ComicVine chokepoint (limiter + cache).
	provider := metadata.NewComicVineProvider(cfg.ComicVineAPIKey)
	limiter := rate.NewLimiter(rate.Every(parseRate(cfg.ComicVineRate)), 1)
	gateway := metadata.NewGateway(provider, repos.MetadataCache, limiter, logger, metadataTTL)

	// Build the series service first (with a nil enqueuer), then the jobs client that
	// references it as the import runner, then inject the client back as the service's
	// enqueuer — this resolves the service<->jobs construction cycle (D-11).
	svc := series.New(series.Deps{
		Gateway:            gateway,
		Repos:              repos,
		AttemptCap:         attemptCap,
		Logger:             logger,
		StalenessThreshold: time.Duration(cfg.StalenessThresholdDays) * 24 * time.Hour,
	})

	// DownloadProviders: SABnzbd comes from config (D-16: SAB is the download client,
	// not an indexer). An empty SabnzbdURL yields a provider whose Submit fails loudly
	// (ErrSabnzbdNotConfigured). No polling loop is started here — tracking is Phase 5.
	downloadProviders := newDownloadProviders(cfg)

	// SearchService converges the indexer gateway (Plan 02), the filter/score pipeline
	// (Plan 03), and the grab handoff (Plan 04) behind manual-search RPCs, and owns the
	// auto-search/RSS job bodies (Plan 06). Built before the jobs client so it can be the
	// AutoSearchRunner/RSSPollRunner; the client is injected back as its enqueuer below.
	indexerGateway := indexerprovider.NewGateway(logger, 0)
	searchSvc := search.New(search.Deps{
		Gateway:           indexerGateway,
		Repos:             repos,
		DownloadProviders: downloadProviders,
		Logger:            logger,
		AttemptCap:        cfg.SearchAttemptCap,
		AutoSearchBatch:   cfg.AutoSearchBatchSize,
	})

	workers := river.NewWorkers()
	river.AddWorker(workers, jobs.NewImportWorker(svc))
	river.AddWorker(workers, jobs.NewRefreshWorker(svc))
	river.AddWorker(workers, jobs.NewSweepWorker(svc))
	river.AddWorker(workers, jobs.NewAutoSearchSweepWorker(searchSvc))
	river.AddWorker(workers, jobs.NewSearchIssueWorker(searchSvc))
	river.AddWorker(workers, jobs.NewRSSPollWorker(searchSvc))

	sweepInterval := time.Duration(cfg.RefreshIntervalHours) * time.Hour
	autoSearchInterval := time.Duration(cfg.AutoSearchIntervalHours) * time.Hour
	rssPollInterval := time.Duration(cfg.RSSPollIntervalMinutes) * time.Minute
	riverClient, err := jobs.New(ctx, database.Write, database.Read, cfg.RiverWorkers, sweepInterval, autoSearchInterval, rssPollInterval, logger, workers)
	if err != nil {
		_ = database.Close()
		return fmt.Errorf("build jobs client: %w", err)
	}
	// Resolve the service<->jobs cycle: both services receive the client as their enqueuer.
	svc.SetEnqueuer(riverClient)
	searchSvc.SetEnqueuer(riverClient)

	// JobService reads run history from River's tables (via the jobs client).
	jobSvc := jobsservice.New(riverClient)

	// IndexerService owns DB-backed indexer CRUD (ADR 0007 domain segmentation).
	indexerSvc := indexer.New(indexer.Deps{Repos: repos, Logger: logger})

	srv, err := newServer(cfg, logger, svc, jobSvc, indexerSvc, searchSvc, repos.Cover)
	if err != nil {
		_ = database.Close()
		return err
	}

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		logger.Info("omnibus listening", slog.String("addr", cfg.HTTPAddr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		if err := riverClient.Start(ctx); err != nil {
			return fmt.Errorf("start jobs engine: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		<-ctx.Done()
		logger.Info("shutdown signal received, draining")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		// Order: stop accepting requests, drain the job engine, close pools (D-12).
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("http shutdown", slog.Any("error", err))
		}
		if err := riverClient.Stop(shutdownCtx); err != nil {
			logger.Error("jobs engine drain", slog.Any("error", err))
		}
		if err := database.Close(); err != nil {
			logger.Error("close database", slog.Any("error", err))
		}
		logger.Info("shutdown complete")
		return nil
	})

	if err := g.Wait(); err != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if stopErr := riverClient.Stop(stopCtx); stopErr != nil {
			logger.Error("jobs engine drain", slog.Any("error", stopErr))
		}
		_ = database.Close()
		return err
	}
	return nil
}

// newServer builds the h2c-wrapped HTTP server hosting the SeriesService + JobService
// Connect handlers (with slog + otel interceptors) and the cover handler serving blobs
// from SQLite, with CORS scoped to the Vite dev origin.
func newServer(cfg config.Config, logger *slog.Logger, svc *series.Service, jobSvc *jobsservice.Service, indexerSvc *indexer.Service, searchSvc *search.Service, covers transport.CoverStore) (*http.Server, error) {
	interceptors, err := transport.NewInterceptors(logger)
	if err != nil {
		return nil, fmt.Errorf("build interceptors: %w", err)
	}

	mux := http.NewServeMux()
	// The frontend Connect transport uses baseUrl "/api" (both under the Vite dev
	// proxy and the production SPA), so each RPC handler is served under that prefix.
	// StripPrefix restores the bare procedure path the Connect handler routes on.
	seriesHandler := transport.NewSeriesHandler(svc)
	seriesPath, seriesH := omnibusv1connect.NewSeriesServiceHandler(seriesHandler, connect.WithInterceptors(interceptors...))
	mux.Handle("/api"+seriesPath, http.StripPrefix("/api", seriesH))

	jobHandler := transport.NewJobHandler(jobSvc)
	jobPath, jobH := omnibusv1connect.NewJobServiceHandler(jobHandler, connect.WithInterceptors(interceptors...))
	mux.Handle("/api"+jobPath, http.StripPrefix("/api", jobH))

	indexerHandler := transport.NewIndexerHandler(indexerSvc)
	indexerPath, indexerH := omnibusv1connect.NewIndexerServiceHandler(indexerHandler, connect.WithInterceptors(interceptors...))
	mux.Handle("/api"+indexerPath, http.StripPrefix("/api", indexerH))

	searchHandler := transport.NewSearchHandler(searchSvc)
	searchPath, searchH := omnibusv1connect.NewSearchServiceHandler(searchHandler, connect.WithInterceptors(interceptors...))
	mux.Handle("/api"+searchPath, http.StripPrefix("/api", searchH))

	mux.Handle("/covers/", transport.NewCoverHandler(covers))

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
	}, nil
}

// newDownloadProviders constructs the DownloadProviders keyed by Kind, ready to inject
// into the search service (Plan 05). SABnzbd is built from config (D-16: SAB is the
// download client, not an indexer); an empty SabnzbdURL yields a provider whose Submit
// returns ErrSabnzbdNotConfigured so a NZB grab without SAB fails loudly. GetComics DDL
// needs no secret. No polling is started here (Phase 5).
func newDownloadProviders(cfg config.Config) map[string]download.DownloadProvider {
	return map[string]download.DownloadProvider{
		"sabnzbd":   download.NewSABnzbdProvider(cfg.SabnzbdURL, cfg.SabnzbdAPIKey, cfg.SabnzbdCategory),
		"getcomics": download.NewGetComicsDDLProvider("https://getcomics.org"),
	}
}

// parseRate maps the OMNIBUS_COMICVINE_RATE config (a Go duration like "2s") to the
// minimum interval between ComicVine calls, falling back to the conservative default.
func parseRate(raw string) time.Duration {
	if raw == "" {
		return defaultCVRate
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultCVRate
	}
	return d
}
