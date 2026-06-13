// Package ddlconfig is the domain-segmented service (ADR 0007) that owns the DB-backed,
// runtime-toggleable Direct-Download (DDL) enable flag (05-07). It persists the flag as
// the user_config key enable_ddl (via repository.DDLConfigRepository) and defaults to
// OFF on a fresh install (DDL is opt-in). It is the config the search/grab path reads to
// gate the built-in GetComics fallback. It depends only on the repository interface and
// slog.
package ddlconfig

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/vizvim/omnibus/internal/repository"
)

// Deps are the Service's injected collaborators.
type Deps struct {
	Repos  *repository.Repositories
	Logger *slog.Logger
}

// Service implements DDLConfigService domain logic over DDLConfigRepository.
type Service struct {
	repo   *repository.DDLConfigRepository
	logger *slog.Logger
}

// New constructs a Service.
func New(d Deps) *Service {
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repo: d.Repos.DDLConfig, logger: logger}
}

// Get returns the DDL config. On a fresh DB (no enable_ddl key written yet) it returns
// the default-OFF posture (DDL is opt-in). Once the value has been written, the stored
// value is returned.
func (s *Service) Get(ctx context.Context) (Config, error) {
	row, err := s.repo.Get(ctx)
	if err != nil {
		return Config{}, fmt.Errorf("get ddl config: %w", err)
	}
	if !row.Present {
		return DefaultDDLConfig(), nil
	}
	return Config{Enabled: row.Enabled}, nil
}

// Update persists the DDL enable toggle. There is no required field to validate — the
// lone boolean is always valid. The value persists at runtime (no restart) and a
// subsequent Get returns it.
func (s *Service) Update(ctx context.Context, in Input) (Config, error) {
	row, err := s.repo.Upsert(ctx, in.Enabled)
	if err != nil {
		return Config{}, fmt.Errorf("update ddl config: %w", err)
	}
	return Config{Enabled: row.Enabled}, nil
}
