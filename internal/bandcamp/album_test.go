package bandcamp

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ali5ter/bc-radio/internal/contract"
)

func TestAlbum(t *testing.T) {
	t.Parallel()
	srv := fixtureServer(t)
	c := testClient(t, srv, "deerhoof")

	tests := []struct {
		name         string
		artist       contract.Artist
		slug         string
		wantTitle    string
		wantID       int64
		wantTrackLen int
		firstTrack   string
		firstDur     time.Duration
		wantHost     string // canonical host the blob's url implies
	}{
		{
			name:         "mrtw bad street",
			artist:       artistWith("mrtw", ""),
			slug:         "bad-street",
			wantTitle:    "Bad Street",
			wantID:       961980295,
			wantTrackLen: 3,
			firstTrack:   "Bad Street",
			firstDur:     time.Duration(283.253 * float64(time.Second)),
			wantHost:     "mrtwinsister.bandcamp.com",
		},
		{
			name:         "deerhoof actually you can",
			artist:       artistWith("deerhoof", ""),
			slug:         "actually-you-can",
			wantTitle:    "Actually, You Can",
			wantID:       1439106361,
			wantTrackLen: 9,
			firstTrack:   "Be Unbarred, O Ye Gates of Hell",
			firstDur:     time.Duration(186.282 * float64(time.Second)),
			wantHost:     "deerhoof.bandcamp.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			album, err := c.Album(context.Background(), tt.artist, contract.Album{Slug: tt.slug})
			if err != nil {
				t.Fatalf("Album: %v", err)
			}
			if album.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", album.Title, tt.wantTitle)
			}
			if album.ID != tt.wantID {
				t.Errorf("ID = %d, want %d", album.ID, tt.wantID)
			}
			if album.Slug != tt.slug {
				t.Errorf("Slug = %q, want %q", album.Slug, tt.slug)
			}
			if len(album.Tracks) != tt.wantTrackLen {
				t.Fatalf("len(Tracks) = %d, want %d", len(album.Tracks), tt.wantTrackLen)
			}
			first := album.Tracks[0]
			if first.Title != tt.firstTrack {
				t.Errorf("first.Title = %q, want %q", first.Title, tt.firstTrack)
			}
			if first.Duration != tt.firstDur {
				t.Errorf("first.Duration = %v, want %v", first.Duration, tt.firstDur)
			}
			if first.Subdomain != tt.wantHost {
				t.Errorf("first.Subdomain = %q, want %q (canonical host from the blob)", first.Subdomain, tt.wantHost)
			}
			if first.AlbumSlug != tt.slug || first.AlbumTitle != tt.wantTitle || first.AlbumID != tt.wantID {
				t.Errorf("track location fields = %+v, want album %q / %q / %d", first, tt.slug, tt.wantTitle, tt.wantID)
			}
			if first.BandName == "" {
				t.Error("first.BandName is empty")
			}
		})
	}
}

func TestAlbumNotFound(t *testing.T) {
	t.Parallel()
	srv := fixtureServer(t)
	c := testClient(t, srv, "deerhoof")
	if _, err := c.Album(context.Background(), artistWith("mrtw", ""), contract.Album{Slug: "no-such-album"}); err == nil {
		t.Fatal("Album: want a not-found error for a missing route")
	}
}

func TestParseTralbumNoBlob(t *testing.T) {
	t.Parallel()
	if _, err := parseTralbum("<html><body>nothing</body></html>"); err == nil {
		t.Fatal("parseTralbum: want an error for a page without data-tralbum")
	}
}

func TestParseTralbumWrongItemType(t *testing.T) {
	t.Parallel()
	// A foreign kind (neither album nor track) is not a release page.
	// "track" itself is valid — singles are a release with one track.
	page := tralbumPage(`{"id":1,"item_type":"merch","current":{}}`)
	if _, err := parseTralbum(page); err == nil || !strings.Contains(err.Error(), "not a release") {
		t.Errorf("err = %v, want a not-a-release error", err)
	}
}

