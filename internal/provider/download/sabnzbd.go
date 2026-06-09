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

// ErrSabnzbdNotConfigured is returned by a SAB provider built without a base URL — a
// NZB grab without SABnzbd configured must fail loudly, not silently succeed.
var ErrSabnzbdNotConfigured = errors.New("sabnzbd is not configured (set OMNIBUS_SABNZBD_URL)")

// SABnzbdProvider submits NZB URLs to a SABnzbd download client (DL-01).
type SABnzbdProvider struct {
	baseURL  string
	apiKey   string
	category string
	client   *http.Client
}

// SABnzbdOption configures a SABnzbdProvider (test seams).
type SABnzbdOption func(*SABnzbdProvider)

// WithSABnzbdHTTPClient overrides the HTTP client (used by tests).
func WithSABnzbdHTTPClient(c *http.Client) SABnzbdOption {
	return func(p *SABnzbdProvider) { p.client = c }
}

// NewSABnzbdProvider builds a SAB provider. An empty baseURL yields a provider whose
// Submit returns ErrSabnzbdNotConfigured.
func NewSABnzbdProvider(baseURL, apiKey, category string, opts ...SABnzbdOption) *SABnzbdProvider {
	if category == "" {
		category = "comics"
	}
	p := &SABnzbdProvider{
		baseURL:  strings.TrimRight(baseURL, "/"),
		apiKey:   apiKey,
		category: category,
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

// Submit hands the NZB URL to SABnzbd via mode=addurl and returns the first nzo_id.
func (p *SABnzbdProvider) Submit(ctx context.Context, req GrabRequest) (string, error) {
	if p.baseURL == "" {
		return "", ErrSabnzbdNotConfigured
	}

	q := url.Values{}
	q.Set("mode", "addurl")
	q.Set("name", req.DownloadURL)
	q.Set("cat", p.category)
	q.Set("apikey", p.apiKey)
	q.Set("output", "json")
	endpoint := p.baseURL + "/api?" + q.Encode()

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
