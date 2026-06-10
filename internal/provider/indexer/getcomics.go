package indexer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// defaultGetComicsBaseURL is the GetComics site root.
const defaultGetComicsBaseURL = "https://getcomics.org"

// GetComicsProvider scrapes GetComics search results (SRCH-02). GetComics has no API,
// so this parses the results HTML. It does NOT rate-limit — the Gateway owns pacing.
type GetComicsProvider struct {
	baseURL string
	client  *http.Client
}

// GetComicsOption configures a GetComicsProvider (test seams).
type GetComicsOption func(*GetComicsProvider)

// WithGetComicsHTTPClient overrides the HTTP client (used by tests).
func WithGetComicsHTTPClient(c *http.Client) GetComicsOption {
	return func(p *GetComicsProvider) { p.client = c }
}

// NewGetComicsProvider builds a GetComics provider; baseURL defaults to the site root.
func NewGetComicsProvider(baseURL string, opts ...GetComicsOption) *GetComicsProvider {
	if baseURL == "" {
		baseURL = defaultGetComicsBaseURL
	}
	p := &GetComicsProvider{
		baseURL: normalizeBaseURL(baseURL),
		client:  http.DefaultClient,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

var _ IndexerProvider = (*GetComicsProvider)(nil)

// Kind reports the provider type.
func (p *GetComicsProvider) Kind() string { return GetComicsKind }

// Host returns the base_url host the gateway keys its per-host limiter on.
func (p *GetComicsProvider) Host() string { return hostOf(p.baseURL) }

// Search scrapes the results page for a query.
func (p *GetComicsProvider) Search(ctx context.Context, query string) ([]Candidate, error) {
	q := url.Values{}
	q.Set("s", query)
	return p.fetch(ctx, p.baseURL+"/?"+q.Encode())
}

// Feed scrapes the front page (recent posts) using the same parser.
func (p *GetComicsProvider) Feed(ctx context.Context) ([]Candidate, error) {
	return p.fetch(ctx, p.baseURL+"/")
}

// sizeRe extracts a "Size : 123 MB/GB" value (best-effort) from post text.
var sizeRe = regexp.MustCompile(`(?i)size\s*:?\s*([\d.]+)\s*(MB|GB)`)

// fetch downloads and parses a GetComics results/listing page.
func (p *GetComicsProvider) fetch(ctx context.Context, pageURL string) ([]Candidate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build getcomics request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getcomics request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getcomics provider unavailable: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read getcomics body: %w", err)
	}

	return p.parseResults(string(body))
}

// post is a partially-built candidate during parsing.
type post struct {
	url   string
	title string
	size  int64
}

// enclosingArticle walks up to the nearest <article> (or <div class="post...">) so the
// size text belonging to a post can be scoped to that post. Returns nil if none.
func enclosingArticle(n *html.Node) *html.Node {
	for cur := n; cur != nil; cur = cur.Parent {
		if cur.Type == html.ElementNode {
			if cur.Data == "article" {
				return cur
			}
			if cur.Data == "div" && strings.Contains(attr(cur, "class"), "post") {
				return cur
			}
		}
	}
	return nil
}

// parseResults extracts post entries from a GetComics results page. GetComics renders
// each result as an <article> containing an <h1 class="post-title"><a href=postURL>.
// We walk the token stream and collect the first anchor inside each post-title heading.
// The parser is resilient to missing nodes (never panics) per the plan.
func (p *GetComicsProvider) parseResults(body string) ([]Candidate, error) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse getcomics html: %w", err)
	}

	var posts []post
	seen := map[string]bool{}

	var visit func(n *html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && isPostTitle(n) {
			if a := firstAnchor(n); a != nil {
				href := attr(a, "href")
				title := strings.TrimSpace(textContent(a))
				if href != "" && title != "" && !seen[href] {
					seen[href] = true
					var size int64
					if art := enclosingArticle(n); art != nil {
						size = parseSize(textContent(art))
					}
					posts = append(posts, post{url: href, title: title, size: size})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)

	// Size is parsed per-post from the enclosing article's text (best-effort). Per the
	// plan, a missing size yields 0 (not an error, not a rejection).
	out := make([]Candidate, 0, len(posts))
	for _, ps := range posts {
		out = append(out, Candidate{
			Provider:       p.Kind(),
			ReleaseKey:     ps.url,
			Title:          ps.title,
			IssueNumberRaw: parseIssueNumber(ps.title),
			SizeBytes:      ps.size,
			DownloadURL:    ps.url,
			Format:         getComicsFormat(ps.title),
			IsPack:         looksLikePack(ps.title),
		})
	}
	return out, nil
}

// getComicsFormat infers the format from the title, defaulting to cbz (GetComics packs
// are predominantly cbz) when no explicit extension is present.
func getComicsFormat(title string) string {
	if f := inferFormat(title); f != "" {
		return f
	}
	return "cbz"
}

// parseSize maps a "Size : 12 MB" string to bytes (0 when absent/unparseable).
func parseSize(text string) int64 {
	m := sizeRe.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	switch strings.ToUpper(m[2]) {
	case "GB":
		return int64(val * 1024 * 1024 * 1024)
	case "MB":
		return int64(val * 1024 * 1024)
	default:
		return 0
	}
}

// --- small html helpers (nil-safe) ---

func isPostTitle(n *html.Node) bool {
	if n.Data != "h1" && n.Data != "h2" && n.Data != "h3" {
		return false
	}
	class := attr(n, "class")
	return strings.Contains(class, "post-title") || strings.Contains(class, "entry-title")
}

func firstAnchor(n *html.Node) *html.Node {
	if n.Type == html.ElementNode && n.Data == "a" {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if a := firstAnchor(c); a != nil {
			return a
		}
	}
	return nil
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func textContent(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			sb.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}
