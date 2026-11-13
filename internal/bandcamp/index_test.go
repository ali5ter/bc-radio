package bandcamp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// namedFollowingBlob renders a data-blob JSON naming the given artists
// (name, subdomain) with band IDs 100, 101, ....
func namedFollowingBlob(artists ...[2]string) string {
	seq := make([]string, len(artists))
	cache := make([]string, len(artists))
	for i, a := range artists {
		id := 100 + i
		seq[i] = fmt.Sprintf(`"%d"`, id)
		cache[i] = fmt.Sprintf(`"%d":{"band_id":%d,"name":%q,"art_id":%d,`+
			`"url_hints":{"subdomain":%q,"custom_domain":null},"date_followed":"07 Jan 2026 12:03:45 GMT","token":"t"}`,
			id, id, a[0], 1000+i, a[1])
	}
	return fmt.Sprintf(`{"fan_data":{"identity":null},"following_bands_data":{"item_count":%d,"batch_size":20,`+
		`"sequence":[%s],"last_token":"x"},"item_cache":{"following_bands":{%s}}}`,
		len(artists), strings.Join(seq, ","), strings.Join(cache, ","))
}

// htmlOK serves s as a text/html page.
func htmlOK(s string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(s))
	}
}

// TestTracksWalk covers the best-effort contract: unreadable artists and
// releases are skipped and named in the returned error, never fatal.
func TestTracksWalk(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/testuser/following/artists_and_labels", htmlOK(followingPage(namedFollowingBlob(
		[2]string{"Alpha", "alpha"},
		[2]string{"Beta", "beta"},
		[2]string{"Gamma", "gamma"},
	))))
	mux.HandleFunc("/alpha/music", htmlOK(musicPage(
		releaseCard(5001, 100, "one", "Alpha One", 1),
		releaseCard(5002, 100, "two", "Alpha Two", 2),
	)))
	mux.HandleFunc("/alpha/album/one", htmlOK(tralbumPage(tralbumJSON(5001, "alpha", "Alpha", "one", "Alpha One", 100,
		tralbumTrack{ID: 1, TrackID: 1, Title: "Alpha One 1", TrackNum: 1, Duration: 180}))))
	mux.HandleFunc("/alpha/album/two", htmlOK(tralbumPage(tralbumJSON(5002, "alpha", "Alpha", "two", "Alpha Two", 100,
		tralbumTrack{ID: 2, TrackID: 2, Title: "Alpha Two 1", TrackNum: 1, Duration: 200}))))
	mux.HandleFunc("/beta/music", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	mux.HandleFunc("/gamma/music", htmlOK(musicPage(
		releaseCard(5003, 102, "one", "Gamma One", 3),
		releaseCard(5004, 102, "two", "Gamma Two", 4),
	)))
	mux.HandleFunc("/gamma/album/one", htmlOK(tralbumPage(tralbumJSON(5003, "gamma", "Gamma", "one", "Gamma One", 102,
		tralbumTrack{ID: 3, TrackID: 3, Title: "Gamma One 1", TrackNum: 1, Duration: 220}))))
	mux.HandleFunc("/gamma/album/two", func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := testClient(t, srv, "testuser")

	tracks, err := c.Tracks(context.Background())
	if err == nil {
		t.Fatal("err = nil, want the skipped artist and release named")
	}
	for _, part := range []string{"1 artist unavailable (Beta)", "1 release unavailable (Gamma Two)"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("err = %q, want it to contain %q", err, part)
		}
	}
	want := []string{"Alpha One 1", "Alpha Two 1", "Gamma One 1"}
	if len(tracks) != len(want) {
		t.Fatalf("len(tracks) = %d, want %d", len(tracks), len(want))
	}
	for i, title := range want {
		if tracks[i].Title != title {
			t.Errorf("tracks[%d].Title = %q, want %q", i, tracks[i].Title, title)
		}
	}
	// Tracks must be self-sufficient: display strings and location denormalized.
	first := tracks[0]
	if first.BandName != "Alpha" || first.BandID != 100 || first.AlbumID != 5001 ||
		first.AlbumSlug != "one" || first.AlbumTitle != "Alpha One" ||
		first.Subdomain != "alpha.bandcamp.com" || first.Number != 1 ||
		first.Duration != 180*time.Second {
		t.Errorf("first track not self-sufficient: %+v", first)
	}
}

