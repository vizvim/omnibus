// Package indexer is the domain-segmented service (ADR 0007) that owns CRUD for
// DB-backed indexer records (D-16, SRCH-09). It depends only on the
// repository.IndexerRepository interface and slog. The api_key is masked at this
// layer (defense in depth) so it never reaches transport.
package indexer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/vizvim/omnibus/internal/repository"
)

// KindNewznab and KindGetComics are the only valid indexer kinds.
const (
	KindNewznab   = "newznab"
	KindGetComics = "getcomics"
)

// defaultNewznabCategories is the Newznab comic category applied when a newznab
// indexer is created without explicit categories (RESEARCH §4).
const defaultNewznabCategories = "7030"

// ErrInvalidKind is returned when an indexer kind is not newznab/getcomics.
var ErrInvalidKind = errors.New("indexer kind must be 'newznab' or 'getcomics'")

// ErrMissingField is returned when a required field (name, base_url) is blank.
var ErrMissingField = errors.New("indexer name and base_url are required")

// Deps are the Service's injected collaborators.
type Deps struct {
	Repos  *repository.Repositories
	Logger *slog.Logger
	// Now is an injectable clock for deterministic timestamps in tests. Zero falls
	// back to time.Now.
	Now func() time.Time
	// Prober runs the real connectivity probe in Test. Zero falls back to a default
	// HTTP prober (short timeout). Tests inject a fake.
	Prober IndexerProber
}

// Service implements IndexerService domain logic over IndexerRepository.
type Service struct {
	repo   repository.IndexerRepository
	logger *slog.Logger
	now    func() time.Time
	prober IndexerProber
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
	prober := d.Prober
	if prober == nil {
		prober = NewHTTPProber()
	}
	return &Service{repo: d.Repos.Indexers, logger: logger, now: now, prober: prober}
}

// List returns all indexers with the api_key masked.
func (s *Service) List(ctx context.Context) ([]Indexer, error) {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list indexers: %w", err)
	}
	out := make([]Indexer, 0, len(rows))
	for _, r := range rows {
		out = append(out, indexerFromRow(r))
	}
	return out, nil
}

// Create validates and inserts a new indexer.
func (s *Service) Create(ctx context.Context, in Input) (Indexer, error) {
	normalized, err := s.normalize(in)
	if err != nil {
		return Indexer{}, err
	}
	row, err := s.repo.Create(ctx, normalized, s.nowISO())
	if err != nil {
		return Indexer{}, fmt.Errorf("create indexer: %w", err)
	}
	return indexerFromRow(row), nil
}

// Update validates and updates an indexer. An empty APIKey leaves the stored key
// unchanged (it is NOT overwritten with a blank value).
func (s *Service) Update(ctx context.Context, id int64, in Input) (Indexer, error) {
	normalized, err := s.normalize(in)
	if err != nil {
		return Indexer{}, err
	}
	if normalized.APIKey == "" {
		// Preserve the existing key rather than blanking it.
		existing, getErr := s.repo.Get(ctx, id)
		if getErr != nil {
			return Indexer{}, fmt.Errorf("load indexer for update: %w", getErr)
		}
		normalized.APIKey = existing.APIKey
	}
	row, err := s.repo.Update(ctx, id, normalized, s.nowISO())
	if err != nil {
		return Indexer{}, fmt.Errorf("update indexer: %w", err)
	}
	return indexerFromRow(row), nil
}

// Delete removes an indexer by id.
func (s *Service) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete indexer: %w", err)
	}
	return nil
}

// TestResult is the outcome of a connectivity probe.
type TestResult struct {
	OK     bool
	Detail string
}

// Test probes an indexer's connectivity via the injected IndexerProber. Loading the row
// (which carries the api_key the probe needs) is a genuine internal fault when it fails —
// that returns a Go error. A reachable-but-failing probe is NOT an error: it is a normal
// TestResult{OK:false, Detail}.
func (s *Service) Test(ctx context.Context, id int64) (TestResult, error) {
	row, err := s.repo.Get(ctx, id)
	if err != nil {
		return TestResult{}, fmt.Errorf("load indexer for test: %w", err)
	}
	return s.prober.Probe(ctx, row), nil
}

// normalize validates the input and applies defaults (newznab category fallback).
func (s *Service) normalize(in Input) (repository.IndexerUpsert, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.Kind = strings.TrimSpace(in.Kind)
	in.Categories = strings.TrimSpace(in.Categories)

	if in.Kind != KindNewznab && in.Kind != KindGetComics {
		return repository.IndexerUpsert{}, ErrInvalidKind
	}
	if in.Name == "" || in.BaseURL == "" {
		return repository.IndexerUpsert{}, ErrMissingField
	}
	// Normalize AFTER the empty check so a blank URL stays an ErrMissingField rather than
	// being turned into "http://". A scheme-less host:port gains an http:// prefix and any
	// trailing slash is trimmed, so the DB + UI hold a well-formed URL that builds a valid
	// request.
	in.BaseURL = normalizeBaseURL(in.BaseURL)
	if in.Kind == KindNewznab && in.Categories == "" {
		in.Categories = defaultNewznabCategories
	}
	return repository.IndexerUpsert{
		Name:       in.Name,
		Kind:       in.Kind,
		BaseURL:    in.BaseURL,
		APIKey:     in.APIKey,
		Enabled:    in.Enabled,
		Categories: in.Categories,
		Priority:   in.Priority,
		UseForRSS:  in.UseForRSS,
	}, nil
}

func (s *Service) nowISO() string {
	return s.now().UTC().Format(time.RFC3339)
}

// normalizeBaseURL canonicalizes a stored indexer base URL using the SAME rule as the
// provider layer (kept dependency-free here so the service does not import the provider
// package): trim whitespace; an empty value stays empty; a scheme-less value gains an
// "http://" prefix (a bare host:port otherwise breaks http.NewRequest); a value that
// already has a scheme is untouched; trailing slashes are trimmed.
func normalizeBaseURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	return strings.TrimRight(s, "/")
}
