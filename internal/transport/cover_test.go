package transport_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/vizvim/omnibus/internal/transport"
)

// fakeCoverStore is an in-test CoverStore backed by a map keyed on entityType+"/"+id.
type fakeCoverStore struct {
	blobs map[string]coverEntry
}

type coverEntry struct {
	image       []byte
	contentType string
}

func (f *fakeCoverStore) Get(_ context.Context, entityType string, id int64) ([]byte, string, bool, error) {
	e, ok := f.blobs[fmt.Sprintf("%s/%d", entityType, id)]
	if !ok {
		return nil, "", false, nil
	}
	return e.image, e.contentType, true, nil
}

func TestCoverHandler(t *testing.T) {
	t.Parallel()
	store := &fakeCoverStore{blobs: map[string]coverEntry{
		"series/1": {image: []byte("\xff\xd8\xffIMG"), contentType: "image/png"},
	}}
	srv := httptest.NewServer(transport.NewCoverHandler(store))
	t.Cleanup(srv.Close)

	get := func(path string) (*http.Response, []byte) {
		t.Helper()
		resp, err := srv.Client().Get(srv.URL + path)
		require.NoError(t, err)
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp, body
	}

	// (a) Stored cover served with stored bytes + content-type.
	resp, body := get("/covers/series/1.jpg")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "image/png", resp.Header.Get("Content-Type"))
	require.Equal(t, []byte("\xff\xd8\xffIMG"), body)

	// (b) Absent cover -> 404.
	resp, _ = get("/covers/series/999.jpg")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	// (c) Bad kind -> 400.
	resp, _ = get("/covers/foo/1.jpg")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// (d) Non-numeric id -> 400.
	resp, _ = get("/covers/series/abc.jpg")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// (e) Traversal-style path never reaches a file and never returns 200.
	resp, body = get("/covers/series/..%2f..%2fsecret.txt")
	require.NotEqual(t, http.StatusOK, resp.StatusCode)
	require.NotContains(t, string(body), "TOPSECRET")
}
