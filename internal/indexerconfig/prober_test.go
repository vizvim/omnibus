package indexerconfig_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/indexerconfig"
	"github.com/vizvim/omnibus/internal/repository"
)

func TestHTTPProberNewznab200IsConnected(t *testing.T) {
	t.Parallel()
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<caps></caps>`))
	}))
	t.Cleanup(srv.Close)

	p := indexerconfig.NewHTTPProber(indexerconfig.WithProberHTTPClient(srv.Client()))
	res := p.Probe(context.Background(), repository.IndexerRow{
		Kind: indexerconfig.KindNewznab, BaseURL: srv.URL, APIKey: "k",
	})
	require.True(t, res.OK)
	require.Equal(t, "connected", res.Detail)
	require.Contains(t, gotQuery, "t=caps", "newznab probe must use the lightweight caps query")
	require.Contains(t, gotQuery, "apikey=k", "the probe must carry the api_key")
}

// TestHTTPProberNewznab200APIErrorIsFailure reproduces the Test-button false
// positive: a newznab indexer returns HTTP 200 with a well-formed <error/> body
// for a bad API key. The probe must report OK:false with the server's own
// description, not a false "connected".
func TestHTTPProberNewznab200APIErrorIsFailure(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<error code="100" description="Incorrect user credentials"/>`))
	}))
	t.Cleanup(srv.Close)

	p := indexerconfig.NewHTTPProber(indexerconfig.WithProberHTTPClient(srv.Client()))
	res := p.Probe(context.Background(), repository.IndexerRow{
		Kind: indexerconfig.KindNewznab, BaseURL: srv.URL, APIKey: "bad-key",
	})
	require.False(t, res.OK)
	require.Contains(t, res.Detail, "Incorrect user credentials")
	require.Contains(t, res.Detail, "100")
}

// TestHTTPProberNewznab200APIErrorNoDescription covers the edge case of an
// <error code="900"/> body with no description — the code alone still fails.
func TestHTTPProberNewznab200APIErrorNoDescription(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<error code="900"/>`))
	}))
	t.Cleanup(srv.Close)

	p := indexerconfig.NewHTTPProber(indexerconfig.WithProberHTTPClient(srv.Client()))
	res := p.Probe(context.Background(), repository.IndexerRow{Kind: indexerconfig.KindNewznab, BaseURL: srv.URL})
	require.False(t, res.OK)
	require.Contains(t, res.Detail, "newznab error 900")
}

func TestHTTPProberNewznab401IsUnauthorized(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	p := indexerconfig.NewHTTPProber(indexerconfig.WithProberHTTPClient(srv.Client()))
	res := p.Probe(context.Background(), repository.IndexerRow{Kind: indexerconfig.KindNewznab, BaseURL: srv.URL})
	require.False(t, res.OK)
	require.Contains(t, strings.ToLower(res.Detail), "unauthorized")
}

// TestHTTPProberGetComicsIsUnknownKind asserts a legacy kind=getcomics row no longer has
// a probe branch (05-07): GetComics is a built-in source, not an indexer kind, so it
// falls through to the "unknown indexer kind" default.
func TestHTTPProberGetComicsIsUnknownKind(t *testing.T) {
	t.Parallel()
	p := indexerconfig.NewHTTPProber()
	res := p.Probe(context.Background(), repository.IndexerRow{Kind: "getcomics", BaseURL: "http://x.test"})
	require.False(t, res.OK)
	require.Equal(t, "unknown indexer kind", res.Detail)
}

func TestHTTPProberSchemeLessBaseURLProbes(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<caps></caps>`))
	}))
	t.Cleanup(srv.Close)

	schemeless := strings.TrimPrefix(srv.URL, "http://")
	p := indexerconfig.NewHTTPProber(indexerconfig.WithProberHTTPClient(srv.Client()))
	res := p.Probe(context.Background(), repository.IndexerRow{Kind: indexerconfig.KindNewznab, BaseURL: schemeless})
	require.True(t, res.OK, "a scheme-less stored URL must still probe successfully")
}

func TestHTTPProberNewznabNon2xxIsStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	p := indexerconfig.NewHTTPProber(indexerconfig.WithProberHTTPClient(srv.Client()))
	res := p.Probe(context.Background(), repository.IndexerRow{Kind: indexerconfig.KindNewznab, BaseURL: srv.URL})
	require.False(t, res.OK)
	require.Contains(t, res.Detail, "status 500")
}

func TestHTTPProberNewznab200NonXMLIsUnexpected(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body>login</body>`)) // not well-formed caps XML
	}))
	t.Cleanup(srv.Close)

	p := indexerconfig.NewHTTPProber(indexerconfig.WithProberHTTPClient(srv.Client()))
	res := p.Probe(context.Background(), repository.IndexerRow{Kind: indexerconfig.KindNewznab, BaseURL: srv.URL})
	require.False(t, res.OK)
	require.Equal(t, "unexpected response", res.Detail)
}

func TestHTTPProberUnknownKind(t *testing.T) {
	t.Parallel()
	p := indexerconfig.NewHTTPProber()
	res := p.Probe(context.Background(), repository.IndexerRow{Kind: "mystery", BaseURL: "http://x.test"})
	require.False(t, res.OK)
	require.Equal(t, "unknown indexer kind", res.Detail)
}

func TestHTTPProberUnreachableHostIsConcise(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := srv.URL
	srv.Close() // close immediately so the address refuses connections

	p := indexerconfig.NewHTTPProber(indexerconfig.WithProberHTTPClient(http.DefaultClient))
	res := p.Probe(context.Background(), repository.IndexerRow{Kind: indexerconfig.KindNewznab, BaseURL: deadURL})
	require.False(t, res.OK)
	require.NotEmpty(t, res.Detail)
	require.NotContains(t, res.Detail, deadURL, "the detail must not leak the raw upstream error/url")
}
