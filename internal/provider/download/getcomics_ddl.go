package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
)

// ErrMirrorExhaustion is returned by Fetch when no candidate mirror on the GetComics post
// page yields a usable file (all mirrors 4xx/5xx/unreachable or report no Content-Length).
// This is the D-04 mirror-exhaustion signal the DDLFetch job routes into the shared failure
// path (download Failed + replacement search).
var ErrMirrorExhaustion = errors.New("getcomics ddl: all mirrors exhausted")

// GetComicsDDLProvider initiates a GetComics direct-download (DL-02) and, on Fetch, resolves
// a working mirror from the post page and streams the file to an intermediate dir under
// data_path. The clientRef is the post URL the resolver re-fetches to discover mirror links.
type GetComicsDDLProvider struct {
	baseURL  string
	client   *http.Client
	dataPath string
}

// GetComicsDDLOption configures a GetComicsDDLProvider (test seams + data path injection).
type GetComicsDDLOption func(*GetComicsDDLProvider)

// WithDDLHTTPClient overrides the HTTP client used to fetch the post page + stream mirrors
// (used by tests to point at an httptest server). The default is http.DefaultClient.
func WithDDLHTTPClient(c *http.Client) GetComicsDDLOption {
	return func(p *GetComicsDDLProvider) { p.client = c }
}

// WithDDLDataPath sets the data_path root under which the intermediate `incomplete/` dir is
// created for streamed files. When unset, Fetch streams into the OS temp dir (defensive).
func WithDDLDataPath(dataPath string) GetComicsDDLOption {
	return func(p *GetComicsDDLProvider) { p.dataPath = dataPath }
}

// NewGetComicsDDLProvider builds a GetComics DDL provider.
func NewGetComicsDDLProvider(baseURL string, opts ...GetComicsDDLOption) *GetComicsDDLProvider {
	p := &GetComicsDDLProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  http.DefaultClient,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

var _ DownloadProvider = (*GetComicsDDLProvider)(nil)

// Kind reports the provider type.
func (p *GetComicsDDLProvider) Kind() string { return "getcomics" }

// Submit records the DDL handoff and returns the post URL as the client reference. The real
// mirror resolution + byte fetch happens in Fetch (driven by the DDLFetch job); Submit only
// validates the request and returns a stable reference so the downloads row + snatched event
// can be written.
func (p *GetComicsDDLProvider) Submit(_ context.Context, req GrabRequest) (string, error) {
	if req.DownloadURL == "" {
		return "", errors.New("getcomics ddl: empty download url")
	}
	// The post URL is the stable client reference Fetch resolves to a working mirror.
	return req.DownloadURL, nil
}

// Fetch resolves a working mirror from the GetComics post page (clientRef) and streams the
// file into the intermediate `<data_path>/incomplete/` dir, reporting progress from the
// mirror's Content-Length. It tries candidate mirrors in preference order ("Download Now" /
// "Mirror Download" first, then cloud hosts) and returns the local path of the first mirror
// that streams successfully. When every candidate is dead (4xx/5xx/unreachable/no
// Content-Length) it returns ErrMirrorExhaustion (D-04). The whole body is NEVER buffered in
// memory — io.Copy streams directly to disk.
//
// progress, when non-nil, is called periodically with (bytesWritten, totalBytes). It is
// optional (a nil progress func is a no-op).
func (p *GetComicsDDLProvider) Fetch(ctx context.Context, clientRef string, progress func(done, total int64)) (string, error) {
	mirrors, err := p.resolveMirrors(ctx, clientRef)
	if err != nil {
		return "", err
	}
	if len(mirrors) == 0 {
		return "", fmt.Errorf("%w: no mirror links found on post page", ErrMirrorExhaustion)
	}

	dir, err := p.incompleteDir()
	if err != nil {
		return "", err
	}

	var lastErr error
	for _, mirror := range mirrors {
		dst, ferr := p.streamMirror(ctx, mirror, dir, progress)
		if ferr == nil {
			return dst, nil
		}
		// Try the next mirror — a single bad mirror (ad page / dead host) is expected.
		lastErr = ferr
	}
	if lastErr != nil {
		return "", fmt.Errorf("%w: %w", ErrMirrorExhaustion, lastErr)
	}
	return "", ErrMirrorExhaustion
}

// resolveMirrors fetches the post page (clientRef) and extracts candidate mirror download
// links in preference order. Only links parsed from the trusted GetComics post page are
// followed (T-05-15 SSRF mitigation: no arbitrary user-supplied redirect targets).
func (p *GetComicsDDLProvider) resolveMirrors(ctx context.Context, postURL string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, postURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build getcomics post request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch getcomics post page: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getcomics post page unavailable: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read getcomics post page: %w", err)
	}
	return parseMirrorLinks(string(body)), nil
}

