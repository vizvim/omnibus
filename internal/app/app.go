// Package app wires omnibus's runtime components and owns the server lifecycle:
// run migrations, open the DB pools, build the gateway + repositories + series service,
// start the River job engine, serve the SeriesService + cover handler over h2c, and on
// context cancellation drain the HTTP server, the job engine (Stop), then the DB pools
// in order. It is separated from main so the lifecycle is testable.
package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
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
	"github.com/vizvim/omnibus/internal/auth"
	"github.com/vizvim/omnibus/internal/config"
	"github.com/vizvim/omnibus/internal/db"
	"github.com/vizvim/omnibus/internal/ddlconfig"
	"github.com/vizvim/omnibus/internal/download"
	"github.com/vizvim/omnibus/internal/downloadclient"
	"github.com/vizvim/omnibus/internal/events"
	indexerprovider "github.com/vizvim/omnibus/internal/indexer"
	"github.com/vizvim/omnibus/internal/indexerconfig"
	"github.com/vizvim/omnibus/internal/jobhistory"
	"github.com/vizvim/omnibus/internal/jobs"
	"github.com/vizvim/omnibus/internal/metadata"
	"github.com/vizvim/omnibus/internal/metadataprovider"
	"github.com/vizvim/omnibus/internal/postprocess"
	"github.com/vizvim/omnibus/internal/renameconfig"
	"github.com/vizvim/omnibus/internal/repository"
	"github.com/vizvim/omnibus/internal/search"
	"github.com/vizvim/omnibus/internal/series"
	"github.com/vizvim/omnibus/internal/tracking"
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
	// downloadPollInterval is the cadence of the periodic download-status poll (DL-03/04/08).
	// Hardcoded constant rather than a config knob (D-13 lean-config posture, Claude's
	// discretion). Short enough to feel live, light enough for a single-user box.
	downloadPollInterval = 20 * time.Second
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

	// The in-process event bus is the live-status fan-out (UI-05, D-08): the tracking poll loop
	// and the jobs client Publish typed envelopes to it, and the EventService stream handler
	// Subscribes and Sends them to subscribed SPAs. It is purely additive observability — a
	// slow/dead subscriber never blocks producers (drop-on-full, T-6-21).
	eventBus := events.NewBus()

	// ComicVine config is now DB-backed and runtime-editable via MetadataProviderService.
	// On first boot, seed the DB row from OMNIBUS_COMICVINE_API_KEY so existing deployments
	// keep working; thereafter the DB is the source of truth.
	if seedErr := seedComicVineConfig(ctx, repos, cfg, logger); seedErr != nil {
		_ = database.Close()
		return fmt.Errorf("seed comicvine config: %w", seedErr)
	}

	// The gateway is the single ComicVine chokepoint (limiter + cache). The provider resolves
	// its API key straight from the DB on each request (no in-memory cache), so a key saved in
	// the UI takes effect on the next search with no restart and nothing to drift.
	provider := metadata.NewComicVineProviderWithKeyFunc(comicVineKeyResolver(repos, logger))
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

	// DownloadProviders: SABnzbd config is now DB-backed and runtime-editable via
	// DownloadClientService (supersedes D-16). On first boot, seed the DB row from
	// OMNIBUS_SABNZBD_* so existing deployments keep working; thereafter the DB is the
	// source of truth and the provider resolves config from it on each Submit. An empty
	// resolved URL yields a Submit that fails loudly (ErrSabnzbdNotConfigured). No polling
	// loop is started here — tracking is Phase 5.
	if seedErr := seedSabnzbdConfig(ctx, repos, cfg, logger); seedErr != nil {
		_ = database.Close()
		return fmt.Errorf("seed download client config: %w", seedErr)
	}
	// Build the SABnzbd provider once from a DB-backed resolver so the same instance fronts
	// both NZB submits (search service) and the connectivity probe (download client Test).
	sabProvider := download.NewSABnzbdProviderWithResolver(sabnzbdResolver(repos))
	// The GetComics DDL provider streams into <data_path>/incomplete on Fetch (D-03). Built
	// once so the same instance fronts the search submit map and the tracking DDLFetchers map.
	ddlProvider := download.NewGetComicsDDLProvider("https://getcomics.org",
		download.WithDDLDataPath(cfg.DataPath))
	// Each consumer takes only the capability it needs from these concrete providers: the
	// search/grab path needs Submit (search.Submitter, both providers); the poll loop needs Poll
	// (tracking.Poller, SAB only); post-process needs RemoveFromHistory (postprocess.HistoryRemover,
	// SAB only — DDL has no client-side history); the DDLFetch job needs Fetch (tracking.DDLFetcher,
	// GetComics only).
	// Provider-kind keys for the per-consumer capability maps below.
	const (
		kindSABnzbd   = "sabnzbd"
		kindGetComics = "getcomics"
	)
	downloadSubmitters := map[string]search.Submitter{
		kindSABnzbd:   sabProvider,
		kindGetComics: ddlProvider,
	}

	// SearchService converges the indexer gateway (Plan 02), the filter/score pipeline
	// (Plan 03), and the grab handoff (Plan 04) behind manual-search RPCs, and owns the
	// auto-search/RSS job bodies (Plan 06). Built before the jobs client so it can be the
	// AutoSearchRunner/RSSPollRunner; the client is injected back as its enqueuer below.
	indexerGateway := indexerprovider.NewGateway(logger, 0)
	searchSvc := search.New(search.Deps{
		Gateway:           indexerGateway,
		Repos:             repos,
		DownloadProviders: downloadSubmitters,
		Logger:            logger,
		AttemptCap:        cfg.SearchAttemptCap,
		AutoSearchBatch:   cfg.AutoSearchBatchSize,
	})

	// TrackingService owns the download-status poll loop (DL-03/04/08): it polls active
	// downloads via the SAB Poller, surfaces progress/terminal state on the timeline,
	// records history, and detects failures (explicit + stall). It is the PollRunner the
	// periodic DownloadPoll job delegates to. Only SAB has a Poller this phase.
	trackingSvc := tracking.New(tracking.Deps{
		Repos:   repos,
		Pollers: map[string]tracking.Poller{kindSABnzbd: sabProvider},
		// GetComics DDL has no poll-based Poller; instead the DDLFetch job (RunDDLFetch)
		// streams via this fetcher then feeds the Completed->post-process path (D-03).
		DDLFetchers: map[string]tracking.DDLFetcher{kindGetComics: ddlProvider},
		Logger:      logger,
		// Live-status fan-out: download progress / issue status / timeline events from the poll
		// loop are published to the bus the EventService stream subscribes to (UI-05).
		Publisher: eventBus,
	})

	// RenameConfigService owns the DB-backed, runtime-editable renaming config (D-09), the
	// config source the post-process template engine renders against. Built before the
	// post-process service (its config getter) and the jobs client.
	renameConfigSvc := renameconfig.New(renameconfig.Deps{Repos: repos, Logger: logger})

	// DDLConfigService owns the DB-backed, runtime-toggleable DDL enable flag (05-07),
	// the flag the search/grab path reads to gate the built-in GetComics fallback.
	ddlConfigSvc := ddlconfig.New(ddlconfig.Deps{Repos: repos, Logger: logger})

	// AuthService owns the optional single-user auth gate (AUTH-01, ADR 0008). The
	// session-signing secret comes from config; when unset we generate one and persist it
	// in user_config so the gate works out of the box and survives restarts (D-03). The
	// same authSvc instance backs both the Login handler (which writes cookies) and the
	// gate middleware (which verifies them), so a freshly-issued session validates.
	sessionSecret, err := resolveSessionSecret(ctx, repos, cfg, logger)
	if err != nil {
		_ = database.Close()
		return fmt.Errorf("resolve auth session secret: %w", err)
	}
	authSvc := auth.New(auth.Deps{Repos: repos, Logger: logger, SessionSecret: sessionSecret})

	// PostProcessService composes the 05-02 units against a real completed download (05-03):
	// validate -> render -> import -> Snatched->Downloaded -> events -> history -> remove, and
	// routes a corrupt archive into the shared D-16 replacement path. Built before the jobs
	// client so it can be the PostProcessRunner; the client is injected back as its
	// replacement enqueuer below.
	postProcessSvc := postprocess.New(postprocess.Deps{
		Repos:        repos,
		Removers:     map[string]postprocess.HistoryRemover{kindSABnzbd: sabProvider},
		RenameConfig: renameConfigSvc,
		LibraryPath:  cfg.LibraryPath,
		AttemptCap:   attemptCap,
		Logger:       logger,
	})

	workers := river.NewWorkers()
	river.AddWorker(workers, jobs.NewImportWorker(svc))
	river.AddWorker(workers, jobs.NewRefreshWorker(svc))
	river.AddWorker(workers, jobs.NewSweepWorker(svc))
	river.AddWorker(workers, jobs.NewAutoSearchSweepWorker(searchSvc))
	river.AddWorker(workers, jobs.NewSearchIssueWorker(searchSvc))
	river.AddWorker(workers, jobs.NewRSSPollWorker(searchSvc))
	river.AddWorker(workers, jobs.NewDownloadPollWorker(trackingSvc))
	river.AddWorker(workers, jobs.NewReplacementWorker(searchSvc))
	river.AddWorker(workers, jobs.NewPostProcessWorker(postProcessSvc))
	river.AddWorker(workers, jobs.NewDDLFetchWorker(trackingSvc))

	sweepInterval := time.Duration(cfg.RefreshIntervalHours) * time.Hour
	autoSearchInterval := time.Duration(cfg.AutoSearchIntervalHours) * time.Hour
	rssPollInterval := time.Duration(cfg.RSSPollIntervalMinutes) * time.Minute
	riverClient, err := jobs.New(ctx, database.Write, database.Read, cfg.RiverWorkers, sweepInterval, autoSearchInterval, rssPollInterval, downloadPollInterval, logger, workers)
	if err != nil {
		_ = database.Close()
		return fmt.Errorf("build jobs client: %w", err)
	}
	// Resolve the service<->jobs cycle: every service receives the client as its enqueuer.
	// The tracking service fans out to the replacement search (DL-04 Failed branch, 05-04)
	// and post-process (05-01 Completed branch); the search service fans out replacement
	// searches it enqueues from BlacklistRelease (DL-05).
	svc.SetEnqueuer(riverClient)
	searchSvc.SetEnqueuer(riverClient)
	trackingSvc.SetEnqueuer(riverClient)
	// The post-process service fans out a replacement search on a corrupt archive (D-16).
	postProcessSvc.SetEnqueuer(riverClient)
	// Wire the live-status publisher into the jobs client so enqueued jobs emit a JobStateEvent
	// the Activity view consumes live (UI-05/D-08).
	riverClient.SetPublisher(eventBus)

	// JobService reads run history from River's tables (via the jobs client).
	jobSvc := jobhistory.New(riverClient)

	// IndexerService owns DB-backed indexer CRUD (ADR 0007 domain segmentation).
	indexerSvc := indexerconfig.New(indexerconfig.Deps{Repos: repos, Logger: logger})

	// DownloadClientService owns the singleton DB-backed SABnzbd config (ADR 0007),
	// editable at runtime (supersedes D-16). The same SAB provider is injected as the
	// connectivity prober so Test probes the live, DB-resolved config.
	downloadClientSvc := downloadclient.New(downloadclient.Deps{Repos: repos, Logger: logger, Prober: sabProvider})

	// MetadataProviderService owns the DB-backed ComicVine config (ADR 0007), editable at
	// runtime. The live provider reads the key from the DB per request, so a saved key takes
	// effect on the next search with no restart. The ComicVine provider doubles as the
	// key-validation prober for Test (it resolves the stored key itself).
	metadataProviderSvc := metadataprovider.New(metadataprovider.Deps{
		Repos:  repos,
		Logger: logger,
		Prober: provider,
	})

	srv, err := newServer(cfg, logger, svc, jobSvc, indexerSvc, downloadClientSvc, metadataProviderSvc, renameConfigSvc, ddlConfigSvc, searchSvc, authSvc, eventBus, repos.Cover)
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
func newServer(cfg config.Config, logger *slog.Logger, svc *series.Service, jobSvc *jobhistory.Service, indexerSvc *indexerconfig.Service, downloadClientSvc *downloadclient.Service, metadataProviderSvc *metadataprovider.Service, renameConfigSvc *renameconfig.Service, ddlConfigSvc *ddlconfig.Service, searchSvc *search.Service, authSvc *auth.Service, eventBus *events.Bus, covers transport.CoverStore) (*http.Server, error) {
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

	downloadClientHandler := transport.NewDownloadClientHandler(downloadClientSvc)
	downloadClientPath, downloadClientH := omnibusv1connect.NewDownloadClientServiceHandler(downloadClientHandler, connect.WithInterceptors(interceptors...))
	mux.Handle("/api"+downloadClientPath, http.StripPrefix("/api", downloadClientH))

	metadataProviderHandler := transport.NewMetadataProviderHandler(metadataProviderSvc)
	metadataProviderPath, metadataProviderH := omnibusv1connect.NewMetadataProviderServiceHandler(metadataProviderHandler, connect.WithInterceptors(interceptors...))
	mux.Handle("/api"+metadataProviderPath, http.StripPrefix("/api", metadataProviderH))

	renameConfigHandler := transport.NewRenameConfigHandler(renameConfigSvc)
	renameConfigPath, renameConfigH := omnibusv1connect.NewRenameConfigServiceHandler(renameConfigHandler, connect.WithInterceptors(interceptors...))
	mux.Handle("/api"+renameConfigPath, http.StripPrefix("/api", renameConfigH))

	ddlConfigHandler := transport.NewDDLConfigHandler(ddlConfigSvc)
	ddlConfigPath, ddlConfigH := omnibusv1connect.NewDDLConfigServiceHandler(ddlConfigHandler, connect.WithInterceptors(interceptors...))
	mux.Handle("/api"+ddlConfigPath, http.StripPrefix("/api", ddlConfigH))

	searchHandler := transport.NewSearchHandler(searchSvc)
	searchPath, searchH := omnibusv1connect.NewSearchServiceHandler(searchHandler, connect.WithInterceptors(interceptors...))
	mux.Handle("/api"+searchPath, http.StripPrefix("/api", searchH))

	authHandler := transport.NewAuthHandler(authSvc, cfg.AuthTrustProxy)
	authPath, authH := omnibusv1connect.NewAuthServiceHandler(authHandler, connect.WithInterceptors(interceptors...))
	mux.Handle("/api"+authPath, http.StripPrefix("/api", authH))

	// EventService is the unified live-status server stream (UI-05, D-08). It is mounted under
	// /api like every other handler, so the auth gate (Plan 02) that wraps the whole mux gates
	// it uniformly — the session cookie on the initial stream request is validated once
	// (T-6-20, no ungated side-channel).
	eventHandler := transport.NewEventHandler(eventBus)
	eventPath, eventH := omnibusv1connect.NewEventServiceHandler(eventHandler, connect.WithInterceptors(interceptors...))
	mux.Handle("/api"+eventPath, http.StripPrefix("/api", eventH))

	mux.Handle("/covers/", transport.NewCoverHandler(covers))

	// The embedded SPA is the "/" catch-all, mounted LAST so it never shadows the
	// /api-prefixed Connect handlers or /covers/ above; it serves the built React app and
	// falls back to index.html for client-side routes (D-07). The auth gate (Plan 02)
	// wraps the whole mux so this route is covered uniformly.
	mux.Handle("/", NewSPAHandler())

	// The auth gate wraps the WHOLE mux (API + covers + SPA + future stream) BETWEEN the
	// mux and CORS/h2c, so it is the single fail-closed enforcement seam covering every
	// route uniformly (AUTH-01, ADR 0008, D-07). When auth is Off (default) it passes
	// everything; the AuthService itself stays reachable so a gated instance can be logged
	// into.
	gated := transport.NewAuthMiddleware(authSvc, cfg.AuthTrustProxy, mux)

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
	}).Handler(gated)

	return &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           h2c.NewHandler(corsHandler, &http2.Server{}),
		ReadHeaderTimeout: 10 * time.Second,
	}, nil
}

