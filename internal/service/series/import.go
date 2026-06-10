package series

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/vizvim/omnibus/internal/provider/metadata"
	"github.com/vizvim/omnibus/internal/repository"
)

// runImport paginates the volume's issues through the gateway, upserts each (idempotent
// on comicvine_issue_id with a normalized (sort,qual)), stores covers in the SQLite
// covers table, and updates have/total counts as issues land. Re-running fills
// gaps. It checks ctx.Err() between pages and writes and returns it on cancellation so
// the River job is left for recovery (idempotent re-run is safe) when shutdown drains
// the engine mid-import.
func (s *Service) runImport(ctx context.Context, ser repository.Series, vol metadata.VolumeDetail) error {
	if err := ctx.Err(); err != nil {
		return err
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
			return err
		}

		issues, hasMore, err := s.gw.ListIssues(ctx, ser.ComicvineVolumeID, offset)
		if err != nil {
			return fmt.Errorf("list issues offset %d: %w", offset, err)
		}

		for _, iss := range issues {
			if err := ctx.Err(); err != nil {
				return err
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

			// Immediate enqueue on Wanted (D-10): an issue stored in the Wanted state gets
			// a one-off auto-search job. Re-imports of already-grabbed issues keep their
			// non-Wanted status (the upsert does not reset status), so only genuinely-Wanted
			// issues enqueue; duplicates collapse via the job's per-issue uniqueness.
			if s.enqueuer != nil && stored.Status == string(StatusWanted) {
				if eerr := s.enqueuer.EnqueueSearchIssue(ctx, stored.ID); eerr != nil {
					s.logger.Warn("enqueue auto-search", slog.Int64("issue_id", stored.ID), slog.Any("error", eerr))
				}
			}

			if iss.CoverURL != "" {
				if derr := s.downloadCover(ctx, "issues", stored.ID, iss.CoverURL); derr != nil {
					s.logger.Warn("store issue cover", slog.Int64("issue_id", stored.ID), slog.Any("error", derr))
				}
			}
		}

		// Update progress counts as issues land (the syncing signal).
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
