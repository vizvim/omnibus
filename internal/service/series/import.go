package series

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/vizvim/omnibus/internal/provider/metadata"
	"github.com/vizvim/omnibus/internal/repository"
)

// startImport launches the bounded import goroutine for a series. The goroutine is
// tracked on the WaitGroup and bound to the lifecycle context so shutdown drains it
// (D-05, PLAT-08). This is the ONLY background construct in Phase 2 — no scheduler,
// no jobs table, no worker pool (Phase 3 / D-05).
func (s *Service) startImport(ser repository.Series, vol metadata.VolumeDetail) {
	s.wg.Go(func() {
		if err := s.runImport(s.lifeCtx, ser, vol); err != nil && !errors.Is(err, errImportCancelled) {
			s.logger.Error("series import failed",
				slog.Int64("series_id", ser.ID), slog.Any("error", err))
		}
	})
}

// runImport paginates the volume's issues through the gateway, upserts each (idempotent
// on comicvine_issue_id with a normalized (sort,qual)), stores covers in the SQLite
// covers table, and updates have/total counts as issues land (D-06). Re-running fills
// gaps (D-04). It checks ctx.Err() between pages and writes so shutdown stops it
// promptly (D-05).
func (s *Service) runImport(ctx context.Context, ser repository.Series, vol metadata.VolumeDetail) error {
	if err := ctx.Err(); err != nil {
		return errImportCancelled
	}

	// Series cover.
	if vol.CoverURL != "" {
		if err := s.downloadCover(ctx, "series", ser.ID, vol.CoverURL); err != nil {
			s.logger.Warn("store series cover", slog.Int64("series_id", ser.ID), slog.Any("error", err))
		}
	}

	offset := 0
	imported := 0
	for {
		if err := ctx.Err(); err != nil {
			return errImportCancelled
		}

		issues, hasMore, err := s.gw.ListIssues(ctx, ser.ComicvineVolumeID, offset)
		if err != nil {
			return fmt.Errorf("list issues offset %d: %w", offset, err)
		}

		for _, iss := range issues {
			if err := ctx.Err(); err != nil {
				return errImportCancelled
			}
			sortKey, qual := Normalize(iss.IssueNumber)
			stored, uerr := s.repos.Issue.Upsert(ctx, repository.IssueUpsert{
				SeriesID:         ser.ID,
				ComicvineIssueID: iss.ComicvineIssueID,
				IssueNumberRaw:   iss.IssueNumber,
				IssueNumberSort:  sortKey,
				IssueNumberQual:  qual,
				Title:            iss.Title,
				CoverDate:        iss.CoverDate,
				StoreDate:        iss.StoreDate,
				Status:           string(StatusWanted),
				CreatedAt:        nowISO(),
			})
			if uerr != nil {
				s.logger.Warn("upsert issue", slog.Int64("cv_issue_id", iss.ComicvineIssueID), slog.Any("error", uerr))
				continue
			}
			imported++

			if iss.CoverURL != "" {
				if derr := s.downloadCover(ctx, "issues", stored.ID, iss.CoverURL); derr != nil {
					s.logger.Warn("store issue cover", slog.Int64("issue_id", stored.ID), slog.Any("error", derr))
				}
			}
		}

		// Update progress counts as issues land (D-06 syncing signal).
		total := max(vol.CountOfIssues, int32(imported))
		if uerr := s.repos.Series.UpdateCounts(ctx, ser.ID, total, int32(imported)); uerr != nil {
			s.logger.Warn("update series counts", slog.Int64("series_id", ser.ID), slog.Any("error", uerr))
		}

		if !hasMore || len(issues) == 0 {
			break
		}
		offset += len(issues)
	}

	s.logger.Info("series import complete",
		slog.Int64("series_id", ser.ID), slog.Int("issues", imported))
	return nil
}

// downloadCover fetches cover bytes via the gateway (paced + SSRF-guarded) and stores
// them in the SQLite covers table keyed on (kind, id). The content type is detected from
// the fetched bytes, falling back to image/jpeg when detection yields the generic
// application/octet-stream (covers are always images).
func (s *Service) downloadCover(ctx context.Context, kind string, id int64, url string) error {
	data, err := s.gw.GetCover(ctx, url)
	if err != nil {
		return err
	}
	ct := http.DetectContentType(data)
	if ct == "application/octet-stream" {
		ct = "image/jpeg"
	}
	if err := s.repos.Cover.Upsert(ctx, kind, id, data, ct); err != nil {
		return fmt.Errorf("store cover: %w", err)
	}
	return nil
}