// TestParseTralbumSinglePage pins the single's blob: a /track/ page
// (item_type "track", canonical /track/ URL) parses, and its one track
// carries the slug as TrackSlug, not AlbumSlug.
func TestParseTralbumSinglePage(t *testing.T) {
	t.Parallel()
	page := tralbumPage(`{
		"id": 7, "item_type": "track", "artist": "TONTRAKUL",
		"url": "https://tontrakul.bandcamp.com/track/earn",
		"current": {"title": "Earn", "band_id": 7, "art_id": 1, "release_date": "01 Jan 2020 00:00:00 GMT"},
		"trackinfo": [
			{"id": 100, "track_id": 100, "title": "Earn", "track_num": 1, "duration": 10.5, "file": {}}
		]
	}`)
	release, err := parseTralbum(page)
	if err != nil {
		t.Fatalf("parseTralbum: %v", err)
	}
	if len(release.Album.Tracks) != 1 {
		t.Fatalf("len(tracks) = %d, want 1", len(release.Album.Tracks))
	}
	tr := release.Album.Tracks[0]
	if tr.TrackSlug != "earn" {
		t.Errorf("TrackSlug = %q, want earn (from the blob's /track/ URL)", tr.TrackSlug)
	}
	if tr.AlbumSlug != "" {
		t.Errorf("AlbumSlug = %q, want empty (a single has no /album/ page)", tr.AlbumSlug)
	}
	if release.Host != "tontrakul.bandcamp.com" {
		t.Errorf("Host = %q, want the canonical host", release.Host)
	}
}

func TestParseTralbumSynthetic(t *testing.T) {
	t.Parallel()
	const ts = 1999999999
	page := tralbumPage(`{
		"id": 42, "item_type": "album", "artist": "Synth Band",
		"url": "https://synth.bandcamp.com/album/demo",
		"current": {"title": "Demo", "band_id": 7, "art_id": 99, "release_date": "01 Jan 2020 00:00:00 GMT"},
		"trackinfo": [
			{"id": 100, "track_id": 100, "title": "One", "track_num": 1, "duration": 10.5, "file": {"mp3-128": "https://t4.bcbits.com/stream/x/mp3-128/100?ts=` + strconv.FormatInt(ts, 10) + `"}},
			{"id": 101, "track_id": 101, "title": "Two", "track_num": 2, "duration": 20.25, "file": {"mp3-128": "https://t4.bcbits.com/stream/x/mp3-128/101?ts=` + strconv.FormatInt(ts, 10) + `"}}
		]
	}`)
	release, err := parseTralbum(page)
	if err != nil {
		t.Fatalf("parseTralbum: %v", err)
	}
	if release.Album.ID != 42 || release.Album.Title != "Demo" || release.Album.Slug != "demo" {
		t.Errorf("album = %+v, want id 42 / Demo / demo", release.Album)
	}
	if release.Host != "synth.bandcamp.com" {
		t.Errorf("Host = %q, want synth.bandcamp.com", release.Host)
	}
	if !release.Album.ReleaseDate.Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("ReleaseDate = %v", release.Album.ReleaseDate)
	}
	if len(release.Album.Tracks) != 2 {
		t.Fatalf("tracks = %d, want 2", len(release.Album.Tracks))
	}
	if release.Album.Tracks[0].Duration != 10500*time.Millisecond {
		t.Errorf("track 0 duration = %v, want 10.5s", release.Album.Tracks[0].Duration)
	}
	if u, ok := release.URLs[100]; !ok || !strings.Contains(u, "ts=") {
		t.Errorf("URLs[100] = %q, want a signed url", u)
	}
}

func TestStreamFileURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		file   map[string]string
		want   string
		wantOK bool
	}{
		{name: "prefers mp3-128", file: map[string]string{"mp3-128": "u128", "mp3-640": "u640"}, want: "u128", wantOK: true},
		{name: "falls back to other mp3", file: map[string]string{"mp3-640": "u640"}, want: "u640", wantOK: true},
		{name: "ignores non-mp3", file: map[string]string{"streaming": "preview"}, wantOK: false},
		{name: "empty map", file: map[string]string{}, wantOK: false},
		{name: "blank 128 falls through", file: map[string]string{"mp3-128": "", "mp3-640": "u640"}, want: "u640", wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := streamFileURL(tt.file)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("streamFileURL = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
