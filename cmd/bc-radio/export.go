package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ali5ter/bc-radio/internal/contract"
)

// export runs the non-interactive path of bc-radio: walk the radio's flat
// track index, print it to stdout in the format the flags choose, and
// report the result — the interface, the audio engine, and a terminal are
// all out of the picture. The output is exactly the library the TUI's radio
// draws from, so a script and the interface never disagree about the
// library.
//
// The walk is best-effort, as in the TUI: an unreadable band or release is
// skipped and named on stderr, and the result stays success — the index is
// the union of what could be read. The one hard stop is an unreadable
// follow list (an unknown account, or the network down): no index can be
// built at all, and the error is returned for a non-zero exit.
//
// walk is injected — production passes *bandcamp.Client.Tracks — and stdout
// likewise, so the exit-code contract is testable without a network or a
// terminal.
func (o options) export(ctx context.Context, walk func(ctx context.Context) ([]contract.Track, error), stdout io.Writer) error {
	// A cold build walks the web for a minute; one line on stderr (so a
	// piped stdout stays clean) is the sign that the walk is alive.
	fmt.Fprintln(os.Stderr, "bc-radio: building track index…")

	tracks, err := walk(ctx)
	if len(tracks) == 0 && err != nil {
		return err
	}

	var write func(w io.Writer) error
	if o.jsonOut {
		write = func(w io.Writer) error {
			return writeJSON(w, o.username, tracks)
		}
	} else {
		write = func(w io.Writer) error {
			return writeList(w, o.username, tracks)
		}
	}
	if err := write(stdout); err != nil {
		return err
	}
	if err != nil {
		// The walk's gaps, for the script's log: the output above is
		// everything that could be read.
		fmt.Fprintf(os.Stderr, "bc-radio: %v\n", err)
	}
	return nil
}

// The JSON document: the flat index grouped back into its hierarchy, in
// walk order — follows, then each band's releases, then each release's
// tracks — the same order the TUI's library shows.

type exportIndex struct {
	Username   string       `json:"username"`
	BandCount  int          `json:"band_count"`
	TrackCount int          `json:"track_count"`
	Bands      []exportBand `json:"bands"`
}

type exportBand struct {
	ID         int64         `json:"id"`
	Name       string        `json:"name"`
	AlbumCount int           `json:"album_count"`
	TrackCount int           `json:"track_count"`
	Albums     []exportAlbum `json:"albums"`
}

// exportAlbum carries the release's host: the canonical host the release
// lives on, from which its album URL is https://host/album/slug.
type exportAlbum struct {
	ID         int64         `json:"id"`
	Host       string        `json:"host,omitempty"`
	Title      string        `json:"title"`
	Slug       string        `json:"slug"`
	TrackCount int           `json:"track_count"`
	Tracks     []exportTrack `json:"tracks"`
}

type exportTrack struct {
	ID       string  `json:"id"`
	Number   int     `json:"number"`
	Title    string  `json:"title"`
	Duration float64 `json:"duration_seconds"`
}

// writeJSON prints the index as JSON, indented for the human who skims it.
func writeJSON(w io.Writer, username string, tracks []contract.Track) error {
	doc := exportIndex{Username: username, Bands: make([]exportBand, 0)}
	for _, b := range indexFromTracks(tracks) {
		doc.Bands = append(doc.Bands, b)
	}
	doc.BandCount = len(doc.Bands)
	doc.TrackCount = len(tracks)

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// writeList prints the index as a plain-text list, readable top to bottom:
// the linear fallback for the 60 fps interface, for a screen reader or a
// person walking the library in order without a redraw in the way.
func writeList(w io.Writer, username string, tracks []contract.Track) error {
	bands := indexFromTracks(tracks)
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d bands, %d tracks\n\n", username, len(bands), len(tracks))
	for _, band := range bands {
		b.WriteString(band.Name)
		b.WriteByte('\n')
		for _, album := range band.Albums {
			fmt.Fprintf(&b, "  %s\n", album.Title)
			// One width per album, so the durations line up.
			width := 0
			for _, tr := range album.Tracks {
				if len(tr.Title) > width {
					width = len(tr.Title)
				}
			}
			for _, tr := range album.Tracks {
				fmt.Fprintf(&b, "    %2d. %-*s  %s\n", tr.Number, width, tr.Title, formatDuration(tr.Duration))
			}
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// indexFromTracks groups the flat radio index back into bands, albums and
// tracks, keeping walk order at every level: first-seen order, which is the
// follow order, the /music order, and the release's own track order.
func indexFromTracks(tracks []contract.Track) []exportBand {
	bands := make([]exportBand, 0)
	bandPos := make(map[string]int)  // band key -> index in bands
	albumPos := make(map[string]int) // bandKey/albumSlug -> index in the band's albums

	for _, t := range tracks {
		bk := bandKey(t)
		bi, ok := bandPos[bk]
		if !ok {
			bi = len(bands)
			bandPos[bk] = bi
			bands = append(bands, exportBand{ID: t.BandID, Name: t.BandName, Albums: make([]exportAlbum, 0)})
		}
		ak := bk + "/" + t.AlbumSlug
		ai, ok := albumPos[ak]
		if !ok {
			ai = len(bands[bi].Albums)
			albumPos[ak] = ai
			bands[bi].AlbumCount++
			bands[bi].Albums = append(bands[bi].Albums,
				exportAlbum{ID: t.AlbumID, Host: t.Subdomain, Title: t.AlbumTitle, Slug: t.AlbumSlug, Tracks: make([]exportTrack, 0)})
		}
		bands[bi].Albums[ai].Tracks = append(bands[bi].Albums[ai].Tracks,
			exportTrack{ID: t.ID, Number: t.Number, Title: t.Title, Duration: float64(t.Duration) / float64(time.Second)})
		bands[bi].Albums[ai].TrackCount++
		bands[bi].TrackCount++
	}
	return bands
}

// bandKey identifies a band in the flat index: the band ID, falling back to
// the name when the ID is absent.
func bandKey(t contract.Track) string {
	if t.BandID != 0 {
		return "id:" + strconv.FormatInt(t.BandID, 10)
	}
	return "name:" + t.BandName
}

// formatDuration renders fractional seconds as m:ss, for the list's
// duration column; sub-second remainders are truncated (186.28 s reads
// 3:06, the way a clock does).
func formatDuration(seconds float64) string {
	s := int(seconds)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
