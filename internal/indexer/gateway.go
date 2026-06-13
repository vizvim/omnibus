package indexer

import (
	"context"
	"log/slog"
	"net/url"
	"sync"

	"golang.org/x/time/rate"
)

// defaultPerHostRate is the fixed-default pacing cadence per indexer host (D-11): ~1
// request per second, burst 1. This is the search analog of the ComicVine chokepoint.
const defaultPerHostRate = rate.Limit(1)

// hostKeyer is the optional interface a provider implements to expose the host its
// per-host limiter is keyed on. Newznab/GetComics satisfy it; a provider that does not
// falls back to its Kind() as the limiter key.
type hostKeyer interface {
	Host() string
}

// Gateway fronts a set of IndexerProviders with one rate limiter per indexer host
// (keyed by base_url host). Every provider fetch is preceded by limiter.Wait(ctx) — the
// single pacing chokepoint. A failing provider is logged and skipped; it never aborts
// the whole search.
type Gateway struct {
	rate    rate.Limit
	logger  *slog.Logger
	mu      sync.Mutex
	limiter map[string]*rate.Limiter
}

// NewGateway builds a Gateway. perHostRate <= 0 falls back to the fixed default cadence.
func NewGateway(logger *slog.Logger, perHostRate rate.Limit) *Gateway {
	if perHostRate <= 0 {
		perHostRate = defaultPerHostRate
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Gateway{
		rate:    perHostRate,
		logger:  logger,
		limiter: make(map[string]*rate.Limiter),
	}
}

// Search paces and aggregates a targeted search across providers.
func (g *Gateway) Search(ctx context.Context, providers []IndexerProvider, query string) ([]Candidate, error) {
	return g.run(ctx, providers, func(p IndexerProvider) ([]Candidate, error) {
		return p.Search(ctx, query)
	})
}

// Feed paces and aggregates the recent-uploads feed across providers.
func (g *Gateway) Feed(ctx context.Context, providers []IndexerProvider) ([]Candidate, error) {
	return g.run(ctx, providers, func(p IndexerProvider) ([]Candidate, error) {
		return p.Feed(ctx)
	})
}

// run paces each provider (per-host Wait BEFORE the network call) and aggregates
// candidates. A per-provider error is logged and that provider skipped — one dead
// indexer must not kill the whole search.
func (g *Gateway) run(
	ctx context.Context,
	providers []IndexerProvider,
	fetch func(IndexerProvider) ([]Candidate, error),
) ([]Candidate, error) {
	var out []Candidate
	for _, p := range providers {
		key := limiterKey(p)
		if err := g.wait(ctx, key); err != nil {
			// A limiter wait error means the context was canceled — stop entirely.
			return out, err
		}
		candidates, err := fetch(p)
		if err != nil {
			g.logger.Warn("indexer provider failed, skipping",
				slog.String("provider", p.Kind()), slog.String("host", key), slog.Any("error", err))
			continue
		}
		out = append(out, candidates...)
	}
	return out, nil
}

// wait blocks on the per-host limiter before any provider fetch.
func (g *Gateway) wait(ctx context.Context, key string) error {
	lim := g.limiterFor(key)
	g.logger.Debug("indexer rate-limit wait", slog.String("host", key))
	return lim.Wait(ctx)
}

// limiterFor returns (creating if needed) the limiter for a host key.
func (g *Gateway) limiterFor(key string) *rate.Limiter {
	g.mu.Lock()
	defer g.mu.Unlock()
	lim, ok := g.limiter[key]
	if !ok {
		lim = rate.NewLimiter(g.rate, 1)
		g.limiter[key] = lim
	}
	return lim
}

// limiterKey is the per-host limiter key for a provider (host, or Kind() fallback).
func limiterKey(p IndexerProvider) string {
	if hk, ok := p.(hostKeyer); ok {
		if host := hk.Host(); host != "" {
			return host
		}
	}
	return p.Kind()
}

// hostOf returns the host (host:port) component of a base URL, or the raw string if it
// cannot be parsed (so the limiter key is always non-empty and deterministic).
func hostOf(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return baseURL
	}
	return u.Host
}