// streamMirror streams a single mirror to <dir>/<sanitized filename>, using Content-Length
// for progress. A mirror with a non-200 status or an absent/zero Content-Length is rejected
// (Mylar treats a missing Content-Length as a bad/ad page, T-05-17). On success it returns
// the local path. The body is streamed with io.Copy — never fully buffered.
func (p *GetComicsDDLProvider) streamMirror(ctx context.Context, mirrorURL, dir string, progress func(done, total int64)) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mirrorURL, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("build mirror request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("mirror request %q: %w", mirrorURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mirror %q: status %d", mirrorURL, resp.StatusCode)
	}
	total := resp.ContentLength
	if total <= 0 {
		// Mylar's rule: a missing/zero Content-Length means this is a click-bait/ad page,
		// not the file. Skip this mirror.
		return "", fmt.Errorf("mirror %q: absent or zero Content-Length", mirrorURL)
	}

	dst, err := p.destPath(dir, mirrorURL, resp)
	if err != nil {
		return "", err
	}

	f, err := os.Create(dst)
	if err != nil {
		return "", fmt.Errorf("create intermediate file: %w", err)
	}
	pr := &progressReader{r: resp.Body, total: total, onTick: progress}
	if _, cerr := io.Copy(f, pr); cerr != nil {
		_ = f.Close()
		_ = os.Remove(dst) // do not leave a partial file behind on a failed stream
		return "", fmt.Errorf("stream mirror %q: %w", mirrorURL, cerr)
	}
	if cerr := f.Close(); cerr != nil {
		return "", fmt.Errorf("close intermediate file: %w", cerr)
	}
	return dst, nil
}

// destPath computes the sanitized destination path under dir for a mirror response. The
// filename is taken from the URL path (preferring a Content-Disposition filename when the
// server sends one), filepath.Clean'd, stripped of any directory components, and asserted to
// stay under dir (T-05-16: never trust the remote name verbatim, reject path escapes).
func (p *GetComicsDDLProvider) destPath(dir, mirrorURL string, resp *http.Response) (string, error) {
	name := filenameFromResponse(mirrorURL, resp)
	// Strip any directory components the remote name carries, then Clean — the result is a
	// bare base name that cannot escape dir.
	name = filepath.Base(filepath.Clean("/" + name))
	if name == "" || name == "." || name == "/" || name == string(filepath.Separator) {
		return "", fmt.Errorf("mirror %q: unusable remote filename", mirrorURL)
	}
	dst := filepath.Join(dir, name)
	// Defense-in-depth containment assertion (boundary-safe, not a glob prefix).
	cleanDir := filepath.Clean(dir)
	if dst != filepath.Join(cleanDir, name) || !strings.HasPrefix(dst, cleanDir+string(filepath.Separator)) {
		return "", fmt.Errorf("mirror %q: resolved path %q escapes intermediate dir", mirrorURL, dst)
	}
	return dst, nil
}

