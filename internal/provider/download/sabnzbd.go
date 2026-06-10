package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ErrSabnzbdNotConfigured is returned by a SAB provider whose resolved base URL is empty —
// a NZB grab without SABnzbd configured must fail loudly, not silently succeed.
var ErrSabnzbdNotConfigured = errors.New("sabnzbd is not configured (set the download client URL in Settings)")

// SABnzbdConfig is the SABnzbd connection config resolved at grab time.
type SABnzbdConfig struct {
	BaseURL  string
	APIKey   string
	Category string
}

// SABnzbdConfigResolver resolves the current SABnzbd config on each Submit. It is invoked
// per grab so runtime edits (via the DB-backed DownloadClientService) take effect without
// a restart.
type SABnzbdConfigResolver func(ctx context.Context) (SABnzbdConfig, error)

// SABnzbdProvider submits NZB URLs to a SABnzbd download client (DL-01). Config is
// resolved from the DB on each Submit via the resolver (supersedes D-16's static config).
type SABnzbdProvider struct {
	resolver SABnzbdConfigResolver
	client   *http.Client
}

// SABnzbdOption configures a SABnzbdProvider (test seams).
type SABnzbdOption func(*SABnzbdProvider)

// WithSABnzbdHTTPClient overrides the HTTP client (used by tests).
func WithSABnzbdHTTPClient(c *http.Client) SABnzbdOption {
	return func(p *SABnzbdProvider) { p.client = c }
}

// NewSABnzbdProvider builds a SAB provider from a static config. An empty baseURL yields a
// provider whose Submit returns ErrSabnzbdNotConfigured. It wraps the static values in a
// resolver so existing callers/tests keep their behavior.
func NewSABnzbdProvider(baseURL, apiKey, category string, opts ...SABnzbdOption) *SABnzbdProvider {
	static := SABnzbdConfig{BaseURL: baseURL, APIKey: apiKey, Category: category}
	return NewSABnzbdProviderWithResolver(func(context.Context) (SABnzbdConfig, error) {
		return static, nil
	}, opts...)
}

// NewSABnzbdProviderWithResolver builds a SAB provider that resolves its config on each
// Submit. This lets the config be DB-backed and runtime-editable.
func NewSABnzbdProviderWithResolver(resolver SABnzbdConfigResolver, opts ...SABnzbdOption) *SABnzbdProvider {
	p := &SABnzbdProvider{
		resolver: resolver,
		client:   http.DefaultClient,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

var _ DownloadProvider = (*SABnzbdProvider)(nil)

// Kind reports the provider type.
func (p *SABnzbdProvider) Kind() string { return "sabnzbd" }

// sabResponse models the SAB addurl JSON response.
type sabResponse struct {
	Status bool     `json:"status"`
	NzoIDs []string `json:"nzo_ids"`
	Error  string   `json:"error"`
}

// Submit hands the NZB URL to SABnzbd via mode=addurl and returns the first nzo_id. The
// SABnzbd config is resolved from the DB at this point so runtime edits take effect.
func (p *SABnzbdProvider) Submit(ctx context.Context, req GrabRequest) (string, error) {
	cfg, err := p.resolver(ctx)
	if err != nil {
		return "", fmt.Errorf("resolve sabnzbd config: %w", err)
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		return "", ErrSabnzbdNotConfigured
	}
	category := cfg.Category
	if category == "" {
		category = "comics"
	}

	q := url.Values{}
	q.Set("mode", "addurl")
	q.Set("name", req.DownloadURL)
	q.Set("cat", category)
	q.Set("apikey", cfg.APIKey)
	q.Set("output", "json")
	endpoint := baseURL + "/api?" + q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("build sabnzbd request: %w", err)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("sabnzbd request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("sabnzbd unavailable: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read sabnzbd body: %w", err)
	}

	var parsed sabResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parse sabnzbd response: %w", err)
	}
	if !parsed.Status {
		detail := parsed.Error
		if detail == "" {
			detail = "sabnzbd reported failure"
		}
		return "", fmt.Errorf("sabnzbd addurl failed: %s", detail)
	}
	if len(parsed.NzoIDs) == 0 {
		return "", errors.New("sabnzbd addurl returned no nzo_id")
	}
	return parsed.NzoIDs[0], nil
}
