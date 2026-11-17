package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ali5ter/bc-radio/internal/contract"
)

// twoBands is a small flat index in walk order: band A with two releases,
// band B with one.
func twoBands() []contract.Track {
	return []contract.Track{
		{ID: "1", Title: "Alpha", Number: 1, Duration: 186 * time.Second, BandID: 10, BandName: "Band A", AlbumID: 100, AlbumSlug: "first", AlbumTitle: "First", Subdomain: "a.bandcamp.com"},
		{ID: "2", Title: "Beta", Number: 2, Duration: 45 * time.Second, BandID: 10, BandName: "Band A", AlbumID: 100, AlbumSlug: "first", AlbumTitle: "First", Subdomain: "a.bandcamp.com"},
		{ID: "3", Title: "Gamma", Number: 1, Duration: 361 * time.Second, BandID: 10, BandName: "Band A", AlbumID: 101, AlbumSlug: "second", AlbumTitle: "Second", Subdomain: "a.bandcamp.com"},
		{ID: "4", Title: "Delta", Number: 1, Duration: 3 * time.Second, BandID: 20, BandName: "Band B", AlbumID: 102, AlbumSlug: "only", AlbumTitle: "Only", Subdomain: "b.bandcamp.com"},
	}
}

// The grouping keeps walk order at every level and fills the counts.
func TestIndexFromTracksGroupsInWalkOrder(t *testing.T) {
	bands := indexFromTracks(twoBands())
	if len(bands) != 2 {
		t.Fatalf("got %d bands; want 2", len(bands))
	}
	a := bands[0]
	if a.Name != "Band A" || a.ID != 10 || a.TrackCount != 3 || a.AlbumCount != 2 {
		t.Errorf("band A = %+v; want id 10, 3 tracks, 2 albums", a)
	}
	if len(a.Albums) != 2 || a.Albums[0].Slug != "first" || a.Albums[1].Slug != "second" {
		t.Errorf("band A albums out of order or count: %+v", a.Albums)
	}
	if a.Albums[0].TrackCount != 2 || a.Albums[0].Host != "a.bandcamp.com" {
		t.Errorf("album first = %+v; want 2 tracks, host a.bandcamp.com", a.Albums[0])
	}
	if a.Albums[0].Tracks[0].Title != "Alpha" || a.Albums[0].Tracks[1].Title != "Beta" {
		t.Errorf("track order broken: %q, %q", a.Albums[0].Tracks[0].Title, a.Albums[0].Tracks[1].Title)
	}
	b := bands[1]
	if b.Name != "Band B" || b.ID != 20 || b.TrackCount != 1 {
		t.Errorf("band B = %+v; want id 20, 1 track", b)
	}
}

// The JSON document carries the hierarchy with its counts, and an empty
// index serialises as an empty array, not null (a script must be able to
// iterate it).
func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, "ali5ter", twoBands()); err != nil {
		t.Fatal(err)
	}
	var doc exportIndex
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("the output is not valid JSON: %v", err)
	}
	if doc.Username != "ali5ter" || doc.BandCount != 2 || doc.TrackCount != 4 {
		t.Errorf("counts = %q, %d, %d; want ali5ter, 2, 4", doc.Username, doc.BandCount, doc.TrackCount)
	}
	tr := doc.Bands[0].Albums[0].Tracks[0]
	if tr.ID != "1" || tr.Number != 1 || tr.Title != "Alpha" || tr.Duration != 186 {
		t.Errorf("track = %+v; want Alpha, 186 s", tr)
	}
	if doc.Bands[1].Albums[0].Host != "b.bandcamp.com" {
		t.Errorf("host = %q; want b.bandcamp.com", doc.Bands[1].Albums[0].Host)
	}

	buf.Reset()
	if err := writeJSON(&buf, "ali5ter", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"bands": []`) {
		t.Errorf("empty index serialises as:\n%s", buf.String())
	}
}

// The list is linear and aligned: a summary line, then bands, albums and
// tracks with their durations, indented by level.
func TestWriteList(t *testing.T) {
	var buf bytes.Buffer
	if err := writeList(&buf, "ali5ter", twoBands()); err != nil {
		t.Fatal(err)
	}
	want := `ali5ter: 2 bands, 4 tracks

Band A
  First
     1. Alpha  3:06
     2. Beta   0:45
  Second
     1. Gamma  6:01
Band B
  Only
     1. Delta  0:03
`
	if buf.String() != want {
		t.Errorf("list output:\n%s\nwant:\n%s", buf.String(), want)
	}
}

// formatDuration truncates sub-second remainders, the way a clock does.
func TestFormatDuration(t *testing.T) {
	for _, tc := range []struct {
		seconds float64
		want    string
	}{
		{0, "0:00"},
		{3.7, "0:03"},
		{45, "0:45"},
		{186.28, "3:06"},
		{361, "6:01"},
		{3600, "60:00"},
	} {
		if got := formatDuration(tc.seconds); got != tc.want {
			t.Errorf("formatDuration(%v) = %q; want %q", tc.seconds, got, tc.want)
		}
	}
}

// The exit-code contract: an empty index with an error (the follow list
// itself failed) is the one hard stop; an empty index with no error is a
// valid answer; a partial index prints and reports its gaps.
func TestExportExitContract(t *testing.T) {
	o := options{username: "ali5ter", list: true}
	walk := func(tracks []contract.Track, err error) func(context.Context) ([]contract.Track, error) {
		return func(ctx context.Context) ([]contract.Track, error) {
			return tracks, err
		}
	}

	// Hard stop: no index, an error.
	var buf bytes.Buffer
	if err := o.export(context.Background(), walk(nil, context.DeadlineExceeded), &buf); err == nil {
		t.Error("hard stop: want an error, got nil")
	}
	if buf.Len() != 0 {
		t.Errorf("hard stop: stdout = %q; want empty", buf.String())
	}

	// Valid empty answer: nothing followed, no error.
	buf.Reset()
	if err := o.export(context.Background(), walk(nil, nil), &buf); err != nil {
		t.Fatalf("empty index: got %v; want success", err)
	}
	if buf.String() != "ali5ter: 0 bands, 0 tracks\n\n" {
		t.Errorf("empty index: stdout = %q", buf.String())
	}

	// Partial index: the gaps do not fail the export.
	buf.Reset()
	if err := o.export(context.Background(), walk(twoBands(), context.DeadlineExceeded), &buf); err != nil {
		t.Fatalf("partial index: got %v; want success", err)
	}
	if !strings.Contains(buf.String(), "Band A") {
		t.Errorf("partial index: stdout = %q; want the index", buf.String())
	}
}
