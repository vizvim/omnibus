package series

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// sweepBatchLimit bounds how many stale series a single sweep tick enqueues, keeping
// each tick small (the rest are picked up on subsequent ticks).
const sweepBatchLimit = 50

// RunRefresh performs a conditional metadata refresh for a series, satisfying
// jobs.RefreshRunner. It fetches the volume's current ComicVine date_last_updated
// (through the rate-limited gateway) and compares it to the cached source_updated_at:
//
//   - If both markers are non-empty and EQUAL, the series is unchanged upstream — this
//     is a cheap no-op: it records last_refreshed_at = now and skips the re-import
//     entirely (no ListIssues, no upserts) (D-05/D-06).
//   - Otherwise (changed, or no usable markers) it re-runs the existing idempotent
//     import upsert and then records last_refreshed_at = now.
//
// All ComicVine access goes through the gateway; this never bypasses the limiter.
func (s *Service) RunRefresh(ctx context.Context, seriesID, comicvineVolumeID int64) error {
	ser, err := s.repos.Series.GetByID(ctx, seriesID)
	if err != nil {
		return fmt.Errorf("load series %d: %w", seriesID, err)
	}

	// Fetch current marker (paced + cached through the gateway).
	vol, err := s.gw.GetVolume(ctx, comicvineVolumeID)
	if err != nil {
		return fmt.Errorf("fetch volume %d: %w", comicvineVolumeID, err)
	}

	cached, hasCached := s.gw.CachedSourceUpdatedAt(ctx, comicvineVolumeID)
	if hasCached && vol.DateLastUpdated != "" && vol.DateLastUpdated == cached {
		// Unchanged upstream — skip the re-import, just bump the refresh timestamp.
		s.logger.Info("refresh skipped — unchanged",
			slog.Int64("series_id", seriesID),
			slog.String("date_last_updated", vol.DateLastUpdated))
		if err := s.repos.Series.UpdateLastRefreshed(ctx, seriesID, nowISO()); err != nil {
			return fmt.Errorf("record last_refreshed_at for series %d: %w", seriesID, err)
		}
		return nil
	}

	// Changed (or no usable markers) — re-run the idempotent import, then record the
	// refresh timestamp.
	if err := s.runImport(ctx, ser, vol); err != nil {
		return err
	}
	if err := s.repos.Series.UpdateLastRefreshed(ctx, seriesID, nowISO()); err != nil {
		return fmt.Errorf("record last_refreshed_at for series %d: %w", seriesID, err)
	}
	return nil
}

// RunSweep is the scheduled stale-only refresh sweep (the body the River periodic sweep
// job invokes). It selects Active series whose last_refreshed_at is older than the
// staleness threshold (or never refreshed) and enqueues the SAME conditional refresh
// job per series — one at a time, gateway-paced, with River's unique-job dedup avoiding
// double work. It never calls ComicVine directly and never refreshes all series.
func (s *Service) RunSweep(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-s.stalenessThreshold).Format(time.RFC3339)
	stale, err := s.repos.Series.ListStale(ctx, cutoff, sweepBatchLimit)
	if err != nil {
		return fmt.Errorf("list stale series: %w", err)
	}

	enqueued := 0
	for _, ser := range stale {
		if err := s.enqueuer.EnqueueRefresh(ctx, ser.ID, ser.ComicvineVolumeID); err != nil {
			s.logger.Warn("sweep enqueue refresh", slog.Int64("series_id", ser.ID), slog.Any("error", err))
			continue
		}
		enqueued++
	}
	s.logger.Info("refresh sweep enqueued stale series",
		slog.Int("enqueued", enqueued), slog.String("cutoff", cutoff))
	return nil
}
