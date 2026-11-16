package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/ali5ter/bc-radio/internal/contract"
)

func TestFormatDuration(t *testing.T) {
	cases := map[int]string{
		0:    "0:00",
		59:   "0:59",
		60:   "1:00",
		305:  "5:05",
		3599: "59:59",
		-3:   "0:00",
	}
	for in, want := range cases {
		if got := formatDuration(in); got != want {
			t.Errorf("formatDuration(%d) = %q; want %q", in, got, want)
		}
	}
}

func TestClip(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"abcdefgh", 5, "abcd…"},
		{"abcdefgh", 2, "a…"},
		{"abcdefgh", 1, "a"},
		{"abcdefgh", 0, ""},
	}
	for _, tc := range cases {
		if got := clip(tc.in, tc.n); got != tc.want {
			t.Errorf("clip(%q, %d) = %q; want %q", tc.in, tc.n, got, tc.want)
		}
	}
}

func TestPlace(t *testing.T) {
	// Short lines are padded to exactly w.
	if got := place(10, "abc"); got != "abc       " {
		t.Fatalf("place(10, %q) = %q; want padding to 10", got, got)
	}
	// Long lines are truncated (PlaceHorizontal alone would not clip).
	if got := stripANSI(place(10, strings.Repeat("x", 20))); got != strings.Repeat("x", 10) {
		t.Fatalf("place(10, 20x) = %q; want 10 x's", got)
	}
	// Every line of a multi-line block lands at exactly w.
	for i, l := range strings.Split(place(5, "abcd\nefghij"), "\n") {
		if w, _ := lipgloss.Size(l); w != 5 {
			t.Fatalf("line %d of place output is %d cells; want 5: %q", i, w, l)
		}
	}
}

// TestBrowserColumnsAreIndependent pins the per-column rendering: each
// column shows its own row count and its own selection marker, never the
// focused column's values leaking in (the regression made all three
// headers read the focused column's count and stamped phantom markers in
// the other two columns wherever that selection happened to sit).
func TestBrowserColumnsAreIndependent(t *testing.T) {
	app, _, _ := newTestApp()
	b := app.browser
	b.artists = make([]contract.Artist, 12)
	for i := range b.artists {
		b.artists[i] = contract.Artist{Name: fmt.Sprintf("Artist %d", i)}
	}
	b.albums = []contract.Album{{Title: "One"}, {Title: "Two"}, {Title: "Three"}}
	b.tracks = []contract.Track{
		{Title: "T1"}, {Title: "T2"}, {Title: "T3"},
		{Title: "T4"}, {Title: "T5"}, {Title: "T6"},
	}
	b.selArtist, b.selAlbum, b.selTrack = 11, 2, 5
	b.focus = 2 // focused on the tracks column
	b.scroll = [3]int{}
	app.screen = screenLibrary
	app.status = ""

	got := stripANSI(app.viewLibrary())
	for _, want := range []string{"Artists (12)", "Albums (3)", "Tracks (6)"} {
		if !strings.Contains(got, want) {
			t.Errorf("library view missing header %q;\n%s", want, got)
		}
	}
	// One marker per column, on each column's own selected row.
	for _, want := range []string{"→ Artist 11", "→ Three", "→ T6"} {
		if !strings.Contains(got, want) {
			t.Errorf("library view missing %q;\n%s", want, got)
		}
	}
}

func TestIndexLine(t *testing.T) {
	a := &App{playlist: newPlaylist()}
	// The not-yet-ready states (before the follows land, and while the walk
	// runs) both read "building": the index does not exist until it does.
	a.indexState = indexIdle
	if got := a.indexLine(); got != "building track index…" {
		t.Errorf("indexLine(idle) = %q; want the building line", got)
	}
	a.indexState = indexBuilding
	if got := a.indexLine(); got != "building track index…" {
		t.Errorf("indexLine(building) = %q; want the building line", got)
	}
	// A failed walk reads unavailable (renderStatus colours it bad).
	a.indexState = indexFailed
	if got := a.indexLine(); got != "track index unavailable — the radio plays the queue only" {
		t.Errorf("indexLine(failed) = %q; want the unavailable line", got)
	}
	// Ready: the radio's state and the size of the index it draws from.
	a.indexState = indexReady
	a.playlist.SetIndex([]contract.Track{{ID: "a1", BandID: 1}, {ID: "a2", BandID: 1}, {ID: "b1", BandID: 2}})
	a.playlist.radio = true
	if got := a.indexLine(); got != "◉ radio on · 3 tracks · 2 bands" {
		t.Errorf("indexLine(ready, on) = %q; want the on line", got)
	}
	a.playlist.radio = false
	if got := a.indexLine(); got != "○ radio off · 3 tracks · 2 bands" {
		t.Errorf("indexLine(ready, off) = %q; want the off line", got)
	}
	// The on glyph needs a non-empty index: a radio on with nothing to draw
	// from is shown off, so the line is honest.
	a.playlist.radio = true
	a.playlist.SetIndex(nil)
	if got := a.indexLine(); got != "○ radio off · 0 tracks · 0 bands" {
		t.Errorf("indexLine(ready, on, empty) = %q; want the off line (no index)", got)
	}
}

