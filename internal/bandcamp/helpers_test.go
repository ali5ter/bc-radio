package bandcamp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bc-radio/internal/contract"
)

// fixtureServer serves the testdata corpus through one origin, with
// artist hosts as path prefixes — the shape the client's test-origin
// BaseURL mode produces. It also serves the synthetic tralbum and
// following pages registered on it.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	fixture := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(b)
		}
	}
	// The corpus was captured under these account/host names.
	mux.Handle("/deerhoof/following/artists_and_labels", fixture("deerhoof-following.html"))
	mux.Handle("/mrtw/music", fixture("mrtw-music.html"))
	mux.Handle("/mrtw/album/bad-street", fixture("mrtw-album.html"))
	mux.Handle("/mrtwinsister/album/bad-street", fixture("mrtw-album.html"))
	mux.Handle("/deerhoof/music", fixture("deerhoof-sub-music.html"))
	mux.Handle("/deerhoof/album/actually-you-can", fixture("album-actually-you-can.html"))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// testClient builds a Client pointed at srv with a throwaway cache dir
// and millisecond retry backoff.
func testClient(t *testing.T, srv *httptest.Server, username string) *Client {
	t.Helper()
	c, err := New(Options{
		Username: username,
		BaseURL:  srv.URL,
		CacheDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.backoff = time.Millisecond
	return c
}

// countingHandler wraps a handler with a hit counter.
func countingHandler(handler http.HandlerFunc, hits *int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*hits++
		handler(w, r)
	}
}

// tralbumPage renders an album page embedding the given JSON as the
// &quot;-escaped data-tralbum attribute, the shape Bandcamp serves.
func tralbumPage(json string) string {
	esc := strings.ReplaceAll(json, `"`, `&quot;`)
	return `<html><head></head><body>` +
		`<script type="text/javascript" src="https://s4.bcbits.com/client-bundle/1/trackpipe/tralbum_head-x.js" data-tralbum="` + esc + `"></script>` +
		`</body></html>`
}

// followingPage renders a following page embedding the given JSON as the
// &quot;-escaped data-blob attribute of the pagedata div.
func followingPage(json string) string {
	esc := strings.ReplaceAll(json, `"`, `&quot;`)
	return `<html><body><div id="pagedata" data-blob="` + esc + `"></div></body></html>`
}

// artistWith builds a contract.Artist for test routing.
func artistWith(sub, custom string) contract.Artist {
	return contract.Artist{Subdomain: sub, CustomDomain: custom}
}

// synthTrack builds a self-sufficient track for Open tests against a
// test-origin server.
func synthTrack(albumID int64, host, slug, trackID string) contract.Track {
	return contract.Track{
		ID:        trackID,
		Title:     "Synth",
		AlbumID:   albumID,
		AlbumSlug: slug,
		Subdomain: host,
	}
}
