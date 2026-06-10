package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// defaultNewznabCategories is the comic category applied when none is configured.
const defaultNewznabCategories = "7030"

// NewznabProvider queries a Newznab-compatible indexer's XML API (SRCH-01). It does
// NOT rate-limit — the Gateway owns pacing.
type NewznabProvider struct {
	baseURL    string
	apiKey     string
	categories string
	client     *http.Client
}

// NewznabOption configures a NewznabProvider (test seams).
type NewznabOption func(*NewznabProvider)

// WithNewznabHTTPClient overrides the HTTP client (used by tests).
func WithNewznabHTTPClient(c *http.Client) NewznabOption {
	return func(p *NewznabProvider) { p.client = c }
}

// NewNewznabProvider builds a Newznab provider. categories defaults to 7030 (comics)
// when empty.
func NewNewznabProvider(baseURL, apiKey, categories string, opts ...NewznabOption) *NewznabProvider {
	if categories == "" {
		categories = defaultNewznabCategories
	}
	p := &NewznabProvider{
		baseURL:    normalizeBaseURL(baseURL),
		apiKey:     apiKey,
		categories: categories,
		client:     http.DefaultClient,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

var _ IndexerProvider = (*NewznabProvider)(nil)

// Kind reports the provider type.
func (p *NewznabProvider) Kind() string { return NewznabKind }

// Host returns the base_url host the gateway keys its per-host limiter on.
func (p *NewznabProvider) Host() string { return hostOf(p.baseURL) }

// Search runs a targeted Newznab search (t=search&q=...).
func (p *NewznabProvider) Search(ctx context.Context, query string) ([]Candidate, error) {
	return p.fetch(ctx, query)
}

// Feed returns the recent-uploads feed (same endpoint, no query) reusing the item parser.
func (p *NewznabProvider) Feed(ctx context.Context) ([]Candidate, error) {
	return p.fetch(ctx, "")
}

// newznabError models a top-level <error code=".." description=".."/> response. A
// newznab indexer returns HTTP 200 with this body for application-level failures
// (bad API key, suspended account, API disabled, rate-limited, etc).
type newznabError struct {
	Code        string `xml:"code,attr"`
	Description string `xml:"description,attr"`
}

// parseNewznabError reports a detail message when body is a newznab API error
// document. It echoes the server-supplied code/description verbatim — never a
// hardcoded code→message mapping. isErr is false when body is not an <error> doc
// (a clean <rss>/<caps> document has neither attr, so both fields stay empty).
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

// newznabRSS models the subset of the Newznab RSS/XML response we consume.
type newznabRSS struct {
	Channel struct {
		Items []newznabItem `xml:"item"`
	} `xml:"channel"`
}

type newznabItem struct {
	Title     string `xml:"title"`
	GUID      string `xml:"guid"`
	Enclosure struct {
		URL    string `xml:"url,attr"`
		Length int64  `xml:"length,attr"`
	} `xml:"enclosure"`
	Attrs []newznabAttr `xml:"attr"`
}

// newznabAttr is a <newznab:attr name=".." value=".."/> element. The namespace prefix
// is ignored by encoding/xml when matched on local name.
type newznabAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// fetch issues the request and parses items into Candidates.
func (p *NewznabProvider) fetch(ctx context.Context, query string) ([]Candidate, error) {
	q := url.Values{}
	q.Set("t", "search")
	q.Set("apikey", p.apiKey)
	q.Set("cat", p.categories)
	q.Set("o", "xml")
	if query != "" {
		q.Set("q", query)
	}
	endpoint := p.baseURL + "/api?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build newznab request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("newznab request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("newznab provider unavailable: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read newznab body: %w", err)
	}

	if detail, isErr := parseNewznabError(body); isErr {
		return nil, fmt.Errorf("%s", detail)
	}

	var rss newznabRSS
	if err := xml.Unmarshal(body, &rss); err != nil {
		return nil, fmt.Errorf("parse newznab xml: %w", err)
	}

	out := make([]Candidate, 0, len(rss.Channel.Items))
	for _, item := range rss.Channel.Items {
		out = append(out, p.toCandidate(item))
	}
	return out, nil
}

// toCandidate maps a parsed item to a Candidate.
func (p *NewznabProvider) toCandidate(item newznabItem) Candidate {
	size := item.Enclosure.Length
	if attrSize, ok := newznabAttrInt(item.Attrs, "size"); ok && attrSize > 0 {
		size = attrSize
	}

	releaseKey := strings.TrimSpace(item.GUID)
	if releaseKey == "" {
		releaseKey = hashURL(item.Enclosure.URL)
	}

	return Candidate{
		Provider:       p.Kind(),
		ReleaseKey:     releaseKey,
		Title:          item.Title,
		IssueNumberRaw: parseIssueNumber(item.Title),
		SizeBytes:      size,
		DownloadURL:    item.Enclosure.URL,
		Format:         inferFormat(item.Title),
		IsPack:         looksLikePack(item.Title),
	}
}

// newznabAttr name lookup returning an int.
func newznabAttrInt(attrs []newznabAttr, name string) (int64, bool) {
	for _, a := range attrs {
		if strings.EqualFold(a.Name, name) {
			if v, err := strconv.ParseInt(strings.TrimSpace(a.Value), 10, 64); err == nil {
				return v, true
			}
		}
	}
	return 0, false
}

// hashURL is the deterministic release-key fallback when an item has no guid.
func hashURL(u string) string {
	sum := sha256.Sum256([]byte(u))
	return hex.EncodeToString(sum[:])
}
