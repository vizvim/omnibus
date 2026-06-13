// Package downloadclient is the domain-segmented service (ADR 0007) that owns the
// singleton DB-backed SABnzbd download-client config (supersedes D-16). It depends only
// on the repository.DownloadClientRepository interface and slog. The api_key is masked at
// this layer (defense in depth) so it never reaches transport.
package downloadclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/vizvim/omnibus/internal/repository"
)

// defaultCategory is applied when an update is submitted with a blank category.
const defaultCategory = "comics"

// ErrMissingField is returned when a required field (url) is blank.
var ErrMissingField = errors.New("download client url is required")

// SABProber runs a connectivity probe against the configured download client. It NEVER
// returns a Go error — every outcome maps to (ok, detail). *download.SABnzbdProvider
// satisfies it.
type SABProber interface {
	Test(ctx context.Context) (ok bool, detail string)
}

// TestResult is the outcome of a connectivity probe, mirroring indexer.TestResult.
type TestResult struct {
	OK     bool
	Detail string
}

// Deps are the Service's injected collaborators.
type Deps struct {
	Repos  *repository.Repositories
	Logger *slog.Logger
	// Now is an injectable clock for deterministic timestamps in tests. Zero falls back
	// to time.Now.
	Now func() time.Time
	// Prober runs the real SABnzbd connectivity probe in Test. May be nil (Test then
	// reports the probe is unavailable rather than panicking).
	Prober SABProber
}

// Service implements DownloadClientService domain logic over DownloadClientRepository.
type Service struct {
	repo   repository.DownloadClientRepository
	logger *slog.Logger
	now    func() time.Time
	prober SABProber
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
	return &Service{repo: d.Repos.DownloadClient, logger: logger, now: now, prober: d.Prober}
}

// Test probes the configured download client's connectivity via the injected prober. A nil
// prober yields a clear ok=false result rather than a panic. A failing probe is a normal
// TestResult, never a Go error.
func (s *Service) Test(ctx context.Context) (TestResult, error) {
	if s.prober == nil {
		return TestResult{OK: false, Detail: "download client probe not available"}, nil
	}
	ok, detail := s.prober.Test(ctx)
	return TestResult{OK: ok, Detail: detail}, nil
}

// Get returns the masked config. An absent/empty row is treated as a zero config
// (configured=false), not an error.
func (s *Service) Get(ctx context.Context) (Config, error) {
	row, err := s.repo.Get(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrNoDownloadClientConfig) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("get download client config: %w", err)
	}
	return configFromRow(row), nil
}

// Update validates and upserts the config. An empty URL is rejected (ErrMissingField). An
// empty Category defaults to "comics". An empty APIKey leaves the stored key unchanged
// (it is NOT overwritten with a blank value), exactly like indexer.Update.
func (s *Service) Update(ctx context.Context, in Input) (Config, error) {
	url := strings.TrimSpace(in.URL)
	apiKey := strings.TrimSpace(in.APIKey)
	category := strings.TrimSpace(in.Category)

	if url == "" {
		return Config{}, ErrMissingField
	}
	if category == "" {
		category = defaultCategory
	}
	if apiKey == "" {
		// Preserve the existing key rather than blanking it.
		existing, getErr := s.repo.Get(ctx)
		if getErr != nil && !errors.Is(getErr, repository.ErrNoDownloadClientConfig) {
			return Config{}, fmt.Errorf("load download client config for update: %w", getErr)
		}
		apiKey = existing.APIKey
	}

	row, err := s.repo.Upsert(ctx, repository.DownloadClientConfigUpsert{
		URL:      url,
		APIKey:   apiKey,
		Category: category,
	}, s.nowISO())
	if err != nil {
		return Config{}, fmt.Errorf("update download client config: %w", err)
	}
	return configFromRow(row), nil
}

func (s *Service) nowISO() string {
	return s.now().UTC().Format(time.RFC3339)
}
