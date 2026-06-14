// Package metadataprovider is the domain-segmented service (ADR 0007) that owns the DB-backed
// metadata-provider config (ComicVine today; the store is keyed by provider id so others can
// be added later). It depends only on repository.MetadataProviderConfigRepository and slog.
// The api_key is masked at this layer (defense in depth) so it never reaches transport. There
// is no in-memory key cache: the live ComicVine provider resolves its key straight from the
// DB per request, so a key saved here takes effect on the next search with no restart and
// nothing to drift.
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
// error — every outcome maps to (ok, detail). The app satisfies it with a real CV probe that,
// given a blank key, falls back to the live DB-resolved key — so Test never reads the store
// itself.
type MetadataProber interface {
	// Probe validates apiKey against live ComicVine, returning (ok, concise detail). A blank
	// apiKey probes the currently stored/resolved key. The key is never echoed in detail.
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
	// Prober validates a supplied/stored key against live ComicVine in Test. May be nil
	// (Test then reports the probe is unavailable rather than panicking).
	Prober MetadataProber
	// Now is an injectable clock for deterministic timestamps in tests. Zero falls back to
	// time.Now.
	Now func() time.Time
}

// Service implements MetadataProviderService domain logic over MetadataProviderConfigRepository.
type Service struct {
	repo   *repository.MetadataProviderConfigRepository
	logger *slog.Logger
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
		repo:   d.Repos.MetadataProviderConfig,
		logger: logger,
		prober: d.Prober,
		now:    now,
	}
}

// Get returns the masked config. An absent/empty row is treated as a zero config
// (configured=false), not an error.
func (s *Service) Get(ctx context.Context) (Config, error) {
	row, err := s.repo.Get(ctx, ProviderComicVine)
	if err != nil {
		if errors.Is(err, repository.ErrNoMetadataProviderConfig) {
			return Config{Provider: ProviderComicVine}, nil
		}
		return Config{}, fmt.Errorf("get metadata provider config: %w", err)
	}
	return configFromRow(row), nil
}

// Update validates and upserts the config. An empty APIKey leaves the stored key unchanged
// (it is NOT overwritten with a blank value), exactly like downloadclient.Update. The live
// provider reads the key from the DB per request, so the change takes effect on the next
// search with no restart. The key is never logged.
func (s *Service) Update(ctx context.Context, in Input) (Config, error) {
	apiKey := strings.TrimSpace(in.APIKey)
	if apiKey == "" {
		// Preserve the existing key rather than blanking it.
		existing, getErr := s.repo.Get(ctx, ProviderComicVine)
		if getErr != nil && !errors.Is(getErr, repository.ErrNoMetadataProviderConfig) {
			return Config{}, fmt.Errorf("load metadata provider config for update: %w", getErr)
		}
		apiKey = existing.APIKey
	}

	row, err := s.repo.Upsert(ctx, repository.MetadataProviderConfigUpsert{
		Provider: ProviderComicVine,
		APIKey:   apiKey,
	}, s.nowISO())
	if err != nil {
		return Config{}, fmt.Errorf("update metadata provider config: %w", err)
	}
	return configFromRow(row), nil
}

// Test validates the supplied key (or, when blank, the stored key) against live ComicVine via
// the injected prober. A nil prober yields a clear ok=false result rather than a panic. A bad
// key — or an unreachable provider — is a normal TestResult, never a Go error, so a transient
// DB/probe fault is never surfaced to the client as an RPC error. The prober resolves the
// stored key itself, so this method never reads the config store. The key is never logged.
func (s *Service) Test(ctx context.Context, apiKey string) (TestResult, error) {
	if s.prober == nil {
		return TestResult{OK: false, Detail: "metadata provider probe not available"}, nil
	}
	ok, detail := s.prober.Probe(ctx, apiKey)
	return TestResult{OK: ok, Detail: detail}, nil
}

func (s *Service) nowISO() string {
	return s.now().UTC().Format(time.RFC3339)
}
