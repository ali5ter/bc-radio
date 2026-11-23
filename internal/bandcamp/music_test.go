package bandcamp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ali5ter/bc-radio/internal/contract"
)

// TestAlbumsSingleReleaseRedirect pins the one-release account: Bandcamp
// answers /music with a 303 to the release page, so the card walk finds
// nothing in a page that is a release, not a listing. The moved final URL
// is the redirect's signature and its path supplies the slug; the album
// comes back whole, on a fresh fetch and on the cached read (the cache
// must carry the final URL, or the album would vanish on the second
// launch).
func TestAlbumsSingleReleaseRedirect(t *testing.T) {
	t.Parallel()
	srv := fixtureServer(t)
	c := testClient(t, srv, "deerhoof")
	artist := artistWith("gokurosawa", "")

	albums, err := c.Albums(context.Background(), artist)
	if err != nil {
		t.Fatalf("Albums: %v", err)
	}
	if len(albums) != 1 {
		t.Fatalf("len(albums) = %d, want 1 (the redirected release)", len(albums))
	}
	if albums[0].Title != "soft shakes" || albums[0].Slug != "soft-shakes" {
		t.Errorf("album = %q %q, want \"soft shakes\" soft-shakes", albums[0].Title, albums[0].Slug)
	}
	if len(albums[0].Tracks) != 8 {
		t.Errorf("len(tracks) = %d, want 8", len(albums[0].Tracks))
	}
	// The cached read of /music carries the same body and final URL.
	again, err := c.Albums(context.Background(), artist)
	if err != nil {
		t.Fatalf("Albums (cached): %v", err)
	}
	if len(again) != 1 || again[0].Title != "soft shakes" {
		t.Fatalf("cached read = %d albums (%v), want the release: it must not vanish", len(again), titles(again))
	}
}

func titles(albums []contract.Album) []string {
	out := make([]string, len(albums))
	for i, a := range albums {
		out[i] = a.Title
	}
	return out
}

// TestRedirectRelease pins the redirect's signature: a /music request
// whose final URL is a release page names the release's kind and slug; an
// in-place answer (a true listing) or a redirect elsewhere (a home page)
// does not.
func TestRedirectRelease(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		requested string
		final     string
		wantKind  string
		wantSlug  string
		wantOK    bool
	}{
		{name: "in place", requested: "https://x.bandcamp.com/music", final: "https://x.bandcamp.com/music"},
		{name: "album redirect", requested: "https://gokurosawa.bandcamp.com/music", final: "https://gokurosawa.bandcamp.com/album/soft-shakes", wantKind: "album", wantSlug: "soft-shakes", wantOK: true},
		{name: "prefixed test origin", requested: "http://127.0.0.1:1/gokurosawa/music", final: "http://127.0.0.1:1/gokurosawa/album/soft-shakes", wantKind: "album", wantSlug: "soft-shakes", wantOK: true},
		{name: "track redirect", requested: "https://x.bandcamp.com/music", final: "https://x.bandcamp.com/track/earn", wantKind: "track", wantSlug: "earn", wantOK: true},
		{name: "home redirect", requested: "https://x.bandcamp.com/music", final: "https://x.bandcamp.com/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kind, slug, ok := redirectRelease(tt.requested, tt.final)
			if ok != tt.wantOK || kind != tt.wantKind || slug != tt.wantSlug {
				t.Errorf("redirectRelease = %q %q %v, want %q %q %v", kind, slug, ok, tt.wantKind, tt.wantSlug, tt.wantOK)
			}
		})
	}
}

