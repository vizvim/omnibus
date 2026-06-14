package metadata

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
)

// Sentinel errors callers can match on.
var (
	// ErrProviderUnavailable is returned when ComicVine responds non-200 or signals a
	// rate-limit/ban (so callers back off rather than parse a bad body).
	ErrProviderUnavailable = errors.New("comicvine provider unavailable")
	// ErrInvalidCoverURL is returned when a cover URL fails the SSRF guard.
	ErrInvalidCoverURL = errors.New("invalid cover URL")
)

const (
	defaultBaseURL = "https://comicvine.gamespot.com/api"
	// coverHostSuffix is the only host GetCover will fetch from (SSRF guard).
	coverHostSuffix = "comicvine.gamespot.com"
	// volumeFieldList / issueFieldList request only the fields we use (fewer bytes,
	// fewer chances to trip ComicVine's per-resource budget).
	volumeFieldList = "id,name,start_year,count_of_issues,description,publisher,image,date_last_updated"
	issueFieldList  = "id,issue_number,name,cover_date,store_date,image,description,deck,person_credits,date_last_updated"
	searchFieldList = "id,name,start_year,count_of_issues,description,publisher,image"
)

// ComicVineProvider is the real ComicVine HTTP MetadataProvider implementation.
type ComicVineProvider struct {
	// keyFunc resolves the ComicVine API key per request (typically straight from the DB), so
	// a key saved via MetadataProviderService takes effect on the next search with no restart
	// and no in-memory cache to drift. The key is read only here and is never logged.
	keyFunc func(context.Context) string
	baseURL string
	client  *http.Client
}

// Option configures a ComicVineProvider.
type Option func(*ComicVineProvider)

// WithBaseURL overrides the ComicVine API base URL (used by tests).
func WithBaseURL(u string) Option { return func(p *ComicVineProvider) { p.baseURL = u } }

// WithHTTPClient overrides the HTTP client (used by tests).
func WithHTTPClient(c *http.Client) Option { return func(p *ComicVineProvider) { p.client = c } }

// NewComicVineProvider builds a ComicVine provider from a static API key. The key is wrapped
// in a constant resolver so existing callers/tests keep their behavior. It is held for
// request signing and is never logged.
func NewComicVineProvider(apiKey string, opts ...Option) *ComicVineProvider {
	return NewComicVineProviderWithKeyFunc(func(context.Context) string { return apiKey }, opts...)
}

// NewComicVineProviderWithKeyFunc builds a ComicVine provider that resolves its API key on
// each request via keyFunc. This lets the key be DB-backed and runtime-editable: a key saved
// via MetadataProviderService takes effect on the next search with no restart.
func NewComicVineProviderWithKeyFunc(keyFunc func(context.Context) string, opts ...Option) *ComicVineProvider {
	if keyFunc == nil {
		keyFunc = func(context.Context) string { return "" }
	}
	p := &ComicVineProvider{
		keyFunc: keyFunc,
		baseURL: defaultBaseURL,
		client:  http.DefaultClient,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

var _ MetadataProvider = (*ComicVineProvider)(nil)

// Probe validates a ComicVine API key against the live API by issuing a minimal authenticated
// request. A bad key (or unreachable CV) maps to (false, concise detail); a good key maps to
// (true, "connected"). It NEVER returns a Go error and never echoes the key in detail — it is
// the metadataprovider.MetadataProber the app injects. A blank apiKey falls back to the live,
// DB-resolved key (via keyFunc), so the service layer never has to read the stored key itself.
func (p *ComicVineProvider) Probe(ctx context.Context, apiKey string) (ok bool, detail string) {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		// No explicit key supplied: probe the currently stored/resolved key.
		key = strings.TrimSpace(p.keyFunc(ctx))
	}
	if key == "" {
		return false, "not configured"
	}
	// Issue a tiny authenticated request (limit=1 search) using a one-shot provider that
	// resolves the chosen key, so the live provider's resolver is untouched.
	probe := NewComicVineProviderWithKeyFunc(func(context.Context) string { return key },
		WithBaseURL(p.baseURL), WithHTTPClient(p.client))
	q := url.Values{}
	q.Set("query", "batman")
	q.Set("resources", "volume")
	q.Set("field_list", "id")
	q.Set("limit", "1")
	if _, err := probe.do(ctx, "/search/", q); err != nil {
		if errors.Is(err, ErrProviderUnavailable) {
			return false, "unauthorized or unreachable (check API key)"
		}
		return false, "probe failed"
	}
	return true, "connected"
}

// cvImage models the ComicVine image object; we pick the largest available URL.
type cvImage struct {
	SuperURL string `json:"super_url"`
	ThumbURL string `json:"thumb_url"`
	IconURL  string `json:"icon_url"`
}

func (i cvImage) best() string {
	switch {
	case i.SuperURL != "":
		return i.SuperURL
	case i.ThumbURL != "":
		return i.ThumbURL
	default:
		return i.IconURL
	}
}

type cvPublisher struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type cvVolume struct {
	ID              int64        `json:"id"`
	Name            string       `json:"name"`
	StartYear       string       `json:"start_year"`
	CountOfIssues   int32        `json:"count_of_issues"`
	Description     string       `json:"description"`
	DateLastUpdated string       `json:"date_last_updated"`
	Publisher       *cvPublisher `json:"publisher"`
	Image           *cvImage     `json:"image"`
}

type cvIssue struct {
	ID              int64            `json:"id"`
	IssueNumber     string           `json:"issue_number"`
	Name            string           `json:"name"`
	CoverDate       string           `json:"cover_date"`
	StoreDate       string           `json:"store_date"`
	Image           *cvImage         `json:"image"`
	Description     string           `json:"description"`
	Deck            string           `json:"deck"`
	DateLastUpdated string           `json:"date_last_updated"`
	PersonCredits   []cvPersonCredit `json:"person_credits"`
}

// cvPersonCredit is one ComicVine creator credit. Role is a comma-separated list of
// roles (e.g. "writer, cover") which we split into one Credit per role.
type cvPersonCredit struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// cvEnvelope is the common ComicVine response envelope. Results is decoded
// separately per endpoint (object for volume, array for search/issues).
type cvEnvelope struct {
	StatusCode           int             `json:"status_code"`
	Error                string          `json:"error"`
	NumberOfTotalResults int             `json:"number_of_total_results"`
	Limit                int             `json:"limit"`
	Offset               int             `json:"offset"`
	Results              json.RawMessage `json:"results"`
}

func parseYear(s string) int32 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 32)
	if err != nil {
		return 0
	}
	return int32(n)
}