// sabnzbdResolver builds the DB-backed SABnzbd config resolver. SABnzbd config is now
// DB-backed and runtime-editable (supersedes D-16): URL/API key/category are resolved from
// repos.DownloadClient on each Submit/probe, so an empty stored URL yields a Submit that
// returns ErrSabnzbdNotConfigured (and a Test that reports "not configured").
func sabnzbdResolver(repos *repository.Repositories) download.SABnzbdConfigResolver {
	return func(ctx context.Context) (download.SABnzbdConfig, error) {
		row, err := repos.DownloadClient.Get(ctx)
		if err != nil {
			if errors.Is(err, repository.ErrNoDownloadClientConfig) {
				// No row yet: an empty config makes Submit fail loudly.
				return download.SABnzbdConfig{}, nil
			}
			return download.SABnzbdConfig{}, err
		}
		return download.SABnzbdConfig{
			BaseURL:  row.URL,
			APIKey:   row.APIKey,
			Category: row.Category,
		}, nil
	}
}

// seedComicVineConfig seeds the comicvine metadata_provider_config row from
// OMNIBUS_COMICVINE_API_KEY on first boot so existing deployments keep working after ComicVine
// config moved into the DB. It only seeds when the DB row is empty/absent AND
// cfg.ComicVineAPIKey is non-empty — it never overwrites a user-set DB row. The key is never
// logged.
func seedComicVineConfig(ctx context.Context, repos *repository.Repositories, cfg config.Config, logger *slog.Logger) error {
	row, err := repos.MetadataProviderConfig.Get(ctx, metadataprovider.ProviderComicVine)
	if err != nil && !errors.Is(err, repository.ErrNoMetadataProviderConfig) {
		return err
	}
	if row.APIKey != "" {
		// DB already has a user-set key; never overwrite it.
		return nil
	}
	if cfg.ComicVineAPIKey == "" {
		// Nothing to seed from.
		return nil
	}
	nowISO := time.Now().UTC().Format(time.RFC3339)
	if _, err := repos.MetadataProviderConfig.Upsert(ctx, repository.MetadataProviderConfigUpsert{
		Provider: metadataprovider.ProviderComicVine,
		APIKey:   cfg.ComicVineAPIKey,
	}, nowISO); err != nil {
		return err
	}
	logger.Info("seeded comicvine config from OMNIBUS_COMICVINE_API_KEY on first boot")
	return nil
}

