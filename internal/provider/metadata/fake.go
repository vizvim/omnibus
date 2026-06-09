package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FakeProvider is a fixture-backed MetadataProvider for tests and local dev — it does
// no network I/O, so CI needs no ComicVine API key. It parses the same
// CV-shaped fixtures the real provider would receive.
type FakeProvider struct {
	search    []SeriesResult
	volume    VolumeDetail
	volumeRaw []byte
	issues    []IssueDetail
	issuesRaw []byte
	// cover is the byte payload returned by GetCover for any (valid) URL.
	cover []byte
}

var _ MetadataProvider = (*FakeProvider)(nil)

// NewFakeProvider loads the search/volume/issues fixtures from dir.
func NewFakeProvider(dir string) (*FakeProvider, error) {
	f := &FakeProvider{cover: []byte("\xff\xd8\xff")} // minimal JPEG SOI marker

	searchBody, err := os.ReadFile(filepath.Join(dir, "search_same_title.json"))
	if err != nil {
		return nil, fmt.Errorf("read search fixture: %w", err)
	}
	f.search, err = decodeSearch(searchBody)
	if err != nil {
		return nil, err
	}

	volBody, err := os.ReadFile(filepath.Join(dir, "volume_large_series.json"))
	if err != nil {
		return nil, fmt.Errorf("read volume fixture: %w", err)
	}
	f.volumeRaw = volBody
	f.volume, err = decodeVolume(volBody)
	if err != nil {
		return nil, err
	}

	issuesBody, err := os.ReadFile(filepath.Join(dir, "issues_non_integer.json"))
	if err != nil {
		return nil, fmt.Errorf("read issues fixture: %w", err)
	}
	f.issuesRaw = issuesBody
	f.issues, err = decodeIssues(issuesBody)
	if err != nil {
		return nil, err
	}

	return f, nil
}

// SearchSeries returns the fixture search candidates.
func (f *FakeProvider) SearchSeries(_ context.Context, _ string) ([]SeriesResult, error) {
	return f.search, nil
}

// GetVolume returns the fixture volume detail and the original fixture bytes (so the
// gateway caches a realistic raw payload).
func (f *FakeProvider) GetVolume(_ context.Context, _ int64) (VolumeDetail, []byte, error) {
	return f.volume, f.volumeRaw, nil
}

// ListIssues returns the fixture issues on offset 0 and an empty final page otherwise.
func (f *FakeProvider) ListIssues(_ context.Context, _ int64, offset int) ([]IssueDetail, bool, []byte, error) {
	if offset > 0 {
		return nil, false, []byte("[]"), nil
	}
	return f.issues, false, f.issuesRaw, nil
}

// GetCover returns a fixed fake image (no network, no SSRF concern in the fake).
func (f *FakeProvider) GetCover(_ context.Context, _ string) ([]byte, error) {
	return f.cover, nil
}

func decodeSearch(body []byte) ([]SeriesResult, error) {
	var env cvEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode fake search: %w", err)
	}
	var vols []cvVolume
	if err := json.Unmarshal(env.Results, &vols); err != nil {
		return nil, fmt.Errorf("decode fake search results: %w", err)
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

func decodeVolume(body []byte) (VolumeDetail, error) {
	var env cvEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return VolumeDetail{}, fmt.Errorf("decode fake volume: %w", err)
	}
	var v cvVolume
	if err := json.Unmarshal(env.Results, &v); err != nil {
		return VolumeDetail{}, fmt.Errorf("decode fake volume results: %w", err)
	}
	d := VolumeDetail{
		ComicvineVolumeID: v.ID,
		Name:              v.Name,
		StartYear:         parseYear(v.StartYear),
		Description:       v.Description,
		CountOfIssues:     v.CountOfIssues,
		CoverURL:          imageURL(v.Image),
		DateLastUpdated:   v.DateLastUpdated,
	}
	if v.Publisher != nil {
		d.Publisher = PublisherRef{ComicvineID: v.Publisher.ID, Name: v.Publisher.Name}
	}
	return d, nil
}

func decodeIssues(body []byte) ([]IssueDetail, error) {
	var env cvEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode fake issues: %w", err)
	}
	var raw []cvIssue
	if err := json.Unmarshal(env.Results, &raw); err != nil {
		return nil, fmt.Errorf("decode fake issue results: %w", err)
	}
	out := make([]IssueDetail, 0, len(raw))
	for _, i := range raw {
		out = append(out, IssueDetail{
			ComicvineIssueID: i.ID,
			IssueNumber:      i.IssueNumber,
			Title:            i.Name,
			CoverDate:        i.CoverDate,
			StoreDate:        i.StoreDate,
			CoverURL:         imageURL(i.Image),
		})
	}
	return out, nil
}
