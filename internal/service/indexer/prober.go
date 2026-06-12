package indexer

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/vizvim/omnibus/internal/repository"
)

// defaultProbeTimeout bounds a connectivity probe so a hung/slow indexer host cannot
// stall the handler or the UI indefinitely (T-uzn-02).
const defaultProbeTimeout = 8 * time.Second

// IndexerProber performs a real connectivity probe against an indexer row. A probe NEVER
// returns a Go error — every outcome (success, auth failure, dial error) maps to a
// TestResult so the transport layer can return it as a normal response body, not an RPC
// error.
//
//nolint:revive // IndexerProber is the deliberate, canonical seam name (plan 260609-uzn); the package-qualified stutter is intentional and mirrors indexer.IndexerProvider.
type IndexerProber interface {
	Probe(ctx context.Context, row repository.IndexerRow) TestResult
}

// httpProber is the production IndexerProber: it issues a lightweight request (newznab
// t=caps, getcomics site root) and maps the outcome to a TestResult.
type httpProber struct {
	client *http.Client
}

// HTTPProberOption configures an httpProber (test seam).
type HTTPProberOption func(*httpProber)

// WithProberHTTPClient overrides the HTTP client (used by tests).
func WithProberHTTPClient(c *http.Client) HTTPProberOption {
	return func(p *httpProber) { p.client = c }
}

// NewHTTPProber builds an httpProber with a short default timeout.
func NewHTTPProber(opts ...HTTPProberOption) IndexerProber {
	p := &httpProber{
		client: &http.Client{Timeout: defaultProbeTimeout},
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Probe dispatches on the indexer kind. The base URL is normalized with the same rule as
// the service/provider so a scheme-less stored URL still probes correctly.
func (p *httpProber) Probe(ctx context.Context, row repository.IndexerRow) TestResult {
	switch row.Kind {
	case KindNewznab:
		return p.probeNewznab(ctx, row)
	case KindGetComics:
		return p.probeGetComics(ctx, row)
	default:
		return TestResult{OK: false, Detail: "unknown indexer kind"}
	}
}

// probeNewznab issues a t=caps request — a lightweight capabilities query that does not
// run a search. A 2xx body that XML-parses (even an empty caps doc) is treated as a live,
// reachable indexer.
func (p *httpProber) probeNewznab(ctx context.Context, row repository.IndexerRow) TestResult {
	q := url.Values{}
	q.Set("t", "caps")
	if row.APIKey != "" {
		q.Set("apikey", row.APIKey)
	}
	q.Set("o", "xml")
	endpoint := normalizeBaseURL(row.BaseURL) + "/api?" + q.Encode()

	body, status, err := p.get(ctx, endpoint)
	if err != nil {
		return TestResult{OK: false, Detail: dialDetail(err)}
	}
	if res, ok := statusDetail(status); !ok {
		return res
	}
	if !isWellFormedXML(body) {
		return TestResult{OK: false, Detail: "unexpected response"}
	}
	// A newznab indexer returns HTTP 200 with a well-formed <error code=".."
	// description=".."/> body for application-level failures (bad API key,
	// suspended, API disabled). Surface the server's own detail instead of
	// reporting a false-positive "connected".
	if detail, isErr := parseNewznabError(body); isErr {
		return TestResult{OK: false, Detail: detail}
	}
	return TestResult{OK: true, Detail: "connected"}
}

// newznabError models a top-level <error code=".." description=".."/> response.
type newznabError struct {
	Code        string `xml:"code,attr"`
	Description string `xml:"description,attr"`
}

// parseNewznabError reports a detail message when body is a newznab API error
// document, echoing the server-supplied code/description verbatim (never a
// hardcoded code→message mapping). isErr is false when body is not an <error>
// doc — a clean <caps>/<rss> document has neither attr, so both fields stay
// empty. This is a deliberate unexported copy of the provider/indexer helper:
// service/indexer keeps its newznab probe logic self-contained (mirroring its
// local normalizeBaseURL) rather than taking a package dependency on
// provider/indexer for ~10 lines.
func parseNewznabError(body []byte) (detail string, isErr bool) {
	var e newznabError
	if err := xml.Unmarshal(body, &e); err != nil {
		return "", false
	}
	// xml.Unmarshal only populates these when the ROOT element is <error>.
	if e.Code == "" && e.Description == "" {
		return "", false
	}
	switch {
	case e.Code != "" && e.Description != "":
		return fmt.Sprintf("newznab error %s: %s", e.Code, e.Description), true
	case e.Description != "":
		return "newznab error: " + e.Description, true
	default:
		return "newznab error " + e.Code, true
	}
}

// probeGetComics requests the site root; a 2xx means the scraper target is reachable.
func (p *httpProber) probeGetComics(ctx context.Context, row repository.IndexerRow) TestResult {
	endpoint := normalizeBaseURL(row.BaseURL) + "/"

	_, status, err := p.get(ctx, endpoint)
	if err != nil {
		return TestResult{OK: false, Detail: dialDetail(err)}
	}
	if res, ok := statusDetail(status); !ok {
		return res
	}
	return TestResult{OK: true, Detail: "connected"}
}

// get issues a GET and returns the body + status code. A non-nil error is a transport
// (dial/timeout) failure, never a non-2xx status.
func (p *httpProber) get(ctx context.Context, endpoint string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, 0, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// statusDetail maps a status code to a TestResult. The bool is true when the status is a
// 2xx (caller should continue); false means the returned TestResult is the final result.
func statusDetail(status int) (TestResult, bool) {
	switch {
	case status >= 200 && status < 300:
		return TestResult{}, true
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return TestResult{OK: false, Detail: "unauthorized (check API key)"}, false
	default:
		return TestResult{OK: false, Detail: fmt.Sprintf("status %d", status)}, false
	}
}

// isWellFormedXML reports whether body parses as XML (any root). A newznab t=caps
// response is XML; a 2xx body that does not parse signals an unexpected response (e.g. an
// HTML login page) rather than a live indexer API.
func isWellFormedXML(body []byte) bool {
	dec := xml.NewDecoder(bytes.NewReader(body))
	sawToken := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return sawToken
		}
		if err != nil {
			return false
		}
		if _, ok := tok.(xml.StartElement); ok {
			sawToken = true
		}
	}
}

// dialDetail produces a concise, non-leaky detail string from a transport error. It
// surfaces a recognizable cause (timeout / connection refused) without echoing the full
// upstream error chain (T-uzn-01).
func dialDetail(err error) string {
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
