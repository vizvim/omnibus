// Package renameconfig is the domain-segmented service (ADR 0007) that owns the
// DB-backed, runtime-editable renaming configuration (D-09). It persists the config as
// user_config keys (via repository.RenameConfigRepository) and exposes the user's literal
// Mylar defaults (D-06) on a fresh install. It is the config source the post-process
// template engine (05-02) renders against. It depends only on the repository interface
// and slog.
package renameconfig

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/vizvim/omnibus/internal/repository"
)

// validPaddingFormats are the issue-number padding widths the UI offers (D-07/D-09):
// "00x" (3-digit), "0x" (2-digit), "x" (none).
var validPaddingFormats = map[string]struct{}{
	"00x": {},
	"0x":  {},
	"x":   {},
}

// ErrMissingField is returned when a required field is blank — folder/file format must
// be non-empty while rename is enabled (T-05-04: reject zero-length paths). Mapped to
// InvalidArgument by transport.
var ErrMissingField = errors.New("folder format and file format are required when rename is enabled")

// Deps are the Service's injected collaborators.
type Deps struct {
	Repos  *repository.Repositories
	Logger *slog.Logger
}

// Service implements RenameConfigService domain logic over RenameConfigRepository.
type Service struct {
	repo   repository.RenameConfigRepository
	logger *slog.Logger
}

// New constructs a Service.
func New(d Deps) *Service {
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repo: d.Repos.RenameConfig, logger: logger}
}

// Get returns the renaming config. On a fresh DB (no renaming key written yet) it returns
// the D-06 Mylar defaults verbatim. Once any value has been written, the stored values
// are returned (with the padding format defaulted if it was somehow left blank).
func (s *Service) Get(ctx context.Context) (Config, error) {
	row, err := s.repo.Get(ctx)
	if err != nil {
		return Config{}, fmt.Errorf("get rename config: %w", err)
	}
	if !row.Present {
		return DefaultRenameConfig(), nil
	}
	cfg := configFromRow(row)
	if cfg.PaddingFormat == "" {
		cfg.PaddingFormat = defaultPaddingFormat
	}
	return cfg, nil
}

// Update validates and persists the renaming config. Folder/file format must be
// non-empty when rename is enabled (ErrMissingField otherwise). A blank or unknown
// padding format defaults to "00x". The values persist at runtime (no restart) and a
// subsequent Get returns them.
func (s *Service) Update(ctx context.Context, in Input) (Config, error) {
	folder := strings.TrimSpace(in.FolderFormat)
	file := strings.TrimSpace(in.FileFormat)

	if in.RenameEnabled && (folder == "" || file == "") {
		return Config{}, ErrMissingField
	}

	padding := strings.TrimSpace(in.PaddingFormat)
	if _, ok := validPaddingFormats[padding]; !ok {
		padding = defaultPaddingFormat
	}

	row, err := s.repo.Upsert(ctx, repository.RenameConfigUpsert{
		FolderFormat:        folder,
		FileFormat:          file,
		RenameEnabled:       in.RenameEnabled,
		IssuePaddingEnabled: in.IssuePaddingEnabled,
		PaddingFormat:       padding,
		ReplaceSpaces:       in.ReplaceSpaces,
		Lowercase:           in.Lowercase,
	})
	if err != nil {
		return Config{}, fmt.Errorf("update rename config: %w", err)
	}
	return configFromRow(row), nil
}
