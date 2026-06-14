// Package metadataprovider is the domain-segmented service (ADR 0007) that owns the
// singleton DB-backed metadata-provider config (ComicVine today). It depends only on the
// repository.ComicVineConfigRepository interface and slog. The api_key is masked at this
// layer (defense in depth) so it never reaches transport, and is pushed into a
// hot-swappable KeyHolder on Update so the live provider picks it up on the next search
// with no restart.
package metadataprovider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/vizvim/omnibus/internal/repository"
)

// MetadataProber validates a ComicVine API key against live ComicVine. It NEVER returns a Go
// error — every outcome maps to (ok, detail). The app satisfies it with a real CV probe.
type MetadataProber interface {
	// Probe validates apiKey against live ComicVine, returning (ok, concise detail). The
	// key is never echoed in detail.
	Probe(ctx context.Context, apiKey string) (ok bool, detail string)
}

// TestResult is the outcome of a key-validation probe.
type TestResult struct {
	OK     bool
	Detail string
}

// Deps are the Service's injected collaborators.
type Deps struct {
	Repos  *repository.Repositories
	Logger *slog.Logger
	// Holder is the hot-swappable key holder the live ComicVine provider resolves its key
	// from. Update writes the resolved key here so it takes effect with no restart. May be
	// nil (Update then skips the hot-swap).
	Holder *KeyHolder
	// Prober validates a supplied/stored key against live ComicVine in Test. May be nil
	// (Test then reports the probe is unavailable rather than panicking).
	Prober MetadataProber
	// Now is an injectable clock for deterministic timestamps in tests. Zero falls back to
	// time.Now.
	Now func() time.Time
}

// Service implements MetadataProviderService domain logic over ComicVineConfigRepository.
type Service struct {
	repo   *repository.ComicVineConfigRepository
	logger *slog.Logger
	holder *KeyHolder
	prober MetadataProber
	now    func() time.Time
}

// New constructs a Service.
func New(d Deps) *Service {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		repo:   d.Repos.ComicVineConfig,
		logger: logger,
		holder: d.Holder,
		prober: d.Prober,
		now:    now,
	}
}

// Get returns the masked config. An absent/empty row is treated as a zero config
// (configured=false), not an error.
func (s *Service) Get(ctx context.Context) (Config, error) {
	row, err := s.repo.Get(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrNoComicVineConfig) {
			return Config{Provider: providerComicVine}, nil
		}
		return Config{}, fmt.Errorf("get comicvine config: %w", err)
	}
	return configFromRow(row), nil
}

// Update validates and upserts the config. An empty APIKey leaves the stored key unchanged
// (it is NOT overwritten with a blank value), exactly like downloadclient.Update. On success
// the resolved key is pushed into the hot-swappable holder so the live provider picks it up
// on the next search with no restart. The key is never logged.
func (s *Service) Update(ctx context.Context, in Input) (Config, error) {
	apiKey := strings.TrimSpace(in.APIKey)
	if apiKey == "" {
		// Preserve the existing key rather than blanking it.
		existing, getErr := s.repo.Get(ctx)
		if getErr != nil && !errors.Is(getErr, repository.ErrNoComicVineConfig) {
			return Config{}, fmt.Errorf("load comicvine config for update: %w", getErr)
		}
		apiKey = existing.APIKey
	}

	row, err := s.repo.Upsert(ctx, repository.ComicVineConfigUpsert{APIKey: apiKey}, s.nowISO())
	if err != nil {
		return Config{}, fmt.Errorf("update comicvine config: %w", err)
	}

	// Hot-swap the live provider's key so the change takes effect on the next search with no
	// restart. The key value is never logged.
	if s.holder != nil {
		s.holder.Set(row.APIKey)
	}
	return configFromRow(row), nil
}

// Test validates the supplied key (or, when blank, the stored key) against live ComicVine
// via the injected prober. A nil prober yields a clear ok=false result rather than a panic.
// A bad key is a normal TestResult, never a Go error. The key is never logged.
func (s *Service) Test(ctx context.Context, apiKey string) (TestResult, error) {
	if s.prober == nil {
		return TestResult{OK: false, Detail: "metadata provider probe not available"}, nil
	}
	key := strings.TrimSpace(apiKey)
	if key == "" {
		row, err := s.repo.Get(ctx)
		if err != nil {
			if errors.Is(err, repository.ErrNoComicVineConfig) {
				return TestResult{OK: false, Detail: "not configured"}, nil
			}
			return TestResult{}, fmt.Errorf("load comicvine config for test: %w", err)
		}
		key = row.APIKey
	}
	if key == "" {
		return TestResult{OK: false, Detail: "not configured"}, nil
	}
	ok, detail := s.prober.Probe(ctx, key)
	return TestResult{OK: ok, Detail: detail}, nil
}

func (s *Service) nowISO() string {
	return s.now().UTC().Format(time.RFC3339)
}