// do performs a GET against the CV API path with the given query, returning the body
// or a typed error on non-200 / CV error status.
func (p *ComicVineProvider) do(ctx context.Context, path string, q url.Values) ([]byte, error) {
	q.Set("api_key", p.keyFunc(ctx))
	q.Set("format", "json")

	endpoint := strings.TrimRight(p.baseURL, "/") + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrProviderUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: http %d", ErrProviderUnavailable, resp.StatusCode)
	}

	// CV signals success with status_code == 1; anything else is an error/ban body.
	var env cvEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode envelope: %w", err)
	}
	if env.StatusCode != 1 {
		return nil, fmt.Errorf("%w: cv status_code %d (%s)", ErrProviderUnavailable, env.StatusCode, env.Error)
	}
	return body, nil
}

// SearchSeries queries the CV search endpoint, filtered to volumes.
func (p *ComicVineProvider) SearchSeries(ctx context.Context, query string) ([]SeriesResult, error) {
	q := url.Values{}
	q.Set("query", query)
	q.Set("resources", "volume")
	q.Set("field_list", searchFieldList)

	body, err := p.do(ctx, "/search/", q)
	if err != nil {
		return nil, err
	}

	var env cvEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode search: %w", err)
	}
	var vols []cvVolume
	if err := json.Unmarshal(env.Results, &vols); err != nil {
		return nil, fmt.Errorf("decode search results: %w", err)
	}

	out := make([]SeriesResult, 0, len(vols))
	for _, v := range vols {
		out = append(out, SeriesResult{
			ComicvineVolumeID: v.ID,
			Name:              v.Name,
			StartYear:         parseYear(v.StartYear),
			Publisher:         publisherName(v.Publisher),
			CountOfIssues:     v.CountOfIssues,
			CoverURL:          imageURL(v.Image),
			Description:       v.Description,
		})
	}
	return out, nil
}

// GetVolume fetches a single volume's detail and returns the raw payload for caching.
func (p *ComicVineProvider) GetVolume(ctx context.Context, volumeID int64) (VolumeDetail, []byte, error) {
	q := url.Values{}
	q.Set("field_list", volumeFieldList)

	path := fmt.Sprintf("/volume/4050-%d/", volumeID)
	body, err := p.do(ctx, path, q)
	if err != nil {
		return VolumeDetail{}, nil, err
	}

	var env cvEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return VolumeDetail{}, nil, fmt.Errorf("decode volume: %w", err)
	}
	var v cvVolume
	if err := json.Unmarshal(env.Results, &v); err != nil {
		return VolumeDetail{}, nil, fmt.Errorf("decode volume results: %w", err)
	}

	detail := VolumeDetail{
		ComicvineVolumeID: v.ID,
		Name:              v.Name,
		StartYear:         parseYear(v.StartYear),
		Description:       v.Description,
		CountOfIssues:     v.CountOfIssues,
		CoverURL:          imageURL(v.Image),
		DateLastUpdated:   v.DateLastUpdated,
	}
	if v.Publisher != nil {
		detail.Publisher = PublisherRef{ComicvineID: v.Publisher.ID, Name: v.Publisher.Name}
	}
	return detail, body, nil
}