// TestAlbumsSingles pins the synthetic grouping on the corpus's
// singles-only artist (TONTRAKUL: three single-track releases, no
// albums): Albums shows one "singles" entry, and the full record is
// best-effort per single — the one readable single comes back with the
// single's stream location (TrackSlug, no AlbumSlug) and the grouping
// name, the two killed singles are named in the error.
func TestAlbumsSingles(t *testing.T) {
	t.Parallel()
	srv := fixtureServer(t)
	c := testClient(t, srv, "deerhoof")
	artist := contract.Artist{Name: "TONTRAKUL", BandID: 3908733182, Subdomain: "tontrakul"}

	albums, err := c.Albums(context.Background(), artist)
	if err != nil {
		t.Fatalf("Albums: %v", err)
	}
	if len(albums) != 1 {
		t.Fatalf("len(albums) = %d, want 1 (the singles grouping, no real albums)", len(albums))
	}
	if !albums[0].Single || albums[0].Title != "singles" {
		t.Fatalf("grouping = %q single=%v, want the synthetic 'singles' album", albums[0].Title, albums[0].Single)
	}

	full, err := c.Album(context.Background(), artist, albums[0])
	if err == nil {
		t.Fatal("Album: want an error naming the two killed singles")
	}
	if len(full.Tracks) != 1 {
		t.Fatalf("len(tracks) = %d, want 1 (the readable single keeps its siblings' group): %v", len(full.Tracks), err)
	}
	tr := full.Tracks[0]
	if tr.Title != "Earn" || tr.ID != "2391514618" {
		t.Errorf("track = %q %q, want Earn 2391514618", tr.Title, tr.ID)
	}
	if tr.TrackSlug != "earn" {
		t.Errorf("TrackSlug = %q, want earn (its /track/ page is the stream source)", tr.TrackSlug)
	}
	if tr.AlbumSlug != "" {
		t.Errorf("AlbumSlug = %q, want empty (a single has no /album/ page)", tr.AlbumSlug)
	}
	if tr.AlbumTitle != "singles" {
		t.Errorf("AlbumTitle = %q, want the grouping name", tr.AlbumTitle)
	}
	if tr.Subdomain != "tontrakul.bandcamp.com" {
		t.Errorf("Subdomain = %q, want the canonical host from the blob", tr.Subdomain)
	}
	if tr.Duration < 270*time.Second || tr.Duration > 272*time.Second {
		t.Errorf("Duration = %v, want about 270.8 s", tr.Duration)
	}
	if !strings.Contains(err.Error(), "Isan Lum Long") {
		t.Errorf("err = %q, want the killed singles named", err)
	}
}

func TestAlbums(t *testing.T) {
	t.Parallel()
	srv := fixtureServer(t)
	c := testClient(t, srv, "deerhoof")

	tests := []struct {
		name       string
		artist     contract.Artist
		wantCount  int
		firstTitle string
		firstSlug  string
		firstArtID int64
	}{
		{
			// 14 albums + the synthetic "singles" grouping of the 5
			// single-track releases (one entry, not five rows).
			name:       "mrtw music page",
			artist:     artistWith("mrtw", ""),
			wantCount:  15,
			firstTitle: "Mr Twin Sister (Demos, Outtakes, B-Sides)",
			firstSlug:  "mr-twin-sister-demos-outtakes-b-sides",
			firstArtID: 4191051966,
		},
		{
			// A >16-release catalog: 16 static cards, the remaining 38
			// (35 albums, 3 singles) in the grid's data-client-items.
			// The 51 albums (16 static + 35 client) plus the synthetic
			// "singles" grouping of the 3 client singles. Before the
			// client-items merge (2026-11-23) this read 16 — the rest
			// of the catalog missing from the index.
			name:       "deerhoof music page",
			artist:     artistWith("deerhoof", ""),
			wantCount:  52,
			firstTitle: "Divine Schism",
			firstSlug:  "divine-schism",
			firstArtID: 1107809698,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			albums, err := c.Albums(context.Background(), tt.artist)
			if err != nil {
				t.Fatalf("Albums: %v", err)
			}
			if len(albums) != tt.wantCount {
				t.Fatalf("len(albums) = %d, want %d", len(albums), tt.wantCount)
			}
			first := albums[0]
			if first.Title != tt.firstTitle {
				t.Errorf("first.Title = %q, want %q", first.Title, tt.firstTitle)
			}
			if first.Slug != tt.firstSlug {
				t.Errorf("first.Slug = %q, want %q", first.Slug, tt.firstSlug)
			}
			if first.ArtID != tt.firstArtID {
				t.Errorf("first.ArtID = %d, want %d", first.ArtID, tt.firstArtID)
			}
			// Minimal albums carry no tracks and no release date.
			for _, a := range albums {
				if a.Tracks != nil || !a.ReleaseDate.IsZero() || a.Slug == "" {
					t.Errorf("album %q is not minimal: %+v", a.Title, a)
				}
			}
		})
	}
}

