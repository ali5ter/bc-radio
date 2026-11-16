package bandcamp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ali5ter/bc-radio/internal/contract"
)

func TestArtists(t *testing.T) {
	t.Parallel()
	srv := fixtureServer(t)
	c := testClient(t, srv, "deerhoof")
	artists, err := c.Artists(context.Background())
	if err != nil {
		t.Fatalf("Artists: %v", err)
	}
	if len(artists) != 12 {
		t.Fatalf("len(artists) = %d, want 12", len(artists))
	}
	first := artists[0]
	if first.Name != "Tunji Oyelana" || first.Subdomain != "tunjioyelana" || first.BandID != 1199846473 {
		t.Errorf("artists[0] = %+v, want Tunji Oyelana / tunjioyelana / 1199846473", first)
	}
	if !first.DateFollowed.Equal(time.Date(2026, 1, 7, 12, 3, 45, 0, time.UTC)) {
		t.Errorf("DateFollowed = %v, want 2026-01-07 12:03:45 UTC", first.DateFollowed)
	}
	// Follow order must be preserved from the blob's sequence.
	if artists[1].Name != "Mr Twin Sister" || artists[1].Subdomain != "mrtwinsister" {
		t.Errorf("artists[1] = %+v, want Mr Twin Sister / mrtwinsister", artists[1])
	}
	last := artists[len(artists)-1]
	if last.Name != "OUTLIER RECORDINGS" {
		t.Errorf("artists[last] = %+v, want OUTLIER RECORDINGS", last)
	}
}

// followingBlobJSON renders a following-page blob with n cached bands.
func followingBlobJSON(itemCount, n int) string {
	seq := make([]string, n)
	cache := make([]string, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("\"%d\"", 1000+i) // string IDs, quoted in the JSON
		seq[i] = id
		cache[i] = fmt.Sprintf(`%s:{"band_id":%d,"name":"Band %d","art_id":%d,"url_hints":{"subdomain":"sub%d","custom_domain":null},"date_followed":"07 Jan 2026 12:03:45 GMT","token":"t"}`,
			id, 1000+i, i, 1000+i, i)
	}
	return fmt.Sprintf(`{"fan_data":{"identity":null},"following_bands_data":{"item_count":%d,"batch_size":20,"sequence":[%s],"last_token":"x"},"item_cache":{"following_bands":{%s}}}`,
		itemCount, strings.Join(seq, ","), strings.Join(cache, ","))
}

func TestParseFollowingBatchCap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		itemCount   int
		bands       int
		wantCount   int
		wantErrPart string
	}{
		{name: "within cap", itemCount: 12, bands: 12, wantCount: 12},
		{name: "over cap", itemCount: 25, bands: 20, wantCount: 20, wantErrPart: "partial"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			artists, err := parseFollowing(followingPage(followingBlobJSON(tt.itemCount, tt.bands)))
			if got := len(artists); got != tt.wantCount {
				t.Errorf("len = %d, want %d", got, tt.wantCount)
			}
			if (err != nil) != (tt.wantErrPart != "") {
				t.Fatalf("err = %v, want %v", err, tt.wantErrPart != "")
			}
			if err != nil && !strings.Contains(err.Error(), tt.wantErrPart) {
				t.Errorf("err = %q, want it to mention %q", err, tt.wantErrPart)
			}
		})
	}
}

// TestArtistsBatchCap pins the over-cap contract at the client level: the
// 20 readable bands are returned alongside the partial-list note, not
// discarded — the list is the library screen's data and the track
// index's input, and throwing it away would punish every user past 20
// follows for a Bandcamp limitation.
func TestArtistsBatchCap(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/big/following/artists_and_labels", htmlOK(followingPage(followingBlobJSON(25, 20))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := testClient(t, srv, "big")
	artists, err := c.Artists(context.Background())
	if err == nil || !strings.Contains(err.Error(), "partial") {
		t.Fatalf("err = %v, want the partial-list note", err)
	}
	if len(artists) != 20 {
		t.Fatalf("len(artists) = %d, want the 20 readable bands alongside the note", len(artists))
	}
}

func TestParseFollowingNoBlob(t *testing.T) {
	t.Parallel()
	_, err := parseFollowing("<html><body>no data here</body></html>")
	if err == nil || !strings.Contains(err.Error(), "data-blob") {
		t.Errorf("err = %v, want a data-blob error", err)
	}
}

func TestParseFollowingSkipsUncachedSequenceIDs(t *testing.T) {
	t.Parallel()
	// Sequence references a band missing from the item cache: it is
	// skipped, not fatal.
	page := followingPage(`{"following_bands_data":{"item_count":1,"sequence":["1","2"],"batch_size":20},"item_cache":{"following_bands":{"1":{"band_id":1,"name":"Kept","url_hints":{"subdomain":"s"}}}}}`)
	artists, err := parseFollowing(page)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(artists) != 1 || artists[0].Name != "Kept" {
		t.Errorf("artists = %+v, want the single cached band", artists)
	}
}

func TestParseBCDate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want time.Time
	}{
		{in: "07 Jan 2026 12:03:45 GMT", want: time.Date(2026, 1, 7, 12, 3, 45, 0, time.UTC)},
		{in: "25 Aug 2021 16:31:50 GMT", want: time.Date(2021, 8, 25, 16, 31, 50, 0, time.UTC)},
		{in: "12 Aug 2011 00:00:00 GMT", want: time.Date(2011, 8, 12, 0, 0, 0, 0, time.UTC)},
		{in: ""},
		{in: "not a date"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got := parseBCDate(tt.in)
			if !got.Equal(tt.want) {
				t.Errorf("parseBCDate(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestArtistsSatisfiesContract pins the interface at compile time.
var (
	_ contract.Library     = (*Client)(nil)
	_ contract.AudioSource = (*Client)(nil)
	_ contract.Indexer     = (*Client)(nil)
)
