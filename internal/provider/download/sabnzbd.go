package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// sabProbeTimeout bounds a connectivity probe so a hung/slow SABnzbd host cannot stall the
// handler or the UI indefinitely (T-uzn-02).
const sabProbeTimeout = 8 * time.Second

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

var (
	_ DownloadProvider = (*SABnzbdProvider)(nil)
	_ Tracker          = (*SABnzbdProvider)(nil)
)

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

// Test runs a lightweight connectivity probe against SABnzbd (mode=version). It NEVER
// returns a Go error — every outcome maps to (ok, detail) so the caller can surface it as
// a normal response, not an RPC error. An empty resolved URL yields (false,"not
// configured"); a short timeout bounds a hung host.
func (p *SABnzbdProvider) Test(ctx context.Context) (ok bool, detail string) {
	cfg, err := p.resolver(ctx)
	if err != nil {
		return false, "config error"
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		return false, "not configured"
	}

	q := url.Values{}
	q.Set("mode", "version")
	q.Set("output", "json")
	q.Set("apikey", cfg.APIKey)
	endpoint := baseURL + "/api?" + q.Encode()

	probeCtx, cancel := context.WithTimeout(ctx, sabProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return false, "invalid url"
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return false, sabDialDetail(err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return false, "unauthorized (check API key)"
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return false, fmt.Sprintf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, "unexpected response"
	}
	// SAB returns an {"error": "API Key Incorrect"} JSON body with a 200 status on a bad
	// key, so inspect the parsed payload as well as the status code.
	var parsed struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false, "unexpected response"
	}
	if parsed.Error != "" {
		if strings.Contains(strings.ToLower(parsed.Error), "api key") {
			return false, "unauthorized (check API key)"
		}
		return false, "sabnzbd error"
	}
	return true, "connected"
}

// sabDialDetail maps a transport error to a concise, non-leaky detail string (T-uzn-01).
func sabDialDetail(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "context deadline exceeded"), strings.Contains(msg, "Client.Timeout"), strings.Contains(msg, "timeout"):
		return "timeout"
	case strings.Contains(msg, "connection refused"):
		return "connection refused"
	case strings.Contains(msg, "no such host"):
		return "host not found"
	default:
		return "unreachable"
	}
}

// sabQueueResp models the mode=queue&output=json response. SAB returns numeric fields as
// JSON STRINGS ("mb":"123.4"), so they are typed as string and parsed with strconv
// (RESEARCH Pitfall 2).
type sabQueueResp struct {
	Queue sabQueue `json:"queue"`
}

type sabQueue struct {
	Slots []sabQueueSlot `json:"slots"`
}

type sabQueueSlot struct {
	NzoID  string `json:"nzo_id"`
	Status string `json:"status"`
	MB     string `json:"mb"`
	MBLeft string `json:"mbleft"`
}

// sabHistResp models the mode=history&output=json response.
type sabHistResp struct {
	History sabHistory `json:"history"`
}

type sabHistory struct {
	Slots []sabHistSlot `json:"slots"`
}

type sabHistSlot struct {
	NzoID    string `json:"nzo_id"`
	Status   string `json:"status"`
	Storage  string `json:"storage"`
	FailMsg  string `json:"fail_message"`
	Category string `json:"category"`
}

// Poll reports the current state of one nzo_id following the pull-based *arr/Mylar
// "Completed Download Handling" standard (D-01): it queries mode=queue for in-progress
// state (progress % from mbleft/mb) and mode=history for terminal state + the final
// storage path. The SAB config is resolved per call so runtime edits apply. The two
// landmines are encoded here: numeric fields arrive as strings (parse with an mb>0 guard),
// and a Completed history entry with an empty storage path means "not yet ready" (re-poll),
// NOT success (RESEARCH Pitfalls 1+2).
func (p *SABnzbdProvider) Poll(ctx context.Context, clientRef string) (PollResult, error) {
	cfg, err := p.resolver(ctx)
	if err != nil {
		return PollResult{}, fmt.Errorf("resolve sabnzbd config: %w", err)
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		return PollResult{}, ErrSabnzbdNotConfigured
	}
	category := cfg.Category
	if category == "" {
		category = "comics"
	}

	// (1) Queue: is it still in flight? If found, report progress (in-progress).
	var queue sabQueueResp
	if err := p.sabGetJSON(ctx, baseURL, cfg.APIKey, category, "queue", clientRef, &queue); err != nil {
		return PollResult{}, err
	}
	for _, slot := range queue.Queue.Slots {
		if slot.NzoID != clientRef {
			continue
		}
		return PollResult{Status: PollInProgress, ProgressPct: sabProgressPct(slot.MB, slot.MBLeft)}, nil
	}

	// (2) History: terminal state + the final storage path.
	var hist sabHistResp
	if err := p.sabGetJSON(ctx, baseURL, cfg.APIKey, category, "history", clientRef, &hist); err != nil {
		return PollResult{}, err
	}
	for _, slot := range hist.History.Slots {
		if slot.NzoID != clientRef {
			continue
		}
		return classifySABHistory(slot), nil
	}

	// Neither queue nor history knows this ref yet: nothing actionable, re-poll next tick.
	return PollResult{Status: PollNotReady}, nil
}