func TestSplitItemID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in       string
		wantKind string
		wantNum  int64
		wantOK   bool
	}{
		{in: "album-123", wantKind: "album", wantNum: 123, wantOK: true},
		{in: "track-456", wantKind: "track", wantNum: 456, wantOK: true},
		{in: "album", wantOK: false},
		{in: "album-", wantOK: false},
		{in: "album-x", wantOK: false},
		{in: "", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			kind, num, ok := splitItemID(tt.in)
			if ok != tt.wantOK || kind != tt.wantKind || num != tt.wantNum {
				t.Errorf("splitItemID(%q) = (%q, %d, %v), want (%q, %d, %v)",
					tt.in, kind, num, ok, tt.wantKind, tt.wantNum, tt.wantOK)
			}
		})
	}
}

func TestArtIDPattern(t *testing.T) {
	t.Parallel()
	tests := []struct {
		src  string
		want string
	}{
		{src: "https://f4.bcbits.com/img/a4191051966_2.jpg", want: "4191051966"},
		{src: "https://f4.bcbits.com/img/a1107809698_1.jpg", want: "1107809698"},
		{src: "https://f4.bcbits.com/img/albumart/02188/xxx.jpg", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			m := artIDPattern.FindStringSubmatch(tt.src)
			got := ""
			if m != nil {
				got = m[1]
			}
			if got != tt.want {
				t.Errorf("artID = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseMusicNoCards(t *testing.T) {
	t.Parallel()
	albums, singles, err := parseMusic("<html><body><p>nothing</p></body></html>")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(albums) != 0 || len(singles) != 0 {
		t.Errorf("albums = %+v, singles = %+v, want none", albums, singles)
	}
}

// clientItemsPage renders a /music page in the >16-release shape: the
// grid's static cards plus a data-client-items JSON array on the ol, in
// the &quot;-escaped form Bandcamp serves.
func clientItemsPage(cards, items string) string {
	esc := strings.ReplaceAll(items, `"`, `&quot;`)
	return `<html><body><ol id="music-grid" data-edit-callback="/music_reorder" data-client-items="` + esc + `">` + cards + `</ol></body></html>`
}

// TestParseMusicClientItems pins the data-client-items merge (the
// 2026-11-23 find: King Gizzard's /music carries 16 static cards and 16
// more releases in the grid's client batch; the static-only walk dropped
// all 16). Both sets must land — static cards in page order, then the
// batch in its own order, de-duplicated by ID, an entry without a
// release-page URL dropped.
func TestParseMusicClientItems(t *testing.T) {
	t.Parallel()
	cards :=
		`<li data-item-id="album-100" data-band-id="42"><a href="/album/duplicate">` +
			`<img src="https://f4.bcbits.com/img/a11_2.jpg"><p class="title"> Duplicate </p></a></li>` +
			`<li data-item-id="album-200" data-band-id="42"><a href="/album/static-only">` +
			`<img src="https://f4.bcbits.com/img/a22_2.jpg"><p class="title"> Static Only </p></a></li>` +
			`<li data-item-id="track-300" data-band-id="42"><a href="/track/static-single">` +
			`<p class="title"> Static Single </p></a></li>`
	items := `[` +
		`{"id":900,"band_id":42,"art_id":55,"page_url":"/album/from-the-batch","title":"From The Batch","type":"album"},` +
		`{"id":100,"band_id":42,"art_id":99,"page_url":"https://x.bandcamp.com/album/duplicate","title":"Duplicate","type":"album"},` +
		`{"id":777,"band_id":42,"art_id":77,"page_url":"/track/batch-single","title":"Batch Single","type":"track"},` +
		`{"id":888,"band_id":42,"art_id":88,"page_url":"/no-such-page","title":"Dropped","type":"album"}` +
		`]`

	albums, singles, err := parseMusic(clientItemsPage(cards, items))
	if err != nil {
		t.Fatalf("parseMusic: %v", err)
	}
	// The duplicate (ID 100, static card and client entry) is read once,
	// from the static card; the no-page entry is dropped.
	wantAlbums := []struct {
		id    int64
		slug  string
		title string
	}{
		{100, "duplicate", "Duplicate"},
		{200, "static-only", "Static Only"},
		{900, "from-the-batch", "From The Batch"},
	}
	if len(albums) != len(wantAlbums) {
		t.Fatalf("len(albums) = %d (%+v), want %d", len(albums), albums, len(wantAlbums))
	}
	for i, w := range wantAlbums {
		if albums[i].ID != w.id || albums[i].Slug != w.slug || albums[i].Title != w.title {
			t.Errorf("albums[%d] = %+v, want id %d slug %s title %s", i, albums[i], w.id, w.slug, w.title)
		}
	}
	if albums[2].BandID != 42 || albums[2].ArtID != 55 {
		t.Errorf("albums[2] band/art = %d/%d, want 42/55 from the client entry", albums[2].BandID, albums[2].ArtID)
	}
	if len(singles) != 2 {
		t.Fatalf("len(singles) = %d (%+v), want 2 (static and client)", len(singles), singles)
	}
	if singles[0].slug != "static-single" || singles[1].slug != "batch-single" {
		t.Errorf("singles = %+v, want static-single then batch-single", singles)
	}
}

// TestParseMusicClientItemsUndecodable pins the loud-fail: a
// data-client-items attribute that is present but will not decode is a
// schema drift — the parse fails rather than returning a half index.
func TestParseMusicClientItemsUndecodable(t *testing.T) {
	t.Parallel()
	page := clientItemsPage(
		`<li data-item-id="album-100" data-band-id="42"><a href="/album/x"><p class="title"> X </p></a></li>`,
		`not json`,
	)
	if _, _, err := parseMusic(page); err == nil {
		t.Fatal("parseMusic with an undecodable client batch = nil error, want the drift named")
	}
}

// TestCardPath pins the href normalisation: a relative link is the path
// already, an absolute or protocol-relative URL is reduced to its path
// (query and fragment discarded), a bare root or empty value fails.
func TestCardPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		href string
		want string
		ok   bool
	}{
		{name: "relative", href: "/album/moisturizer", want: "/album/moisturizer", ok: true},
		{name: "absolute", href: "https://wetleg.bandcamp.com/album/moisturizer", want: "/album/moisturizer", ok: true},
		{name: "protocol-relative", href: "//wetleg.bandcamp.com/track/wet-dream", want: "/track/wet-dream", ok: true},
		{name: "query discarded", href: "https://x.bandcamp.com/album/x?foo=1", want: "/album/x", ok: true},
		{name: "root", href: "https://x.bandcamp.com/"},
		{name: "empty", href: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := cardPath(tt.href)
			if ok != tt.ok || got != tt.want {
				t.Errorf("cardPath(%q) = (%q, %v), want (%q, %v)", tt.href, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// TestParseMusicAbsoluteHrefs pins the wetleg regression (2026-11-20): a
// /music listing whose album cards link absolutely
// (https://host/album/x) and whose single cards link relatively
// (/track/x). Both must parse to the same slugs — before the fix the
// absolute-href albums failed the raw /album/ prefix check and were
// dropped without a trace, and the artist came back with only its
// singles.
func TestParseMusicAbsoluteHrefs(t *testing.T) {
	t.Parallel()
	page := musicPage(
		`<li data-item-id="album-2053136752" data-band-id="1626407001">`+
			`<a href="https://wetleg.bandcamp.com/album/moisturizer-deluxe">`+
			`<div class="art"><img src="https://f4.bcbits.com/img/a3533201704_2.jpg" alt="" /></div>`+
			`<p class="title"> moisturizer (deluxe) </p></a></li>`,
		`<li data-item-id="track-4003442188" data-band-id="1626407001">`+
			`<a href="/track/too-late-now-soulwax-remix">`+
			`<div class="art"><img src="https://f4.bcbits.com/img/a2496422928_2.jpg" alt="" /></div>`+
			`<p class="title"> Too Late Now (Soulwax Remix) </p></a></li>`,
	)
	albums, singles, err := parseMusic(page)
	if err != nil {
		t.Fatalf("parseMusic: %v", err)
	}
	if len(albums) != 1 {
		t.Fatalf("len(albums) = %d, want 1 (the absolute-href card must parse)", len(albums))
	}
	a := albums[0]
	if a.Slug != "moisturizer-deluxe" || a.Title != "moisturizer (deluxe)" || a.BandID != 1626407001 || a.ArtID != 3533201704 {
		t.Errorf("album = %+v, want slug moisturizer-deluxe with its title, band and art", a)
	}
	if len(singles) != 1 {
		t.Fatalf("len(singles) = %d, want 1 (the relative-href card)", len(singles))
	}
	if singles[0].slug != "too-late-now-soulwax-remix" || singles[0].title != "Too Late Now (Soulwax Remix)" {
		t.Errorf("single = %+v, want slug too-late-now-soulwax-remix", singles[0])
	}
}