// ListIssues fetches one page (100 issues) for a volume at the given offset.
func (p *ComicVineProvider) ListIssues(ctx context.Context, volumeID int64, offset int) ([]IssueDetail, bool, []byte, error) {
	q := url.Values{}
	q.Set("filter", fmt.Sprintf("volume:%d", volumeID))
	q.Set("field_list", issueFieldList)
	q.Set("offset", strconv.Itoa(offset))
	q.Set("limit", strconv.Itoa(pageSize))

	body, err := p.do(ctx, "/issues/", q)
	if err != nil {
		return nil, false, nil, err
	}

	var env cvEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, false, nil, fmt.Errorf("decode issues: %w", err)
	}
	var raw []cvIssue
	if err := json.Unmarshal(env.Results, &raw); err != nil {
		return nil, false, nil, fmt.Errorf("decode issue results: %w", err)
	}

	issues := make([]IssueDetail, 0, len(raw))
	for _, i := range raw {
		issues = append(issues, issueDetailFromCV(i))
	}

	hasMore := offset+len(raw) < env.NumberOfTotalResults
	return issues, hasMore, body, nil
}

// GetIssue fetches a single issue's DETAIL from the per-issue resource endpoint
// (/issue/4000-{id}/). This is the only endpoint that returns person_credits (the
// /issues/ LIST endpoint omits them even when requested), so it is the source for
// populating an issue's creator credits. The envelope's results is a SINGLE object
// here (not an array), but for robustness this decodes either shape.
func (p *ComicVineProvider) GetIssue(ctx context.Context, cvIssueID int64) (IssueDetail, error) {
	q := url.Values{}
	q.Set("field_list", issueFieldList)

	// ComicVine's issue resource prefix is 4000-.
	path := fmt.Sprintf("/issue/4000-%d/", cvIssueID)
	body, err := p.do(ctx, path, q)
	if err != nil {
		return IssueDetail{}, err
	}

	var env cvEnvelope
	if uerr := json.Unmarshal(body, &env); uerr != nil {
		return IssueDetail{}, fmt.Errorf("decode issue: %w", uerr)
	}
	i, err := decodeSingleIssue(env.Results)
	if err != nil {
		return IssueDetail{}, err
	}
	return issueDetailFromCV(i), nil
}

// decodeSingleIssue decodes a CV issue results payload that may be either a single
// object (the /issue/ DETAIL endpoint) or a one-element array (defensive), returning
// the issue. An empty array yields a zero-value issue with no error.
func decodeSingleIssue(results json.RawMessage) (cvIssue, error) {
	trimmed := strings.TrimSpace(string(results))
	if strings.HasPrefix(trimmed, "[") {
		var arr []cvIssue
		if err := json.Unmarshal(results, &arr); err != nil {
			return cvIssue{}, fmt.Errorf("decode issue results array: %w", err)
		}
		if len(arr) == 0 {
			return cvIssue{}, nil
		}
		return arr[0], nil
	}
	var i cvIssue
	if err := json.Unmarshal(results, &i); err != nil {
		return cvIssue{}, fmt.Errorf("decode issue results object: %w", err)
	}
	return i, nil
}

// issueDetailFromCV maps a decoded ComicVine issue into an IssueDetail, the single
// shared mapping used by both ListIssues and GetIssue.
func issueDetailFromCV(i cvIssue) IssueDetail {
	return IssueDetail{
		ComicvineIssueID: i.ID,
		IssueNumber:      i.IssueNumber,
		Title:            i.Name,
		CoverDate:        i.CoverDate,
		StoreDate:        i.StoreDate,
		CoverURL:         imageURL(i.Image),
		Description:      issueDescription(i),
		CVLastUpdated:    i.DateLastUpdated,
		Credits:          issueCredits(i.PersonCredits),
	}
}

