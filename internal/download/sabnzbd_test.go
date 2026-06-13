package download_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/download"
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

func TestSABnzbdTestConnectedOnVersion200(t *testing.T) {
	t.Parallel()
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"4.1.0"}`))
	}))
	t.Cleanup(srv.Close)

	p := download.NewSABnzbdProvider(srv.URL, "sab-key", "comics", download.WithSABnzbdHTTPClient(srv.Client()))
	ok, detail := p.Test(context.Background())
	require.True(t, ok)
	require.Equal(t, "connected", detail)
	require.Contains(t, gotQuery, "mode=version", "the probe must use the lightweight version endpoint")
}

func TestSABnzbdTest401IsUnauthorized(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	p := download.NewSABnzbdProvider(srv.URL, "bad", "", download.WithSABnzbdHTTPClient(srv.Client()))
	ok, detail := p.Test(context.Background())
	require.False(t, ok)
	require.Contains(t, detail, "unauthorized")
}

func TestSABnzbdTestBadKeyJSONErrorIsUnauthorized(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// SAB returns a 200 with an {"error": ...} body on a bad key.
		_, _ = w.Write([]byte(`{"error":"API Key Incorrect"}`))
	}))
	t.Cleanup(srv.Close)

	p := download.NewSABnzbdProvider(srv.URL, "bad", "", download.WithSABnzbdHTTPClient(srv.Client()))
	ok, detail := p.Test(context.Background())
	require.False(t, ok)
	require.Contains(t, detail, "unauthorized")
}

func TestSABnzbdTestEmptyURLIsNotConfigured(t *testing.T) {
	t.Parallel()
	p := download.NewSABnzbdProvider("", "", "")
	ok, detail := p.Test(context.Background())
	require.False(t, ok)
	require.Equal(t, "not configured", detail)
}

func TestSABnzbdTestUnreachableIsConcise(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := srv.URL
	srv.Close() // refuse connections

	p := download.NewSABnzbdProvider(deadURL, "k", "")
	ok, detail := p.Test(context.Background())
	require.False(t, ok)
	require.NotEmpty(t, detail)
	require.NotContains(t, detail, deadURL, "detail must not leak the raw upstream error/url")
}

// sabPollServer stands up an httptest server that answers mode=queue with queueJSON and
// mode=history with histJSON, and returns a SAB provider wired to it.
func sabPollServer(t *testing.T, queueJSON, histJSON string) *download.SABnzbdProvider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("mode") {
		case "queue":
			_, _ = w.Write([]byte(queueJSON))
		case "history":
			_, _ = w.Write([]byte(histJSON))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	return download.NewSABnzbdProvider(srv.URL, "k", "comics", download.WithSABnzbdHTTPClient(srv.Client()))
}

func TestSABParse(t *testing.T) {
	t.Parallel()

	t.Run("in-progress parses string numerics to a percentage", func(t *testing.T) {
		t.Parallel()
		// mb=123.4, mbleft=60 -> ~51.4% done. Numerics arrive as JSON strings (Pitfall 2).
		queue := `{"queue":{"slots":[{"nzo_id":"nzo_1","status":"Downloading","mb":"123.4","mbleft":"60"}]}}`
		p := sabPollServer(t, queue, `{"history":{"slots":[]}}`)
		res, err := p.Poll(context.Background(), "nzo_1")
		require.NoError(t, err)
		require.Equal(t, download.PollInProgress, res.Status)
		require.InDelta(t, 51.38, res.ProgressPct, 0.5)
	})

	t.Run("completed with real storage is terminal completed", func(t *testing.T) {
		t.Parallel()
		hist := `{"history":{"slots":[{"nzo_id":"nzo_2","status":"Completed","storage":"/downloads/Saga 07"}]}}`
		p := sabPollServer(t, `{"queue":{"slots":[]}}`, hist)
		res, err := p.Poll(context.Background(), "nzo_2")
		require.NoError(t, err)
		require.Equal(t, download.PollCompleted, res.Status)
		require.Equal(t, "/downloads/Saga 07", res.StoragePath)
		require.InDelta(t, 100, res.ProgressPct, 0.01)
	})

	t.Run("completed with empty storage is not-ready, not success", func(t *testing.T) {
		t.Parallel()
		// Pitfall 1: SAB reports Completed before the storage path is populated.
		hist := `{"history":{"slots":[{"nzo_id":"nzo_3","status":"Completed","storage":""}]}}`
		p := sabPollServer(t, `{"queue":{"slots":[]}}`, hist)
		res, err := p.Poll(context.Background(), "nzo_3")
		require.NoError(t, err)
		require.Equal(t, download.PollNotReady, res.Status)
		require.Empty(t, res.StoragePath)
	})

	t.Run("transient history status is still in progress", func(t *testing.T) {
		t.Parallel()
		hist := `{"history":{"slots":[{"nzo_id":"nzo_4","status":"Extracting","storage":""}]}}`
		p := sabPollServer(t, `{"queue":{"slots":[]}}`, hist)
		res, err := p.Poll(context.Background(), "nzo_4")
		require.NoError(t, err)
		require.Equal(t, download.PollInProgress, res.Status)
	})

	t.Run("unknown ref is not-ready", func(t *testing.T) {
		t.Parallel()
		p := sabPollServer(t, `{"queue":{"slots":[]}}`, `{"history":{"slots":[]}}`)
		res, err := p.Poll(context.Background(), "nope")
		require.NoError(t, err)
		require.Equal(t, download.PollNotReady, res.Status)
	})

	t.Run("zero mb guards against divide-by-zero", func(t *testing.T) {
		t.Parallel()
		queue := `{"queue":{"slots":[{"nzo_id":"nzo_5","status":"Queued","mb":"0","mbleft":"0"}]}}`
		p := sabPollServer(t, queue, `{"history":{"slots":[]}}`)
		res, err := p.Poll(context.Background(), "nzo_5")
		require.NoError(t, err)
		require.Equal(t, download.PollInProgress, res.Status)
		require.InDelta(t, 0, res.ProgressPct, 0.01)
	})
}

func TestSABPollFailure(t *testing.T) {
	t.Parallel()
	hist := `{"history":{"slots":[{"nzo_id":"nzo_f","status":"Failed","fail_message":"unpack failed: CRC error"}]}}`
	p := sabPollServer(t, `{"queue":{"slots":[]}}`, hist)
	res, err := p.Poll(context.Background(), "nzo_f")
	require.NoError(t, err)
	require.Equal(t, download.PollFailed, res.Status)
	require.Contains(t, res.FailMessage, "CRC error")
}

func TestSABRemoveFromHistory(t *testing.T) {
	t.Parallel()
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":true}`))
	}))
	t.Cleanup(srv.Close)

	p := download.NewSABnzbdProvider(srv.URL, "k", "comics", download.WithSABnzbdHTTPClient(srv.Client()))
	require.NoError(t, p.RemoveFromHistory(context.Background(), "nzo_done"))
	require.Contains(t, gotQuery, "mode=history")
	require.Contains(t, gotQuery, "name=delete")
	require.Contains(t, gotQuery, "value=nzo_done")
	require.NotContains(t, gotQuery, "del_files", "Usenet does not seed; the library file persists (D-05)")
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
