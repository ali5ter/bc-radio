package bandcamp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ali5ter/bc-radio/internal/contract"
)

// demoPage renders the synthetic release with the given signed-URL expiry.
func demoPage(srvBase string, expires time.Time) string {
	ts := strconv.FormatInt(expires.Unix(), 10)
	streamURL := srvBase + "/stream/100?ts=" + ts
	return tralbumPage(`{
		"id": 42, "item_type": "album", "artist": "Synth Band",
		"url": "https://fakehost.bandcamp.com/album/demo",
		"current": {"title": "Demo", "band_id": 7, "art_id": 1, "release_date": "01 Jan 2020 00:00:00 GMT"},
		"trackinfo": [
			{"id": 100, "track_id": 100, "title": "One", "track_num": 1, "duration": 10.5, "file": {"mp3-128": "` + streamURL + `"}}
		]
	}`)
}

// singlePage is demoPage for a /track/ release: item_type "track", a
// canonical /track/ URL, one track.
func singlePage(srvBase string, expires time.Time) string {
	ts := strconv.FormatInt(expires.Unix(), 10)
	streamURL := srvBase + "/stream/100?ts=" + ts
	return tralbumPage(`{
		"id": 42, "item_type": "track", "artist": "Synth Band",
		"url": "https://tontrakul.bandcamp.com/track/earn",
		"current": {"title": "Earn", "band_id": 7, "art_id": 1, "release_date": "01 Jan 2020 00:00:00 GMT"},
		"trackinfo": [
			{"id": 100, "track_id": 100, "title": "Earn", "track_num": 1, "duration": 10.5, "file": {"mp3-128": "` + streamURL + `"}}
		]
	}`)
}

// TestOpenSingle pins the single's stream path: a track with TrackSlug
// (and no AlbumSlug) resolves from its /track/ page, not /album/.
func TestOpenSingle(t *testing.T) {
	t.Parallel()
	// The track page is set after the server exists because the signed
	// stream URL embeds the server's own address.
	var page string
	mux := http.NewServeMux()
	mux.HandleFunc("/tontrakul/track/earn", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(page))
	})
	mux.HandleFunc("/stream/100", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("MP3DATA"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	page = singlePage(srv.URL, time.Now().Add(24*time.Hour))

	c := testClient(t, srv, "deerhoof")
	tr := contract.Track{
		ID:        "100",
		Title:     "Earn",
		AlbumID:   42,
		TrackSlug: "earn", // a single: its /track/ page carries the stream URL
		Subdomain: "tontrakul",
	}
	stream, err := c.Open(context.Background(), tr)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer stream.Body.Close()
	body, _ := io.ReadAll(stream.Body)
	if string(body) != "MP3DATA" {
		t.Errorf("body = %q, want MP3DATA", body)
	}
}

func TestOpenHappyPath(t *testing.T) {
	t.Parallel()
	// The album page is set after the server exists because the signed
	// stream URL embeds the server's own address.
	var page string
	mux := http.NewServeMux()
	mux.HandleFunc("/fakehost/album/demo", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(page))
	})
	mux.HandleFunc("/stream/100", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("MP3DATA"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	page = demoPage(srv.URL, time.Now().Add(24*time.Hour))

	c := testClient(t, srv, "deerhoof")
	stream, err := c.Open(context.Background(), synthTrack(42, "fakehost.bandcamp.com", "demo", "100"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer stream.Body.Close()
	body, _ := io.ReadAll(stream.Body)
	if string(body) != "MP3DATA" {
		t.Errorf("body = %q, want MP3DATA", body)
	}
	if stream.ContentLength != int64(len("MP3DATA")) {
		t.Errorf("ContentLength = %d, want %d", stream.ContentLength, len("MP3DATA"))
	}
}

func TestOpenReusesCachedURL(t *testing.T) {
	t.Parallel()
	var page string
	pageHits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/fakehost/album/demo", countingHandler(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(page))
	}, &pageHits))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	page = demoPage(srv.URL, time.Now().Add(24*time.Hour))
	// The stream URL points back at the test server; serve it.
	mux.HandleFunc("/stream/100", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("MP3DATA"))
	})

	c := testClient(t, srv, "deerhoof")
	tr := synthTrack(42, "fakehost.bandcamp.com", "demo", "100")
	for i := 0; i < 2; i++ {
		stream, err := c.Open(context.Background(), tr)
		if err != nil {
			t.Fatalf("Open %d: %v", i, err)
		}
		io.Copy(io.Discard, stream.Body)
		stream.Body.Close()
	}
	if pageHits != 1 {
		t.Errorf("album page hits = %d, want 1 (the signed URL was still valid)", pageHits)
	}
}