// incompleteDir returns (creating if needed) the intermediate dir streamed files land in:
// <data_path>/incomplete (Claude's discretion, RESEARCH A3). When no data_path was injected
// it falls back to the OS temp dir (defensive — app.go always injects data_path).
func (p *GetComicsDDLProvider) incompleteDir() (string, error) {
	root := p.dataPath
	if strings.TrimSpace(root) == "" {
		root = os.TempDir()
	}
	dir := filepath.Join(root, "incomplete")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create intermediate dir: %w", err)
	}
	return dir, nil
}

// filenameFromResponse derives a download filename, preferring a Content-Disposition filename
// then falling back to the last path segment of the mirror URL.
func filenameFromResponse(mirrorURL string, resp *http.Response) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if fn := strings.TrimSpace(params["filename"]); fn != "" {
				return fn
			}
		}
	}
	if u, err := url.Parse(mirrorURL); err == nil {
		if base := path.Base(u.Path); base != "" && base != "/" && base != "." {
			return base
		}
	}
	return ""
}

// --- mirror-link parsing (golang.org/x/net/html, mirroring the indexer's nil-safe walk) ---

// mirrorPriority ranks an anchor by its preference: a "Download Now"/"Mirror Download" link
// (the GetComics primary buttons) outranks a cloud host (mega/pixeldrain/mediafire). A lower
// number sorts first. -1 means "not a download link" (skip).
func mirrorPriority(text, title string) int {
	hay := strings.ToLower(text + " " + title)
	switch {
	case strings.Contains(hay, "download now"):
		return 0
	case strings.Contains(hay, "mirror download"):
		return 1
	case strings.Contains(hay, "mediafire"):
		return 2
	case strings.Contains(hay, "pixeldrain"):
		return 3
	case strings.Contains(hay, "mega"):
		return 4
	default:
		return -1
	}
}

// parseMirrorLinks extracts candidate mirror download URLs from a GetComics post page in
// preference order. It walks anchors, ranking each by mirrorPriority, then returns the hrefs
// ordered best-first with duplicates removed. The parser is nil-safe (never panics).
func parseMirrorLinks(body string) []string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil
	}

	type ranked struct {
		href string
		prio int
	}
	var found []ranked
	seen := map[string]bool{}

	var visit func(n *html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			href := strings.TrimSpace(nodeAttr(n, "href"))
			title := nodeAttr(n, "title")
			text := nodeText(n)
			if href != "" && !seen[href] {
				if prio := mirrorPriority(text, title); prio >= 0 {
					seen[href] = true
					found = append(found, ranked{href: href, prio: prio})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)

	// Stable insertion-order sort by priority (lower first) — a simple selection sort keeps
	// equal-priority links in document order without pulling in sort for a tiny slice.
	for i := 0; i < len(found); i++ {
		best := i
		for j := i + 1; j < len(found); j++ {
			if found[j].prio < found[best].prio {
				best = j
			}
		}
		found[i], found[best] = found[best], found[i]
	}

	out := make([]string, 0, len(found))
	for _, r := range found {
		out = append(out, r.href)
	}
	return out
}

// nodeAttr returns the value of a node attribute (empty string if absent).
func nodeAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// nodeText returns the concatenated text content of a node subtree.
func nodeText(n *html.Node) string {
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

// progressReader wraps an io.Reader to emit (bytesRead, total) progress as bytes flow through
// io.Copy. It never buffers — it counts as the underlying stream is read.
type progressReader struct {
	r      io.Reader
	total  int64
	read   int64
	onTick func(done, total int64)
}

// Read counts bytes and emits progress (when an onTick callback is set) as the stream flows.
func (pr *progressReader) Read(b []byte) (int, error) {
	n, err := pr.r.Read(b)
	if n > 0 {
		pr.read += int64(n)
		if pr.onTick != nil {
			pr.onTick(pr.read, pr.total)
		}
	}
	return n, err
}
