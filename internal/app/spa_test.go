package app_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/app"
)

// assembleMux mirrors newServer's mount order: /api and /covers first, then the SPA
// catch-all at "/". It lets the tests prove the catch-all never shadows the prefixed
// handlers without standing up the full server (DB, jobs, etc.).
func assembleMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("api-sentinel"))
	})
	mux.HandleFunc("/covers/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("covers-sentinel"))
	})
	mux.Handle("/", app.NewSPAHandler())
	return mux
}

func TestSPAServesIndexAtRoot(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	app.NewSPAHandler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, strings.ToLower(body), "<!doctype html>",
		"GET / must return the HTML SPA shell")
	require.Contains(t, body, `id="root"`, "the SPA shell mounts on #root")
}

func TestSPADeepRouteFallsBackToIndex(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	// A deep client route that is not an embedded asset must return the index.html shell
	// so client-side routing can take over (SPA fallback).
	req := httptest.NewRequest(http.MethodGet, "/settings", http.NoBody)
	app.NewSPAHandler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, strings.ToLower(rec.Body.String()), "<!doctype html>",
		"a deep client route must serve the index.html fallback, not 404")
}

func TestSPADoesNotShadowAPI(t *testing.T) {
	t.Parallel()

	mux := assembleMux()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/omnibus.v1.SeriesService/ListSeries", http.NoBody)
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "api-sentinel", rec.Body.String(),
		"the / catch-all must not intercept /api paths")
}

func TestSPADoesNotShadowCovers(t *testing.T) {
	t.Parallel()

	mux := assembleMux()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/covers/series/1.jpg", http.NoBody)
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "covers-sentinel", rec.Body.String(),
		"the / catch-all must not intercept /covers paths")
}

func TestSPARejectsNonGet(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/settings", http.NoBody)
	app.NewSPAHandler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code,
		"the SPA handler serves GET/HEAD only")
}
