package download

import (
	"context"
	"errors"
	"strings"
)

// GetComicsDDLProvider initiates a GetComics direct-download (DL-02). This phase only
// needs the fetch *initiated* and a client reference returned; full mirror-resolution +
// completion tracking is Phase 5. The clientRef is the post/mirror URL the Phase-5
// tracker will resolve and poll.
type GetComicsDDLProvider struct {
	baseURL string
}

// NewGetComicsDDLProvider builds a GetComics DDL provider.
func NewGetComicsDDLProvider(baseURL string) *GetComicsDDLProvider {
	return &GetComicsDDLProvider{baseURL: strings.TrimRight(baseURL, "/")}
}

var _ DownloadProvider = (*GetComicsDDLProvider)(nil)

// Kind reports the provider type.
func (p *GetComicsDDLProvider) Kind() string { return "getcomics" }

// Submit records the DDL handoff and returns the post URL as the client reference. The
// real mirror resolution + byte fetch is Phase 5; here we only validate the request and
// return a stable reference so the downloads row + snatched event can be written.
func (p *GetComicsDDLProvider) Submit(_ context.Context, req GrabRequest) (string, error) {
	if req.DownloadURL == "" {
		return "", errors.New("getcomics ddl: empty download url")
	}
	// The post URL is the stable client reference Phase 5 resolves to a working mirror.
	return req.DownloadURL, nil
}
