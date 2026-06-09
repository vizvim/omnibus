package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/time/rate"

	"github.com/vizvim/omnibus/internal/repository"
)

// timeFormat is the ISO-8601 UTC layout used for cache timestamps.
const timeFormat = time.RFC3339

// Gateway is the single ComicVine chokepoint. It fronts a MetadataProvider with one
// global token-bucket limiter and the metadata_cache, so no call path can bypass
// pacing or re-hit a fresh entry. Conditional refresh is keyed on cache TTL (and,
// later, CV date_last_updated).
type Gateway struct {
	provider MetadataProvider
	cache    repository.MetadataCacheRepository
	limiter  *rate.Limiter
	logger   *slog.Logger
	ttl      time.Duration
	now      func() time.Time
}

// NewGateway builds a Gateway. ttl bounds cache freshness; limiter paces every network
// fetch; logger receives cache hit/miss + rate-wait lines.
func NewGateway(
	provider MetadataProvider,
	cache repository.MetadataCacheRepository,
	limiter *rate.Limiter,
	logger *slog.Logger,
	ttl time.Duration,
) *Gateway {
	return &Gateway{
		provider: provider,
		cache:    cache,
		limiter:  limiter,
		logger:   logger,
		ttl:      ttl,
		now:      time.Now,
	}
}

// SearchSeries paces and caches a series search.
func (g *Gateway) SearchSeries(ctx context.Context, query string) ([]SeriesResult, error) {
	key := "search:" + query
	if raw, ok := g.freshCache(ctx, key); ok {
		return decodeSearch(raw)
	}
	if err := g.wait(ctx, key); err != nil {
		return nil, err
	}

	results, err := g.provider.SearchSeries(ctx, query)
	if err != nil {
		return nil, err
	}
	// The provider does not return raw bytes for search, so re-encode the normalized
	// results into a CV-shaped envelope that decodeSearch can read back on a cache hit.
	raw, encErr := encodeSearchEnvelope(results)
	if encErr != nil {
		g.logger.Warn("encode search cache", slog.Any("error", encErr))
		return results, nil
	}
	g.put(ctx, key, raw, "")
	return results, nil
}

// GetVolume paces and caches a volume fetch.
func (g *Gateway) GetVolume(ctx context.Context, volumeID int64) (VolumeDetail, error) {
	key := fmt.Sprintf("volume:4050-%d", volumeID)
	if raw, ok := g.freshCache(ctx, key); ok {
		return decodeVolume(raw)
	}
	if err := g.wait(ctx, key); err != nil {
		return VolumeDetail{}, err
	}

	detail, raw, err := g.provider.GetVolume(ctx, volumeID)
	if err != nil {
		return VolumeDetail{}, err
	}
	g.put(ctx, key, raw, detail.DateLastUpdated)
	return detail, nil
}

// ListIssues paces and caches one page of issues for a volume.
func (g *Gateway) ListIssues(ctx context.Context, volumeID int64, offset int) ([]IssueDetail, bool, error) {
	key := fmt.Sprintf("issues:4050-%d:%d", volumeID, offset)
	if raw, ok := g.freshCache(ctx, key); ok {
		issues, err := decodeIssues(raw)
		if err != nil {
			return nil, false, err
		}
		// hasMore is recomputed from the page size: a full page implies more.
		return issues, len(issues) == pageSize, nil
	}
	if err := g.wait(ctx, key); err != nil {
		return nil, false, err
	}

	issues, hasMore, raw, err := g.provider.ListIssues(ctx, volumeID, offset)
	if err != nil {
		return nil, false, err
	}
	g.put(ctx, key, raw, "")
	g.logger.Debug("imported issue page",
		slog.Int64("volume_id", volumeID), slog.Int("offset", offset), slog.Int("count", len(issues)))
	return issues, hasMore, nil
}

// GetCover paces and fetches a cover image (not cached in the metadata_cache — cover
// bytes are stored on disk by the import in 02-04).
func (g *Gateway) GetCover(ctx context.Context, url string) ([]byte, error) {
	if err := g.wait(ctx, "cover"); err != nil {
		return nil, err
	}
	return g.provider.GetCover(ctx, url)
}

// freshCache returns the cached raw payload for key if present and within TTL.
func (g *Gateway) freshCache(ctx context.Context, key string) ([]byte, bool) {
	entry, err := g.cache.Get(ctx, key)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			g.logger.Warn("cache lookup error", slog.String("key", key), slog.Any("error", err))
		}
		g.logger.Debug("cache miss", slog.String("key", key))
		return nil, false
	}

	fetchedAt, perr := time.Parse(timeFormat, entry.FetchedAt)
	if perr == nil && g.now().Sub(fetchedAt) >= g.ttl {
		g.logger.Debug("cache stale", slog.String("key", key))
		return nil, false
	}

	g.logger.Debug("cache hit", slog.String("key", key))
	return []byte(entry.Payload), true
}

// wait blocks on the global limiter before any network fetch. No fetch path
// may skip this — it is the single point that prevents a ComicVine ban.
func (g *Gateway) wait(ctx context.Context, key string) error {
	g.logger.Debug("rate-limit wait", slog.String("key", key))
	if err := g.limiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter wait: %w", err)
	}
	return nil
}

func (g *Gateway) put(ctx context.Context, key string, raw []byte, sourceUpdatedAt string) {
	err := g.cache.Put(ctx, repository.MetadataCacheEntry{
		CacheKey:        key,
		Payload:         string(raw),
		SourceUpdatedAt: sourceUpdatedAt,
		FetchedAt:       g.now().UTC().Format(timeFormat),
	})
	if err != nil {
		g.logger.Warn("cache put error", slog.String("key", key), slog.Any("error", err))
	}
}
