package download_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/provider/download"
)

func TestSABnzbdSubmitParsesNzoID(t *testing.T) {
	t.Parallel()
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":true,"nzo_ids":["SABnzbd_nzo_abc"]}`))
	}))
	t.Cleanup(srv.Close)

	p := download.NewSABnzbdProvider(srv.URL, "sab-key", "comics", download.WithSABnzbdHTTPClient(srv.Client()))
	ref, err := p.Submit(context.Background(), download.GrabRequest{
		Title:       "Saga #7",
		DownloadURL: "http://nzb/abc.nzb",
		ReleaseKey:  "abc",
	})
	require.NoError(t, err)
	require.Equal(t, "SABnzbd_nzo_abc", ref)

	require.Contains(t, gotQuery, "mode=addurl")
	require.Contains(t, gotQuery, "apikey=sab-key")
	require.Contains(t, gotQuery, "cat=comics")
	require.Equal(t, "sabnzbd", p.Kind())
}

func TestSABnzbdSubmitStatusFalseIsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":false,"error":"API Key Incorrect"}`))
	}))
	t.Cleanup(srv.Close)

	p := download.NewSABnzbdProvider(srv.URL, "bad", "", download.WithSABnzbdHTTPClient(srv.Client()))
	_, err := p.Submit(context.Background(), download.GrabRequest{DownloadURL: "http://nzb/x.nzb"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "API Key Incorrect")
}

func TestSABnzbdNotConfiguredFailsLoudly(t *testing.T) {
	t.Parallel()
	p := download.NewSABnzbdProvider("", "", "")
	_, err := p.Submit(context.Background(), download.GrabRequest{DownloadURL: "http://nzb/x.nzb"})
	require.ErrorIs(t, err, download.ErrSabnzbdNotConfigured)
}

func TestGetComicsDDLSubmitReturnsPostURL(t *testing.T) {
	t.Parallel()
	p := download.NewGetComicsDDLProvider("https://getcomics.org")
	ref, err := p.Submit(context.Background(), download.GrabRequest{
		DownloadURL: "https://getcomics.org/comic/saga-7/",
		ReleaseKey:  "https://getcomics.org/comic/saga-7/",
	})
	require.NoError(t, err)
	require.Equal(t, "https://getcomics.org/comic/saga-7/", ref)
	require.Equal(t, "getcomics", p.Kind())
}

func TestFakeProviderRecordsRequest(t *testing.T) {
	t.Parallel()
	f := download.NewFakeProvider("sabnzbd", "ref-1")
	ref, err := f.Submit(context.Background(), download.GrabRequest{Title: "T", ReleaseKey: "rk"})
	require.NoError(t, err)
	require.Equal(t, "ref-1", ref)
	require.Equal(t, "rk", f.LastRequest.ReleaseKey)
	require.Equal(t, 1, f.Calls)
}