// GetCover fetches cover bytes, but only from https ComicVine hosts (SSRF guard).
func (p *ComicVineProvider) GetCover(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidCoverURL, err)
	}
	if u.Scheme != "https" || !isComicVineHost(u.Host) {
		return nil, fmt.Errorf("%w: scheme=%q host=%q", ErrInvalidCoverURL, u.Scheme, u.Host)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build cover request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrProviderUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: cover http %d", ErrProviderUnavailable, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func isComicVineHost(host string) bool {
	h := strings.ToLower(host)
	return h == coverHostSuffix || strings.HasSuffix(h, "."+coverHostSuffix)
}

func publisherName(p *cvPublisher) string {
	if p == nil {
		return ""
	}
	return p.Name
}

func imageURL(i *cvImage) string {
	if i == nil {
		return ""
	}
	return i.best()
}

// issueDescription prefers CV's short "deck" summary when present, falling back to the
// longer (HTML) description field.
func issueDescription(i cvIssue) string {
	if strings.TrimSpace(i.Deck) != "" {
		return i.Deck
	}
	return i.Description
}

// issueCredits flattens ComicVine person_credits into one Credit per role: a credit's
// Role field may carry several comma-separated roles (e.g. "writer, cover"), each of
// which becomes its own Credit. Roles are trimmed and lowercased here; the service
// layer applies synonym normalization. Empty roles and names are skipped.
func issueCredits(pcs []cvPersonCredit) []Credit {
	if len(pcs) == 0 {
		return nil
	}
	out := make([]Credit, 0, len(pcs))
	for _, pc := range pcs {
		name := strings.TrimSpace(pc.Name)
		if name == "" {
			continue
		}
		for role := range strings.SplitSeq(pc.Role, ",") {
			role = strings.ToLower(strings.TrimSpace(role))
			if role == "" {
				continue
			}
			out = append(out, Credit{
				Role:              role,
				Name:              name,
				ComicvinePersonID: pc.ID,
			})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// decodeIssueDetail decodes a cached CV-shaped single-issue envelope back into an
// IssueDetail, handling both the object and array results shapes.
func decodeIssueDetail(body []byte) (IssueDetail, error) {
	var env cvEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return IssueDetail{}, fmt.Errorf("decode cached issue: %w", err)
	}
	i, err := decodeSingleIssue(env.Results)
	if err != nil {
		return IssueDetail{}, err
	}
	return issueDetailFromCV(i), nil
}

// encodeIssueEnvelope re-encodes an IssueDetail into the CV-shaped single-issue
// envelope (results as an object) so the gateway can cache then re-read it. Each
// already-flattened Credit becomes one person_credit with a single role; decoding
// re-splits/normalizes idempotently. Description is written to the description field
// (deck is left empty so issueDescription returns it unchanged on read-back).
func encodeIssueEnvelope(d IssueDetail) ([]byte, error) {
	pcs := make([]cvPersonCredit, 0, len(d.Credits))
	for _, c := range d.Credits {
		pcs = append(pcs, cvPersonCredit{
			ID:   c.ComicvinePersonID,
			Name: c.Name,
			Role: c.Role,
		})
	}
	iss := cvIssue{
		ID:              d.ComicvineIssueID,
		IssueNumber:     d.IssueNumber,
		Name:            d.Title,
		CoverDate:       d.CoverDate,
		StoreDate:       d.StoreDate,
		Image:           &cvImage{SuperURL: d.CoverURL},
		Description:     d.Description,
		DateLastUpdated: d.CVLastUpdated,
		PersonCredits:   pcs,
	}
	env := struct {
		StatusCode int     `json:"status_code"`
		Results    cvIssue `json:"results"`
	}{StatusCode: 1, Results: iss}
	b, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("encode issue envelope: %w", err)
	}
	return b, nil
}

// encodeSearchEnvelope re-encodes normalized search results into the CV-shaped
// envelope decodeSearch consumes, so the gateway can cache then re-read search hits.
func encodeSearchEnvelope(results []SeriesResult) ([]byte, error) {
	vols := make([]cvVolume, 0, len(results))
	for _, r := range results {
		vols = append(vols, cvVolume{
			ID:            r.ComicvineVolumeID,
			Name:          r.Name,
			StartYear:     strconv.Itoa(int(r.StartYear)),
			CountOfIssues: r.CountOfIssues,
			Description:   r.Description,
			Publisher:     &cvPublisher{Name: r.Publisher},
			Image:         &cvImage{SuperURL: r.CoverURL},
		})
	}
	env := struct {
		StatusCode int        `json:"status_code"`
		Results    []cvVolume `json:"results"`
	}{StatusCode: 1, Results: vols}
	b, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("encode search envelope: %w", err)
	}
	return b, nil
}