// TestTracksCleanWalk is a walk with no gaps: tracks and a nil error.
func TestTracksCleanWalk(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/testuser/following/artists_and_labels", htmlOK(followingPage(namedFollowingBlob(
		[2]string{"Solo", "solo"},
	))))
	mux.HandleFunc("/solo/music", htmlOK(musicPage(releaseCard(6001, 100, "ep", "Solo EP", 1))))
	mux.HandleFunc("/solo/album/ep", htmlOK(tralbumPage(tralbumJSON(6001, "solo", "Solo", "ep", "Solo EP", 100,
		tralbumTrack{ID: 1, TrackID: 1, Title: "Solo 1", TrackNum: 1, Duration: 60},
		tralbumTrack{ID: 2, TrackID: 2, Title: "Solo 2", TrackNum: 2, Duration: 90},
	))))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := testClient(t, srv, "testuser")

	tracks, err := c.Tracks(context.Background())
	if err != nil {
		t.Fatalf("err = %v, want nil on a clean walk", err)
	}
	if len(tracks) != 2 || tracks[0].Title != "Solo 1" || tracks[1].Title != "Solo 2" {
		t.Errorf("tracks = %+v, want the two EP tracks in order", tracks)
	}
}

// TestTracksNoFollows: an empty follow list is a valid answer, not an error.
func TestTracksNoFollows(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/testuser/following/artists_and_labels", htmlOK(followingPage(namedFollowingBlob())))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := testClient(t, srv, "testuser")
	tracks, err := c.Tracks(context.Background())
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if tracks != nil {
		t.Errorf("tracks = %+v, want nil", tracks)
	}
}

// TestTracksFollowListFails: the one hard stop — the follow list itself
// failing leaves nothing to walk.
func TestTracksFollowListFails(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/testuser/following/artists_and_labels", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := testClient(t, srv, "testuser")
	_, err := c.Tracks(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v, want the follow-list fetch failure", err)
	}
}

// TestTracksCachesPages pins the cache-warm contract: a second Tracks makes
// no page hits at all (the TTL is 24h, the test runs in milliseconds).
func TestTracksCachesPages(t *testing.T) {
	t.Parallel()
	var hitsFollowing, hitsMusic, hitsAlbum int
	mux := http.NewServeMux()
	mux.HandleFunc("/testuser/following/artists_and_labels",
		countingHandler(htmlOK(followingPage(namedFollowingBlob([2]string{"Solo", "solo"}))), &hitsFollowing))
	mux.HandleFunc("/solo/music", countingHandler(htmlOK(musicPage(releaseCard(6001, 100, "ep", "Solo EP", 1))), &hitsMusic))
	mux.HandleFunc("/solo/album/ep",
		countingHandler(htmlOK(tralbumPage(tralbumJSON(6001, "solo", "Solo", "ep", "Solo EP", 100,
			tralbumTrack{ID: 1, TrackID: 1, Title: "Solo 1", TrackNum: 1, Duration: 60}))), &hitsAlbum))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := testClient(t, srv, "testuser")
	ctx := context.Background()
	if _, err := c.Tracks(ctx); err != nil {
		t.Fatalf("first Tracks: %v", err)
	}
	if hitsFollowing != 1 || hitsMusic != 1 || hitsAlbum != 1 {
		t.Fatalf("first walk hit the network %d/%d/%d times, want 1/1/1", hitsFollowing, hitsMusic, hitsAlbum)
	}
	tracks, err := c.Tracks(ctx)
	if err != nil {
		t.Fatalf("second Tracks: %v", err)
	}
	if hitsFollowing != 1 || hitsMusic != 1 || hitsAlbum != 1 {
		t.Fatalf("second walk hit the network %d/%d/%d times, want no hits (page cache)", hitsFollowing, hitsMusic, hitsAlbum)
	}
	if len(tracks) != 1 {
		t.Errorf("second walk returned %d tracks, want 1", len(tracks))
	}
}

// TestTracksBatchCap: the >20 embed cap surfaces as a partial-list note,
// cascading into per-artist skips when every /music page is also missing.
func TestTracksBatchCap(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/testuser/following/artists_and_labels", htmlOK(followingPage(followingBlobJSON(25, 20))))
	for i := 0; i < 20; i++ {
		sub := fmt.Sprintf("sub%d", i)
		mux.HandleFunc("/"+sub+"/music", func(w http.ResponseWriter, _ *http.Request) {
			http.NotFound(w, nil)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := testClient(t, srv, "testuser")
	tracks, err := c.Tracks(context.Background())
	if err == nil {
		t.Fatal("err = nil, want the partial-list note and the skip list")
	}
	for _, part := range []string{"the list is partial", "20 artists unavailable (Band 0, Band 1, Band 2, 17 more)"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("err = %q, want it to contain %q", err, part)
		}
	}
	if len(tracks) != 0 {
		t.Errorf("tracks = %d, want none (every /music page was missing)", len(tracks))
	}
}

// TestTracksCancelled: a cancelled context stops the walk with the context
// error, whatever it had gathered so far.
func TestTracksCancelled(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/testuser/following/artists_and_labels", htmlOK(followingPage(namedFollowingBlob(
		[2]string{"Alpha", "alpha"},
	))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := testClient(t, srv, "testuser")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Tracks(ctx)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// The Client's satisfaction of contract.Library (now including Tracks) is
// pinned at compile time in artists_test.go.