// comicVineKeyResolver builds the DB-backed ComicVine API-key resolver the live provider calls
// on each request (mirrors sabnzbdResolver). The key is read fresh from
// metadata_provider_config, so a key saved via MetadataProviderService takes effect on the
// next search with no restart and no in-memory cache to drift. An absent row resolves to an
// empty key (which makes the next search fail loudly as "not configured"); a genuine read
// error is logged and likewise resolves to empty. The key is never logged.
func comicVineKeyResolver(repos *repository.Repositories, logger *slog.Logger) func(context.Context) string {
	return func(ctx context.Context) string {
		row, err := repos.MetadataProviderConfig.Get(ctx, metadataprovider.ProviderComicVine)
		if err != nil {
			if !errors.Is(err, repository.ErrNoMetadataProviderConfig) {
				logger.ErrorContext(ctx, "resolve comicvine api key", slog.String("error", err.Error()))
			}
			return ""
		}
		return row.APIKey
	}
}

// seedSabnzbdConfig seeds the singleton download_client_config row from OMNIBUS_SABNZBD_*
// on first boot so existing deployments keep working after SAB config moved into the DB
// (supersedes D-16). It only seeds when the DB row is empty/absent AND cfg.SabnzbdURL is
// non-empty — it never overwrites a user-set DB row.
func seedSabnzbdConfig(ctx context.Context, repos *repository.Repositories, cfg config.Config, logger *slog.Logger) error {
	row, err := repos.DownloadClient.Get(ctx)
	if err != nil && !errors.Is(err, repository.ErrNoDownloadClientConfig) {
		return err
	}
	if row.URL != "" {
		// DB already has a user-set config; never overwrite it.
		return nil
	}
	if cfg.SabnzbdURL == "" {
		// Nothing to seed from.
		return nil
	}
	nowISO := time.Now().UTC().Format(time.RFC3339)
	if _, err := repos.DownloadClient.Upsert(ctx, repository.DownloadClientConfigUpsert{
		URL:      cfg.SabnzbdURL,
		APIKey:   cfg.SabnzbdAPIKey,
		Category: cfg.SabnzbdCategory,
	}, nowISO); err != nil {
		return err
	}
	logger.Info("seeded download client config from OMNIBUS_SABNZBD_* on first boot",
		slog.String("sabnzbd_url", cfg.SabnzbdURL))
	return nil
}