// classifySABHistory maps one history slot to a PollResult. Completed-with-empty-storage is
// treated as not-ready (Pitfall 1); Failed carries the fail_message; every other status
// (Moving/Extracting/Verifying/Repairing/Running/Queued/...) is still in progress.
func classifySABHistory(slot sabHistSlot) PollResult {
	switch slot.Status {
	case "Completed":
		if strings.TrimSpace(slot.Storage) == "" {
			// SAB has not populated the final path yet; re-poll rather than declare success.
			return PollResult{Status: PollNotReady}
		}
		return PollResult{Status: PollCompleted, StoragePath: slot.Storage, ProgressPct: 100}
	case "Failed":
		return PollResult{Status: PollFailed, FailMessage: slot.FailMsg}
	default:
		// Moving / Extracting / Verifying / Repairing / Running / Queued: still working.
		return PollResult{Status: PollInProgress}
	}
}

// sabProgressPct computes a download percentage from SAB's string-typed mb/mbleft, guarded
// for mb>0 to avoid a divide-by-zero before the size is known (Pitfall 2). It returns a
// value in [0,100].
func sabProgressPct(mbStr, mbLeftStr string) float64 {
	mb, err := strconv.ParseFloat(strings.TrimSpace(mbStr), 64)
	if err != nil || mb <= 0 {
		return 0
	}
	mbLeft, err := strconv.ParseFloat(strings.TrimSpace(mbLeftStr), 64)
	if err != nil || mbLeft < 0 {
		mbLeft = 0
	}
	if mbLeft > mb {
		mbLeft = mb
	}
	return (mb - mbLeft) / mb * 100
}

// RemoveFromHistory removes a completed item from SAB by nzo_id (the *arr "Remove Completed"
// default, D-05): mode=history&name=delete&value=<nzo_id>. No del_files is sent on success;
// the hard-linked/copied file already persists in the library and Usenet does not seed.
func (p *SABnzbdProvider) RemoveFromHistory(ctx context.Context, clientRef string) error {
	cfg, err := p.resolver(ctx)
	if err != nil {
		return fmt.Errorf("resolve sabnzbd config: %w", err)
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		return ErrSabnzbdNotConfigured
	}

	q := url.Values{}
	q.Set("mode", "history")
	q.Set("name", "delete")
	q.Set("value", clientRef)
	q.Set("output", "json")
	q.Set("apikey", cfg.APIKey)
	endpoint := baseURL + "/api?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return fmt.Errorf("build sabnzbd history-delete request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		// Never leak the URL (it carries the apikey) — map to a concise detail (T-05-01).
		return fmt.Errorf("sabnzbd history-delete: %s", sabDialDetail(err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sabnzbd history-delete: status %d", resp.StatusCode)
	}
	return nil
}

// sabGetJSON issues a SAB API GET (mode=<mode>&output=json&nzo_ids=<ref>&cat=<cat>) and
// decodes the body into out. It reuses sabDialDetail so transport errors never leak the
// apikey-bearing URL (T-05-01).
func (p *SABnzbdProvider) sabGetJSON(ctx context.Context, baseURL, apiKey, category, mode, clientRef string, out any) error {
	q := url.Values{}
	q.Set("mode", mode)
	q.Set("output", "json")
	q.Set("nzo_ids", clientRef)
	q.Set("cat", category)
	q.Set("apikey", apiKey)
	endpoint := baseURL + "/api?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return fmt.Errorf("build sabnzbd %s request: %w", mode, err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("sabnzbd %s: %s", mode, sabDialDetail(err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sabnzbd %s: status %d", mode, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read sabnzbd %s body: %w", mode, err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("parse sabnzbd %s response: %w", mode, err)
	}
	return nil
}
