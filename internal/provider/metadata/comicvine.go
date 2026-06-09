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
	issueFieldList  = "id,issue_number,name,cover_date,store_date,image,date_last_updated"
	searchFieldList = "id,name,start_year,count_of_issues,description,publisher,image"
)

// ComicVineProvider is the real ComicVine HTTP MetadataProvider implementation.
type ComicVineProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// Option configures a ComicVineProvider.
type Option func(*ComicVineProvider)

// WithBaseURL overrides the ComicVine API base URL (used by tests).
func WithBaseURL(u string) Option { return func(p *ComicVineProvider) { p.baseURL = u } }

// WithHTTPClient overrides the HTTP client (used by tests).
func WithHTTPClient(c *http.Client) Option { return func(p *ComicVineProvider) { p.client = c } }

// NewComicVineProvider builds a ComicVine provider. The API key is held for request
// signing and is never logged.
func NewComicVineProvider(apiKey string, opts ...Option) *ComicVineProvider {
	p := &ComicVineProvider{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		client:  http.DefaultClient,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

var _ MetadataProvider = (*ComicVineProvider)(nil)

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
	ID          int64    `json:"id"`
	IssueNumber string   `json:"issue_number"`
	Name        string   `json:"name"`
	CoverDate   string   `json:"cover_date"`
	StoreDate   string   `json:"store_date"`
	Image       *cvImage `json:"image"`
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
	q.Set("api_key", p.apiKey)
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
		issues = append(issues, IssueDetail{
			ComicvineIssueID: i.ID,
			IssueNumber:      i.IssueNumber,
			Title:            i.Name,
			CoverDate:        i.CoverDate,
			StoreDate:        i.StoreDate,
			CoverURL:         imageURL(i.Image),
		})
	}

	hasMore := offset+len(raw) < env.NumberOfTotalResults
	return issues, hasMore, body, nil
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
