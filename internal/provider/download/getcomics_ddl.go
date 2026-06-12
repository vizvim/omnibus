package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

// ddlStallWindow bounds how long streamMirror will wait for the next chunk of bytes from a
// mirror before aborting the stream as stalled (WR-01). A slow-loris mirror that trickles
// bytes under a valid Content-Length would otherwise block the serialized "ddl" queue
// (MaxWorkers:1) indefinitely. This is a per-read liveness bound, not an overall deadline.
const ddlStallWindow = 60 * time.Second

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
		client:  ssrfGuardedClient(http.DefaultClient),
	}
	for _, o := range opts {
		o(p)
	}
	// Ensure any injected client is also SSRF-guarded on redirects (T-05-15): a benign
	// first-hop URL can 302 the server into the internal network, so every redirect hop is
	// re-validated against the scheme/host allowlist regardless of who supplied the client.
	p.client = ssrfGuardedClient(p.client)
	return p
}

// errRejectedTarget marks a URL rejected by the SSRF guard (bad scheme, missing host, or a
// loopback/link-local/private/metadata destination). Used by CheckRedirect to refuse a hop.
var errRejectedTarget = errors.New("getcomics ddl: rejected target (ssrf guard)")

// safeMirrorURL validates a post/mirror URL before it is dialed (T-05-15 SSRF mitigation).
// It enforces an http/https scheme allowlist, requires a host, and — when the host is a
// literal IP (or every resolved IP) — rejects loopback, link-local, private-range, and
// unspecified addresses (e.g. the 169.254.169.254 cloud-metadata endpoint). A page parsed
// off a remote-influenced GetComics post is NOT a trust boundary, so this runs for every URL.
func safeMirrorURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: parse %q: %v", errRejectedTarget, raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%w: scheme %q", errRejectedTarget, u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("%w: no host", errRejectedTarget)
	}
	if err := assertPublicHost(host); err != nil {
		return nil, err
	}
	return u, nil
}

// assertPublicHost rejects a host that resolves (or is) a loopback/link-local/private/
// unspecified IP — the SSRF sink set (internal services, the 169.254.169.254 metadata IP).
// A hostname is resolved and rejected if ANY resolved address is internal (fail-closed).
func assertPublicHost(host string) error {
	if ip := net.ParseIP(host); ip != nil {
		return assertPublicIP(ip)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("%w: resolve %q: %v", errRejectedTarget, host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("%w: host %q resolved to no addresses", errRejectedTarget, host)
	}
	for _, ip := range ips {
		if err := assertPublicIP(ip); err != nil {
			return err
		}
	}
	return nil
}

// assertPublicIP rejects an IP in a loopback/link-local/private/unspecified range.
func assertPublicIP(ip net.IP) error {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() {
		return fmt.Errorf("%w: address %s is loopback/link-local/private", errRejectedTarget, ip)
	}
	return nil
}

// ssrfGuardedClient wraps base with a CheckRedirect that re-validates every redirect hop
// against safeMirrorURL (T-05-15). It clones base so the caller's client is not mutated, and
// caps the redirect chain. A nil base defaults to http.DefaultClient.
func ssrfGuardedClient(base *http.Client) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	cloned := *base
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("%w: too many redirects", errRejectedTarget)
		}
		if _, err := safeMirrorURL(req.URL.String()); err != nil {
			return err
		}
		return nil
	}
	return &cloned
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
	if _, err := safeMirrorURL(postURL); err != nil {
		return nil, fmt.Errorf("reject getcomics post url: %w", err)
	}
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
	if _, err := safeMirrorURL(mirrorURL); err != nil {
		return "", fmt.Errorf("reject mirror url: %w", err)
	}
	// A per-mirror cancellable context backs the stall watchdog (WR-01): if the stream goes
	// silent for ddlStallWindow, the watchdog cancels streamCtx, unblocking io.Copy with a
	// context error instead of hanging the serialized ddl queue forever.
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, mirrorURL, http.NoBody)
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
	stopWatchdog := startStallWatchdog(pr, ddlStallWindow, cancel)
	_, cerr := io.Copy(f, pr)
	stopWatchdog()
	if cerr != nil {
		_ = f.Close()
		_ = os.Remove(dst) // do not leave a partial file behind on a failed/stalled stream
		if streamCtx.Err() != nil {
			return "", fmt.Errorf("stream mirror %q stalled (no bytes for %s): %w", mirrorURL, ddlStallWindow, streamCtx.Err())
		}
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
// io.Copy. It never buffers — it counts as the underlying stream is read. lastRead records the
// time of the most recent non-zero read so the stall watchdog (WR-01) can detect a silent
// mirror; it is guarded by mu because the watchdog reads it from another goroutine.
type progressReader struct {
	r        io.Reader
	total    int64
	read     int64
	onTick   func(done, total int64)
	mu       sync.Mutex
	lastRead time.Time
}

// Read counts bytes and emits progress (when an onTick callback is set) as the stream flows.
func (pr *progressReader) Read(b []byte) (int, error) {
	n, err := pr.r.Read(b)
	if n > 0 {
		pr.read += int64(n)
		pr.mu.Lock()
		pr.lastRead = time.Now()
		pr.mu.Unlock()
		if pr.onTick != nil {
			pr.onTick(pr.read, pr.total)
		}
	}
	return n, err
}

// sinceLastRead reports how long it has been since the last non-zero read (for the watchdog).
func (pr *progressReader) sinceLastRead(now time.Time) time.Duration {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	return now.Sub(pr.lastRead)
}

// startStallWatchdog launches a goroutine that calls cancel() if pr sees no byte progress for
// window (WR-01). It returns a stop func the caller must invoke once io.Copy returns. The
// watchdog primes lastRead so the first window is measured from stream start.
func startStallWatchdog(pr *progressReader, window time.Duration, cancel context.CancelFunc) func() {
	pr.mu.Lock()
	pr.lastRead = time.Now()
	pr.mu.Unlock()

	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }

	go func() {
		ticker := time.NewTicker(window / 2)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case t := <-ticker.C:
				if pr.sinceLastRead(t) >= window {
					cancel()
					return
				}
			}
		}
	}()
	return stop
}
