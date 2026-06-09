// Command omnibus is the single entrypoint for the omnibus server. It wires the
// Connect transport over h2c and serves the SeriesService. Configuration,
// persistence, providers, and graceful shutdown are layered in by later Phase 2
// plans (02-02 onward); this plan (02-01) establishes the runnable transport seam.
package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/rs/cors"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	omnibusv1connect "github.com/vizvim/omnibus/gen/go/omnibus/v1/omnibusv1connect"
	"github.com/vizvim/omnibus/internal/transport"
)

// httpAddr is hardcoded for plan 02-01; config-driven addressing (PLAT-07) lands
// in plan 02-02.
const httpAddr = ":8080"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(logger); err != nil {
		logger.Error("server exited with error", slog.Any("error", err))
		os.Exit(1)
	}
}

// run builds the HTTP server and serves until it stops. It is separated from main
// so it can return an error (main owns the process exit code) and so tests can
// exercise the wiring without os.Exit.
func run(logger *slog.Logger) error {
	mux := http.NewServeMux()

	seriesHandler := transport.NewSeriesHandler()
	path, handler := omnibusv1connect.NewSeriesServiceHandler(seriesHandler)
	mux.Handle(path, handler)

	// Restrict CORS to the Vite dev origin; production is same-origin. AllowAllOrigins
	// is deliberately not used (threat T-02-01-CORS).
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
		ExposedHeaders: []string{
			"Grpc-Status",
			"Grpc-Message",
			"Grpc-Status-Details-Bin",
		},
	}).Handler(mux)

	srv := &http.Server{
		Addr: httpAddr,
		// h2c serves cleartext HTTP/2 so gRPC/Connect clients work without TLS
		// inside the container (operator terminates TLS at their proxy).
		Handler:           h2c.NewHandler(corsHandler, &http2.Server{}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	logger.Info("omnibus listening", slog.String("addr", httpAddr))

	// Serving via *http.Server (not http.ListenAndServe) keeps Shutdown available
	// for the graceful-shutdown wiring that plan 02-02 adds.
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}