// authSessionSecretKey is the user_config key the generated session-signing secret is
// persisted under when the operator did not configure one via OMNIBUS_AUTH_SESSION_SECRET.
const authSessionSecretKey = "auth.session_secret" //nolint:gosec // G101: this is a config KEY name, not a credential value

// resolveSessionSecret returns the HMAC session-signing secret for the auth gate. An
// operator-supplied secret (cfg.AuthSessionSecret) always wins. Otherwise it reuses a
// secret previously generated and persisted in user_config, or — on first boot — generates
// a 32-byte random secret, persists it, and returns it. This makes the gate work out of
// the box with no operator setup while keeping sessions valid across restarts. The secret
// is never logged (config redaction + this code never logs the value).
func resolveSessionSecret(ctx context.Context, repos *repository.Repositories, cfg config.Config, logger *slog.Logger) (string, error) {
	if cfg.AuthSessionSecret != "" {
		return cfg.AuthSessionSecret, nil
	}

	existing, err := repos.UserConfig.Get(ctx, authSessionSecretKey)
	if err == nil && existing != "" {
		return existing, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("read session secret: %w", err)
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(buf)
	if err := repos.UserConfig.Set(ctx, authSessionSecretKey, secret); err != nil {
		return "", fmt.Errorf("persist session secret: %w", err)
	}
	logger.Info("generated and persisted a new auth session-signing secret (no OMNIBUS_AUTH_SESSION_SECRET set)")
	return secret, nil
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