func TestOpenRefetchesWhenURLExpired(t *testing.T) {
	t.Parallel()
	var versions []string
	pageHits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/fakehost/album/demo", countingHandler(func(w http.ResponseWriter, _ *http.Request) {
		// countingHandler already incremented pageHits: it is the request number.
		if pageHits-1 < len(versions) {
			w.Write([]byte(versions[pageHits-1]))
		} else {
			w.Write([]byte(versions[len(versions)-1]))
		}
	}, &pageHits))
	mux.HandleFunc("/stream/100", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("MP3DATA"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	// First render expired an hour ago; the forced refetch renders fresh.
	versions = []string{
		demoPage(srv.URL, time.Now().Add(-time.Hour)),
		demoPage(srv.URL, time.Now().Add(24*time.Hour)),
	}

	c := testClient(t, srv, "deerhoof")
	stream, err := c.Open(context.Background(), synthTrack(42, "fakehost.bandcamp.com", "demo", "100"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	io.Copy(io.Discard, stream.Body)
	stream.Body.Close()
	if pageHits != 2 {
		t.Errorf("album page hits = %d, want 2 (cached page's URL expired, forcing a refetch)", pageHits)
	}
}

func TestOpenRefetchesAfterStreamFailure(t *testing.T) {
	t.Parallel()
	var page string
	pageHits := 0
	streamHits := 0
	streamFailed := 0
	const failOnce = 1
	mux := http.NewServeMux()
	mux.HandleFunc("/fakehost/album/demo", countingHandler(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(page))
	}, &pageHits))
	mux.HandleFunc("/stream/100", countingHandler(func(w http.ResponseWriter, _ *http.Request) {
		if streamFailed < failOnce {
			streamFailed++
			http.NotFound(w, nil)
			return
		}
		w.Write([]byte("MP3DATA"))
	}, &streamHits))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	page = demoPage(srv.URL, time.Now().Add(24*time.Hour))

	c := testClient(t, srv, "deerhoof")
	stream, err := c.Open(context.Background(), synthTrack(42, "fakehost.bandcamp.com", "demo", "100"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	body, _ := io.ReadAll(stream.Body)
	stream.Body.Close()
	if string(body) != "MP3DATA" {
		t.Errorf("body = %q, want MP3DATA after retry", body)
	}
	if streamHits != 2 {
		t.Errorf("stream hits = %d, want 2 (one 404, one success)", streamHits)
	}
	if pageHits != 2 {
		t.Errorf("album page hits = %d, want 2 (the failed URL triggered a refetch)", pageHits)
	}
}

func TestOpenRejectsLocationlessTrack(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	c := testClient(t, srv, "deerhoof")
	stream, err := c.Open(context.Background(), contract.Track{Title: "Nowhere"})
	if err == nil {
		t.Fatal("Open: want an error for a track with no location")
	}
	if stream.Body != nil {
		t.Error("Open returned a body alongside an error")
	}
	if !strings.Contains(err.Error(), "no location") {
		t.Errorf("err = %v, want it to say the track has no location", err)
	}
}

func TestOpenRejectsNonNumericID(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	c := testClient(t, srv, "deerhoof")
	tr := synthTrack(42, "fakehost.bandcamp.com", "demo", "not-a-number")
	if _, err := c.Open(context.Background(), tr); err == nil {
		t.Fatal("Open: want an error for a non-numeric track ID")
	}
}

func TestSignedURLExpiry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want time.Time
	}{
		{name: "valid ts", in: "https://t4.bcbits.com/stream/x/mp3-128/1?ts=1900000000&t=y&token=z", want: time.Unix(1900000000, 0)},
		{name: "missing ts", in: "https://t4.bcbits.com/stream/x/mp3-128/1", want: time.Time{}},
		{name: "malformed ts", in: "https://t4.bcbits.com/stream/x/mp3-128/1?ts=soon", want: time.Time{}},
		{name: "unparseable url", in: "https://", want: time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := signedURLExpiry(tt.in); !got.Equal(tt.want) {
				t.Errorf("expiry = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestURLCache(t *testing.T) {
	t.Parallel()
	c := &urlCache{entries: map[urlKey]urlEntry{}}
	future := time.Now().Add(24 * time.Hour)
	past := time.Now().Add(-time.Hour)
	fresh := time.Now().Add(30 * time.Minute) // inside the safety margin

	c.put(urlKey{1, 1}, "https://s/1?ts="+strconv.FormatInt(future.Unix(), 10))
	c.put(urlKey{1, 2}, "https://s/2?ts="+strconv.FormatInt(past.Unix(), 10))
	c.put(urlKey{1, 3}, "https://s/3?ts="+strconv.FormatInt(fresh.Unix(), 10))
	c.put(urlKey{1, 4}, "https://s/4") // no ts: default lifetime

	if _, ok := c.get(urlKey{1, 1}); !ok {
		t.Error("valid future URL not returned")
	}
	if _, ok := c.get(urlKey{1, 2}); ok {
		t.Error("expired URL returned")
	}
	if _, ok := c.get(urlKey{1, 3}); ok {
		t.Error("URL expiring within the safety margin returned")
	}
	if _, ok := c.get(urlKey{1, 4}); !ok {
		t.Error("URL without ts should fall back to the default lifetime")
	}
	c.invalidate(urlKey{1, 1})
	if _, ok := c.get(urlKey{1, 1}); ok {
		t.Error("invalidated URL still returned")
	}
}

func TestStreamURLResolutionChain(t *testing.T) {
	t.Parallel()
	// Full chain: URL cache -> page cache (stale URL) -> cache-busted
	// fresh fetch.
	var page string
	var pageQueries []string
	pageHits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/fakehost/album/demo", countingHandler(func(w http.ResponseWriter, r *http.Request) {
		pageQueries = append(pageQueries, r.URL.RawQuery)
		w.Write([]byte(page))
	}, &pageHits))
	mux.HandleFunc("/stream/100", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("MP3DATA"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	// The single page version carries an expired URL, so resolution must
	// end in a cache-busted refetch and the "no valid stream URL" error.
	page = demoPage(srv.URL, time.Now().Add(-time.Hour))

	c := testClient(t, srv, "deerhoof")
	if _, err := c.Open(context.Background(), synthTrack(42, "fakehost.bandcamp.com", "demo", "100")); err == nil {
		t.Fatal("Open: want the no-valid-URL error when every render is expired")
	}
	if pageHits != 2 {
		t.Fatalf("page hits = %d, want 2 (page-cache read, then forced refetch)", pageHits)
	}
	if pageQueries[0] != "" {
		t.Errorf("first page query = %q; the ordinary read must not be cache-busted", pageQueries[0])
	}
	if !strings.HasPrefix(pageQueries[1], "_=") {
		t.Errorf("refetch query = %q; the fresh fetch must carry the cache buster", pageQueries[1])
	}
}

// TestCacheBust pins the buster shape: the result must differ from its
// input (a fresh cache key) whether or not the URL already has a query.
func TestCacheBust(t *testing.T) {
	t.Parallel()
	plain := "https://x.bandcamp.com/album/demo"
	if got := cacheBust(plain); got == plain || !strings.Contains(got, "?_=") {
		t.Errorf("cacheBust(%q) = %q; want a distinct busted URL", plain, got)
	}
	queried := "https://x.bandcamp.com/album/demo?x=1"
	if got := cacheBust(queried); got == queried || !strings.Contains(got, "&_=") {
		t.Errorf("cacheBust(%q) = %q; want a distinct busted URL", queried, got)
	}
}

// TestStreamClientTimeoutPolicy pins the stream client's timeout shape: no
// global request timeout (which would sever a body read longer than the
// timeout — and the audio engine reads bodies in real time, so every track
// longer than the timeout), a ResponseHeaderTimeout to bound setup, no
// HTTP/2 (ResponseHeaderTimeout is not enforced for h2 requests), and an
// explicit http/1.1 ALPN: with h2 wiring off, the stdlib would otherwise
// send a ClientHello advertising no protocols at all, which the CDN edge
// refuses (EOF after the headers — see streamClient).
func TestStreamClientTimeoutPolicy(t *testing.T) {
	t.Parallel()
	c := streamClient()
	if c.Timeout != 0 {
		t.Errorf("stream client Timeout = %v, want 0 (a global timeout severs long stream bodies)", c.Timeout)
	}
	transport, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("stream client transport = %T, want *http.Transport", c.Transport)
	}
	if transport.ResponseHeaderTimeout <= 0 {
		t.Error("stream client must bound request setup with a ResponseHeaderTimeout")
	}
	if transport.ForceAttemptHTTP2 {
		t.Error("stream client must not attempt HTTP/2 (ResponseHeaderTimeout is not enforced for h2)")
	}
	protos := transport.TLSClientConfig.NextProtos
	if len(protos) != 1 || protos[0] != "http/1.1" {
		t.Errorf("stream client NextProtos = %v, want [http/1.1] (an ALPN-less handshake is refused by the CDN edge)", protos)
	}
}

// TestOpenSlowStreamBody proves the body itself is untimed: a stream whose
// body arrives slower than the header deadline still reads to completion.
func TestOpenSlowStreamBody(t *testing.T) {
	t.Parallel()
	var page string
	mux := http.NewServeMux()
	mux.HandleFunc("/fakehost/album/demo", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(page))
	})
	mux.HandleFunc("/stream/100", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		fl, _ := w.(http.Flusher)
		// The headers are instant; the body then trickles for three
		// seconds, well past the header deadline below.
		for i := 0; i < 3; i++ {
			w.Write([]byte("A"))
			fl.Flush()
			time.Sleep(time.Second)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	page = demoPage(srv.URL, time.Now().Add(24*time.Hour))

	c := testClient(t, srv, "deerhoof")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = false
	transport.ResponseHeaderTimeout = 100 * time.Millisecond
	c.stream = &http.Client{Transport: transport}

	stream, err := c.Open(context.Background(), synthTrack(42, "fakehost.bandcamp.com", "demo", "100"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer stream.Body.Close()
	body, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatalf("body read: %v (a slow body must not be cut off)", err)
	}
	if string(body) != "AAA" {
		t.Errorf("body = %q, want AAA", body)
	}
}

// TestOpenHeaderStallTimesOut proves setup is still bounded: a stream
// endpoint that never sends headers fails within the header deadline.
func TestOpenHeaderStallTimesOut(t *testing.T) {
	t.Parallel()
	var page string
	mux := http.NewServeMux()
	mux.HandleFunc("/fakehost/album/demo", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(page))
	})
	mux.HandleFunc("/stream/100", func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
		w.Write([]byte("MP3DATA"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	page = demoPage(srv.URL, time.Now().Add(24*time.Hour))

	c := testClient(t, srv, "deerhoof")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = false
	transport.ResponseHeaderTimeout = 100 * time.Millisecond
	c.stream = &http.Client{Transport: transport}

	start := time.Now()
	_, err := c.Open(context.Background(), synthTrack(42, "fakehost.bandcamp.com", "demo", "100"))
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("Open: want an error when the stream headers never arrive")
	}
	if elapsed > time.Second {
		t.Errorf("Open took %v; the header deadline should fail fast", elapsed)
	}
}

// TestOpenStreamContentLengthUnknown pins the -1 convention.
func TestOpenStreamContentLengthUnknown(t *testing.T) {
	t.Parallel()
	var page string
	mux := http.NewServeMux()
	mux.HandleFunc("/fakehost/album/demo", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(page))
	})
	mux.HandleFunc("/stream/100", func(w http.ResponseWriter, _ *http.Request) {
		// Flush partway through so the server commits to chunked
		// encoding: the client then sees an unknown body length.
		w.Write([]byte("MP3"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		w.Write([]byte("DATA"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	page = demoPage(srv.URL, time.Now().Add(24*time.Hour))

	c := testClient(t, srv, "deerhoof")
	stream, err := c.Open(context.Background(), synthTrack(42, "fakehost.bandcamp.com", "demo", "100"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer stream.Body.Close()
	if stream.ContentLength != -1 {
		t.Errorf("ContentLength = %d, want -1 for an unknown length", stream.ContentLength)
	}
}
