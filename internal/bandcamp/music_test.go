package bandcamp

import (
	"context"
	"testing"

	"github.com/ali5ter/bc-radio/internal/contract"
)

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
			name:       "mrtw music page",
			artist:     artistWith("mrtw", ""),
			wantCount:  14, // 14 albums; the 5 single-track releases are skipped
			firstTitle: "Mr Twin Sister (Demos, Outtakes, B-Sides)",
			firstSlug:  "mr-twin-sister-demos-outtakes-b-sides",
			firstArtID: 4191051966,
		},
		{
			name:       "deerhoof music page",
			artist:     artistWith("deerhoof", ""),
			wantCount:  16,
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
	albums, err := parseMusic("<html><body><p>nothing</p></body></html>")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(albums) != 0 {
		t.Errorf("albums = %+v, want none", albums)
	}
}