// TestRenderTitleFits pins the F9 fix: the now-playing fields share the row
// and clip inside their share, so a long title ellipses in the middle of the
// row instead of pushing the volume readout past the terminal edge.
func TestRenderTitleFits(t *testing.T) {
	a := &App{
		width: 80,
		state: contract.State{
			Track: contract.Track{
				ID:         "A",
				BandName:   "The Extraordinarily Longest Band Name In The World",
				Title:      "An Absolutely Unreasonably Long Track Title For Terminals",
				AlbumTitle: "Album Title That Also Exceeds Its Allotted Share",
			},
			Volume: 1,
		},
	}
	row := a.renderTitle()
	if w, _ := lipgloss.Size(row); w != 80 {
		t.Fatalf("title row is %d cells; want exactly 80: %q", w, stripANSI(row))
	}
	got := stripANSI(row)
	if !strings.Contains(got, "vol 100%") {
		t.Fatalf("title row lost the volume readout at 80 cols: %q", got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("an overflowing name should ellipsize, not cut: %q", got)
	}
}

// The readout survives the narrow end too: at the 30-column minimum the
// name fields shrink to nothing before the volume is cut.
func TestRenderTitleNarrow(t *testing.T) {
	a := &App{
		width: 30,
		state: contract.State{
			Track:  contract.Track{ID: "A", BandName: "Some Band", Title: "Some Long Track Title"},
			Volume: 1,
		},
	}
	row := a.renderTitle()
	if w, _ := lipgloss.Size(row); w != 30 {
		t.Fatalf("title row is %d cells; want exactly 30: %q", w, stripANSI(row))
	}
	if got := stripANSI(row); !strings.Contains(got, "vol 100%") {
		t.Fatalf("title row lost the volume readout at 30 cols: %q", got)
	}
}

// TestRenderStatusMuteHint pins the volume-zero hint: with a track loaded
// and the audio silenced, the standing status explains the blank visualizer
// (the analyser sits after the gain) instead of leaving it a mystery.
func TestRenderStatusMuteHint(t *testing.T) {
	a := &App{width: 80, playlist: newPlaylist(), indexState: indexReady}
	a.state = contract.State{Track: contract.Track{ID: "A", Title: "A"}, Volume: 0}
	a.playlist.SetIndex([]contract.Track{{ID: "a1"}})
	a.playlist.radio = true
	if got := stripANSI(a.renderStatus()); !strings.Contains(got, "muted: +/− raises the volume") {
		t.Fatalf("muted status = %q; want the turn-up hint", got)
	}
	// A transient status wins over the hint.
	a.status = "loading"
	if got := stripANSI(a.renderStatus()); got != "loading" {
		t.Fatalf("transient status must win over the mute hint: %q", got)
	}
	// No track loaded: nothing is playing, so there is no blank visual to
	// explain and the hint must not appear.
	a.status = ""
	a.state = contract.State{Volume: 0}
	if got := stripANSI(a.renderStatus()); strings.Contains(got, "muted") {
		t.Fatalf("no track: the mute hint must not show: %q", got)
	}
}

// TestRenderStatusFallback pins that the status row shows the transient
// status when set and the radio's standing line when not.
func TestRenderStatusFallback(t *testing.T) {
	a := &App{width: 80, playlist: newPlaylist()}
	a.status = "now: A — B"
	if got := stripANSI(a.renderStatus()); got != "now: A — B" {
		t.Errorf("renderStatus = %q; want the transient status", got)
	}
	a.status = ""
	a.indexState = indexReady
	a.playlist.SetIndex([]contract.Track{{ID: "a1", BandID: 1}})
	a.playlist.radio = true
	if got := stripANSI(a.renderStatus()); got != "◉ radio on · 1 tracks · 1 bands" {
		t.Errorf("renderStatus fallback = %q; want the standing radio line", got)
	}
	a.indexState = indexFailed
	if got := stripANSI(a.renderStatus()); got != "track index unavailable — the radio plays the queue only" {
		t.Errorf("renderStatus(failed) = %q; want the unavailable line", got)
	}
}

// TestFitHints pins the bottom help row's priority trim: the row always
// fits the width, keeps its display order, and the quit/help hints are the
// floor nothing drops past. The regression it guards: at 80 columns the
// old static player row (123 cells) was MaxWidth-cut by place(), losing
// "l library", "+/− vol", "? help" and "q quit" past the terminal edge.
func TestFitHints(t *testing.T) {
	player := []hint{
		{"space play/pause", 90},
		{"n next", 85},
		{"p prev", 70},
		{"s shuffle", 40},
		{"r repeat", 45},
		{"d radio", 80},
		{"v visual", 30},
		{"f fullscreen", 20},
		{"l library", 60},
		{"+/− vol", 35},
		{"? help", mustKeepHint},
		{"q quit", mustKeepHint},
	}
	cases := []struct {
		w    int
		keep []string
		drop []string
	}{
		// 80 columns: the convenience keys drop; the transport keys, the
		// radio, the library and the must-keep pair survive (79 cells).
		{80,
			[]string{"space play/pause", "n next", "p prev", "r repeat", "d radio", "l library", "? help", "q quit"},
			[]string{"shuffle", "visual", "fullscreen", "vol"}},
		// 60 columns: the library hint drops next.
		{60,
			[]string{"space play/pause", "d radio", "? help", "q quit"},
			[]string{"l library"}},
		// 30 columns (the player minimum): only quit and help survive.
		{30,
			[]string{"? help", "q quit"},
			[]string{"space play/pause", "d radio"}},
		// Below even the must-keep set: clipped, never wider than w.
		{5, nil, nil},
	}
	for _, tc := range cases {
		out := stripANSI(fitHints(tc.w, "  ", player...))
		if w, _ := lipgloss.Size(out); w > tc.w {
			t.Errorf("fitHints(%d) is %d cells wide; must not exceed: %q", tc.w, w, out)
		}
		for _, s := range tc.keep {
			if !strings.Contains(out, s) {
				t.Errorf("fitHints(%d) dropped %q: %q", tc.w, s, out)
			}
		}
		for _, s := range tc.drop {
			if strings.Contains(out, s) {
				t.Errorf("fitHints(%d) kept %q; it should have dropped: %q", tc.w, s, out)
			}
		}
	}
}

// TestFitHintsLibrary pins the library row's narrower trade: at 80 columns
// the "x remove from queue" hint drops (it is in the help overlay) so the
// back-navigation hint displays in full instead of being cut to "l or esc b".
func TestFitHintsLibrary(t *testing.T) {
	a := &App{width: 80}
	out := stripANSI(a.renderLibraryHelp())
	for _, want := range []string{"↑/↓ select", "←/→ column", "enter open / queue", "l or esc back"} {
		if !strings.Contains(out, want) {
			t.Errorf("library help row lost %q: %q", want, out)
		}
	}
	if strings.Contains(out, "remove from queue") {
		t.Errorf("library help row at 80 cols should have dropped the remove hint: %q", out)
	}
}

func TestRenderQueueRadioHint(t *testing.T) {
	a := &App{width: 80, playlist: newPlaylist()}
	// Radio on with a non-empty index: the empty queue previews the radio.
	a.playlist.SetIndex([]contract.Track{{ID: "a1"}})
	a.playlist.radio = true
	if got := stripANSI(a.renderQueue()); !strings.Contains(got, "the radio plays next") {
		t.Fatalf("empty queue = %q; want the radio hint", got)
	}
	// Radio off (or empty index): the old add-tracks hint.
	a.playlist.radio = false
	if got := stripANSI(a.renderQueue()); !strings.Contains(got, "add tracks") {
		t.Fatalf("empty queue, radio off = %q; want the add-tracks hint", got)
	}
	a.playlist.radio = true
	a.playlist.SetIndex(nil)
	if got := stripANSI(a.renderQueue()); !strings.Contains(got, "add tracks") {
		t.Fatalf("empty queue, empty index = %q; want the add-tracks hint", got)
	}
}

func TestRenderQueue(t *testing.T) {
	a := &App{width: 80, playlist: newPlaylist()}
	// The radio is on by default but the index is empty here, so the empty
	// queue still previews the add-tracks hint, not the radio.
	if got := stripANSI(a.renderQueue()); !strings.Contains(got, "add tracks") {
		t.Fatalf("empty queue preview = %q; want the add-tracks hint", got)
	}
	a.playlist.Add(contract.Track{ID: "1", Title: "T1", BandName: "B1"})
	a.playlist.Add(contract.Track{ID: "2", Title: "T2", BandName: "B2"})
	a.playlist.Add(contract.Track{ID: "3", Title: "T3", BandName: "B3"})
	a.playlist.Add(contract.Track{ID: "4", Title: "T4", BandName: "B4"})
	got := stripANSI(a.renderQueue())
	for _, want := range []string{"T1", "T2", "T3", "+1 more"} {
		if !strings.Contains(got, want) {
			t.Errorf("queue preview missing %q: %q", want, got)
		}
	}
}

func TestRenderProgress(t *testing.T) {
	a := &App{
		width: 80,
		pos:   75 * time.Second,
		state: contract.State{
			Track: contract.Track{ID: "A", Title: "A", Duration: 300 * time.Second},
		},
	}
	got := stripANSI(a.renderProgress())
	for _, want := range []string{"1:15", "5:00"} {
		if !strings.Contains(got, want) {
			t.Errorf("progress missing %q: %q", want, got)
		}
	}

	// Idle: just the waiting marker.
	a = &App{width: 80}
	if got := stripANSI(a.renderProgress()); !strings.Contains(got, "waiting") {
		t.Errorf("idle progress = %q; want waiting", got)
	}
}
