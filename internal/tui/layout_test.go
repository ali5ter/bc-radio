package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/ali5ter/bc-radio/internal/contract"
	"github.com/charmbracelet/colorprofile"
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
	if got := a.indexLine(); got != spinFrames[0]+" building track index…" {
		t.Errorf("indexLine(building) = %q; want the spinner line", got)
	}
	// The counter appears once the walk reports how far it is.
	a.indexDone, a.indexTotal = 9, 17
	if got := a.indexLine(); got != spinFrames[0]+" building track index… 9/17 bands" {
		t.Errorf("indexLine(building, 9/17) = %q; want the counter line", got)
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

// TestHeaderFits pins the wordmark header's geometry: every one of the six
// rows is exactly the terminal width — the art, the dash gap and the
// right-justified info block — so an overflowing title ellipses inside its
// budget instead of running off the edge. The volume readout is not among
// them: it reads once, in the status footer.
func TestHeaderFits(t *testing.T) {
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
	rows := a.playerHeader()
	if len(rows) != headerRows {
		t.Fatalf("playerHeader returned %d rows; want %d", len(rows), headerRows)
	}
	for i, row := range rows {
		if w, _ := lipgloss.Size(row); w != 80 {
			t.Fatalf("header row %d is %d cells; want exactly 80: %q", i, w, stripANSI(row))
		}
	}
	// The overflowing title ellipses inside its budget…
	if got := stripANSI(rows[1]); !strings.Contains(got, "…") {
		t.Errorf("an overflowing title should ellipsize, not cut: %q", got)
	}
	// …and the info block is pinned to the right edge: the title row's
	// final cell is the title's closing quote, with no trailing gap.
	if got := stripANSI(rows[1]); got[len(got)-1] != '\'' {
		t.Errorf("title row ends in %q; the info block must sit on the right edge: %q", got[len(got)-1], got)
	}
	// No header row carries a volume readout.
	for i, row := range rows {
		if got := stripANSI(row); strings.Contains(got, "vol ") || strings.Contains(got, "volume") {
			t.Errorf("header row %d carries a volume readout: %q", i, got)
		}
	}
}

// TestNarrowTitle pins the single-row fallback below the wordmark width:
// at the 30-column minimum the row holds the app name, the transport verb
// and the now-playing fields — and no volume (the footer's status tail
// reads it, so the row no longer doubles the readout).
func TestNarrowTitle(t *testing.T) {
	a := &App{
		width: 30,
		state: contract.State{
			Track:  contract.Track{ID: "A", BandName: "Some Band", Title: "Some Long Track Title"},
			Volume: 1,
		},
	}
	rows := a.playerHeader()
	if len(rows) != 1 {
		t.Fatalf("playerHeader at 30 cols returned %d rows; want the single-row title", len(rows))
	}
	if w, _ := lipgloss.Size(rows[0]); w != 30 {
		t.Fatalf("title row is %d cells; want exactly 30: %q", w, stripANSI(rows[0]))
	}
	got := stripANSI(rows[0])
	if strings.Contains(got, "vol ") || strings.Contains(got, "volume") {
		t.Fatalf("narrow title carries a volume readout; it moved to the status footer: %q", got)
	}
	if !strings.Contains(got, "bc-radio") {
		t.Fatalf("narrow title lost the app name: %q", got)
	}
}

// TestRenderStatusMuteHint pins the volume-zero hint: with a track loaded
// and the audio silenced, the standing status explains the blank visualizer
// (the analyser sits after the gain) instead of leaving it a mystery — and
// the hint holds the row against an *info* transient (the "volume 0%"
// confirmation is redundant with the vol readout in the title row) while a
// warn or bad transient keeps the row and the hint rides at its tail.
func TestRenderStatusMuteHint(t *testing.T) {
	a := &App{width: 80, playlist: newPlaylist(), indexState: indexReady}
	a.state = contract.State{Track: contract.Track{ID: "A", Title: "A"}, Volume: 0}
	a.playlist.SetIndex([]contract.Track{{ID: "a1"}})
	a.playlist.radio = true
	if got := stripANSI(a.renderStatus()); !strings.Contains(got, "muted: +/− raises the volume") {
		t.Fatalf("muted status = %q; want the turn-up hint", got)
	}
	// An info transient loses to the hint: it repeats what the title row
	// already says, while the hint is what the quiet visualizer needs.
	a.status = "volume 0%"
	got := stripANSI(a.renderStatus())
	if !strings.Contains(got, "muted: +/− raises the volume") {
		t.Fatalf("muted + info transient = %q; want the turn-up hint", got)
	}
	if strings.Contains(got, "volume 0%") {
		t.Fatalf("muted + info transient = %q; the redundant confirmation must not crowd the hint", got)
	}
	// A bad transient keeps the row; the hint rides at its tail.
	a.status = "playback failed: stream expired"
	a.statusKind = statusBad
	if got := stripANSI(a.renderStatus()); !strings.Contains(got, "playback failed: stream expired") || !strings.Contains(got, "muted: +/− raises the volume") {
		t.Fatalf("muted + bad transient = %q; want the alarm and the hint", got)
	}
	// No track loaded: nothing is playing, so there is no blank visual to
	// explain and the hint must not appear.
	a.status = ""
	a.statusKind = statusInfo
	a.state = contract.State{Volume: 0}
	if got := stripANSI(a.renderStatus()); strings.Contains(got, "muted") {
		t.Fatalf("no track: the mute hint must not show: %q", got)
	}
}

// TestRenderStatusFallback pins that the status row shows the transient
// status when set and the radio's standing line when not — and that the
// dim volume tail rides whole at the row's edge in both cases (the volume
// reads once, here, since the header carries no readout).
func TestRenderStatusFallback(t *testing.T) {
	a := &App{width: 80, playlist: newPlaylist()}
	a.status = "now: A — B"
	if got := stripANSI(a.renderStatus()); got != "now: A — B · volume 0%" {
		t.Errorf("renderStatus = %q; want the transient status with the volume tail", got)
	}
	a.status = ""
	a.indexState = indexReady
	a.playlist.SetIndex([]contract.Track{{ID: "a1", BandID: 1}})
	a.playlist.radio = true
	if got := stripANSI(a.renderStatus()); got != "◉ radio on · 1 tracks · 1 bands · volume 0%" {
		t.Errorf("renderStatus fallback = %q; want the standing radio line with the tail", got)
	}
	a.indexState = indexFailed
	if got := stripANSI(a.renderStatus()); got != "track index unavailable — the radio plays the queue only · volume 0%" {
		t.Errorf("renderStatus(failed) = %q; want the unavailable line with the tail", got)
	}
}

// TestStatusOfIndex pins the standing line's severity: only a failed walk is
// a hard failure (the radio has no pool); every other state reads quiet.
func TestStatusOfIndex(t *testing.T) {
	for s, want := range map[indexState]statusKind{
		indexIdle:     statusInfo,
		indexBuilding: statusInfo,
		indexReady:    statusInfo,
		indexFailed:   statusBad,
	} {
		if got := statusOfIndex(s); got != want {
			t.Errorf("statusOfIndex(%v) = %v; want %v", s, got, want)
		}
	}
}

// TestStatusSeverityStyles pins that each severity maps to its own colour of
// the resolved palette — the warning is its own hue, never the alarm and
// never the quiet. The explicit dark theme resolves the colour palette
// regardless of the terminal, so the comparison is deterministic.
func TestStatusSeverityStyles(t *testing.T) {
	a := &App{width: 80, playlist: newPlaylist(), theme: themeDark}
	a.profile = colorprofile.TrueColor
	a.bgDark = true
	a.applyThemeNow()

	if got := styleForStatus(statusInfo).GetForeground(); got != current.dim {
		t.Errorf("info style = %v; want the dim colour", got)
	}
	if got := styleForStatus(statusWarn).GetForeground(); got != current.warn {
		t.Errorf("warn style = %v; want the warn colour", got)
	}
	if got := styleForStatus(statusBad).GetForeground(); got != current.bad {
		t.Errorf("bad style = %v; want the bad colour", got)
	}
	if w := styleForStatus(statusWarn).GetForeground(); w == current.dim || w == current.bad {
		t.Errorf("the warn colour %v must be distinct from dim and bad", w)
	}
}

// TestStatusScreenScope pins the screen scoping of the transient status line:
// a confirmation raised on the player must not surface in the library title
// row (the "Library   volume 90%" leak), and vice versa — each row falls back
// to its own standing content.
func TestStatusScreenScope(t *testing.T) {
	a := &App{width: 80, playlist: newPlaylist(), indexState: indexReady, browser: newBrowser()}
	a.playlist.SetIndex([]contract.Track{{ID: "a1", BandID: 1}})
	a.playlist.radio = true
	a.status = "volume 90%"
	a.statusKind = statusInfo
	a.statusOn = screenPlayer

	// On the player the row shows the transient it was raised on.
	if got := stripANSI(a.renderStatus()); got != "volume 90%" {
		t.Errorf("player row = %q; want the transient", got)
	}

	// The user moves to the library (l): the title row must not inherit the
	// player confirmation (the "Library   volume 90%" leak) — it reads the
	// focused column's count instead.
	a.screen = screenLibrary
	if got := stripANSI(a.libraryStatus()); got != "0 artists" {
		t.Errorf("library title = %q; want the focused column's count, not a player transient", got)
	}

	// A transient raised on the library shows in the library row…
	a.statusOn = screenLibrary
	if got := stripANSI(a.libraryStatus()); got != "volume 90%" {
		t.Errorf("library title = %q; want the transient on its own screen", got)
	}
	// …and the player row falls back to the standing radio line (with the
	// volume tail: no track is loaded, so the mute branch does not apply).
	if got := stripANSI(a.renderStatus()); got != "◉ radio on · 1 tracks · 1 bands · volume 0%" {
		t.Errorf("player row = %q; want the standing radio line, not a foreign transient", got)
	}
}

// TestFitHints pins the bottom help row's priority trim: the row always
// fits the width, keeps its display order, and the quit/help hints are the
// floor nothing drops past. The regression it guards: at 80 columns the
// old static player row (123 cells) was MaxWidth-cut by place(), losing
// "l library", "+/- vol", "? help" and "q quit" past the terminal edge.
// The cells render in the house style — key in the accent, description in
// dim, no leading space — so the joined text is what the design shows.
func TestFitHints(t *testing.T) {
	player := []hint{
		{"space", "play/pause", 90},
		{"n", "next", 85},
		{"p", "prev", 70},
		{"s", "shuffle", 40},
		{"r", "repeat", 45},
		{"d", "radio", 80},
		{"v", "visual", 30},
		{"f", "fullscreen", 20},
		{"l", "library", 60},
		{"+/-", "vol", 35},
		{"?", "help", mustKeepHint},
		{"q", "quit", mustKeepHint},
	}
	cases := []struct {
		w    int
		keep []string
		drop []string
	}{
		// 80 columns: the convenience keys drop; the transport keys, the
		// radio, the library and the must-keep pair survive (78 cells).
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
	for _, want := range []string{"↑/↓ select", "←/→ column", "enter queue (starts if idle)", "l or esc back"} {
		if !strings.Contains(out, want) {
			t.Errorf("library help row lost %q: %q", want, out)
		}
	}
	if strings.Contains(out, "remove from queue") {
		t.Errorf("library help row at 80 cols should have dropped the remove hint: %q", out)
	}
}

// TestUpNextRadioHint pins the footer's up-next row when the queue is
// empty: the radio previews itself when it has a pool to draw from,
// otherwise the row points at the library — the one place tracks come
// from (and the one place space would not look).
func TestUpNextRadioHint(t *testing.T) {
	a := &App{width: 80, playlist: newPlaylist()}
	// Radio on with a non-empty index: the empty queue previews the radio.
	a.playlist.SetIndex([]contract.Track{{ID: "a1"}})
	a.playlist.radio = true
	if got := stripANSI(a.upNextRow()); !strings.Contains(got, "the radio plays next") {
		t.Fatalf("empty queue = %q; want the radio hint", got)
	}
	// Radio off: the library is where tracks come from.
	a.playlist.radio = false
	if got := stripANSI(a.upNextRow()); !strings.Contains(got, "open the library (l) to queue tracks") {
		t.Fatalf("empty queue, radio off = %q; want the library hint", got)
	}
	// Radio on but the index empty (still building): space would start
	// nothing, so the library hint stands instead of a false preview.
	a.playlist.radio = true
	a.playlist.SetIndex(nil)
	if got := stripANSI(a.upNextRow()); !strings.Contains(got, "open the library (l) to queue tracks") {
		t.Fatalf("empty queue, empty index = %q; want the library hint", got)
	}
}

// TestUpNext pins the up-next row with a queue: it shows only the head —
// the one track that plays next — not the old multi-track preview list.
func TestUpNext(t *testing.T) {
	a := &App{width: 80, playlist: newPlaylist()}
	a.playlist.Add(contract.Track{ID: "1", Title: "T1", BandName: "B1", AlbumTitle: "A1"})
	a.playlist.Add(contract.Track{ID: "2", Title: "T2", BandName: "B2"})
	got := stripANSI(a.upNextRow())
	if got != "└─ Up next: 'T1' by B1 from 'A1'" {
		t.Fatalf("up next = %q; want the queue head with band and album", got)
	}
	if strings.Contains(got, "T2") {
		t.Fatalf("up next shows only the head, not the whole queue: %q", got)
	}
}

// TestHeaderTimeBar pins the header's fourth row: the elapsed time, the
// filled/empty bar, the total and the percent. The bar takes exactly the
// cells the info block's budget leaves after the times, so the row fills
// the budget and never overruns the terminal edge.
func TestHeaderTimeBar(t *testing.T) {
	a := &App{
		width: 84,
		pos:   75 * time.Second,
		state: contract.State{
			Track: contract.Track{ID: "A", Title: "A", Duration: 300 * time.Second},
		},
	}
	got := stripANSI(a.headerTimeRow())
	for _, want := range []string{"1:15 / 5:00", "(25%)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("time bar missing %q: %q", want, got)
		}
	}
	// 75/300 of the 12-cell bar (the 33-cell budget minus the 21 cells of
	// times and percent) is 3 filled; the rest stays dim.
	if n := strings.Count(got, "━"); n != 3 {
		t.Errorf("bar has %d filled cells; want 3: %q", n, got)
	}
	if n := strings.Count(got, "─"); n != 9 {
		t.Errorf("bar has %d empty cells; want 9: %q", n, got)
	}
	if w, _ := lipgloss.Size(a.headerTimeRow()); w != a.headerMax() {
		t.Errorf("time bar is %d cells; want the full %d-cell budget: %q", w, a.headerMax(), got)
	}
	// An unknown duration shows only the elapsed time, no bar.
	a.state.Track.Duration = 0
	if got := stripANSI(a.headerTimeRow()); got != " ∿ 1:15" {
		t.Fatalf("unknown-duration time bar = %q; want the elapsed time only", got)
	}
}

// TestHeaderIdle is the first-run gap, now in the header's title row: the
// idle art is never a silent rectangle, so the row names what space would
// actually start — the queue beats the radio in Start, and the radio needs
// its index (space while the walk is building starts nothing). The narrow
// single-row title carries the same affordance.
func TestHeaderIdle(t *testing.T) {
	// Radio on with a non-empty index: the idle row names space.
	a := &App{width: 100, playlist: newPlaylist()}
	a.playlist.SetIndex([]contract.Track{{ID: "a1"}})
	a.playlist.radio = true
	if got := stripANSI(a.playerHeader()[1]); !strings.Contains(got, "nothing playing — press space to start the radio") {
		t.Fatalf("idle, radio on = %q; want the radio hint", got)
	}
	// Radio on but the index empty (still building): space would start
	// nothing, so the row points at the library instead.
	a = &App{width: 100, playlist: newPlaylist()}
	a.playlist.radio = true
	if got := stripANSI(a.playerHeader()[1]); !strings.Contains(got, "nothing playing — open the library (l)") {
		t.Fatalf("idle, radio on, no index = %q; want the library hint", got)
	}
	// The queue beats the radio in Start: it gets the hint instead.
	a = &App{width: 100, playlist: newPlaylist()}
	a.playlist.Add(contract.Track{ID: "1", Title: "T1"})
	if got := stripANSI(a.playerHeader()[1]); !strings.Contains(got, "nothing playing — press space to play the queue") {
		t.Fatalf("idle, queued = %q; want the queue hint", got)
	}
	// Below the wordmark width the same wording rides the single-row title.
	a = &App{width: 30, playlist: newPlaylist()}
	if got := stripANSI(a.playerHeader()[0]); !strings.Contains(got, "nothing playing") {
		t.Fatalf("narrow idle title = %q; want the affordance", got)
	}
}

// TestCheckOutRow pins the footer's second row: the playing track's band
// and its Bandcamp home, all dim, so a listener can find the release to
// support. Blank when nothing with a band is playing; a long band clips
// and the row stays at the terminal width.
func TestCheckOutRow(t *testing.T) {
	a := &App{width: 80}
	a.state.Track = contract.Track{Title: "T", BandName: "The Band", Subdomain: "theband.bandcamp.com"}
	if got := stripANSI(a.checkOutRow()); got != "├─ Check out The Band at https://theband.bandcamp.com/" {
		t.Fatalf("checkOutRow = %q; want the name and the link", got)
	}
	// No subdomain: the name stands alone, never a broken URL.
	a.state.Track.Subdomain = ""
	if got := stripANSI(a.checkOutRow()); got != "├─ Check out The Band" {
		t.Fatalf("checkOutRow = %q; want the name without a URL", got)
	}
	// Nothing with a band name: the row stays blank.
	a.state.Track.BandName = ""
	if got := stripANSI(a.checkOutRow()); got != "" {
		t.Fatalf("checkOutRow = %q; want blank without a band", got)
	}
	// A long band ellipses; the row never overruns the terminal.
	a.width = 50
	a.state.Track = contract.Track{Title: "T", BandName: strings.Repeat("B", 60)}
	got := stripANSI(a.checkOutRow())
	if w, _ := lipgloss.Size(got); w > 50 {
		t.Fatalf("checkOutRow is %d cells; must not overflow: %q", w, got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("an overflowing band should ellipsize: %q", got)
	}
}

// TestVolumeTail pins the status row's volume readout: it rides at the
// row's edge unless the mute hint already owns the row (volume zero with
// a track) or the row is the "volume N%" confirmation itself — the tail
// would then read "volume 90% · volume 90%". The readout survives a
// "volume" message raised on another screen or at another severity.
func TestVolumeTail(t *testing.T) {
	a := &App{width: 80, playlist: newPlaylist()}
	a.state.Volume = 0.9
	if got := stripANSI(a.volumeTail()); got != " · volume 90%" {
		t.Fatalf("volumeTail = %q; want the readout", got)
	}
	// Muted with a track: the mute hint owns the row; the tail would
	// repeat it.
	a.state = contract.State{Track: contract.Track{ID: "A", Title: "A"}, Volume: 0}
	if got := a.volumeTail(); got != "" {
		t.Fatalf("muted volumeTail = %q; want suppressed", stripANSI(got))
	}
	// Volume zero with no track: no hint to ride on, so the readout stays.
	a.state = contract.State{Volume: 0}
	if got := stripANSI(a.volumeTail()); got != " · volume 0%" {
		t.Fatalf("idle volumeTail = %q; want the readout", got)
	}
	// The confirmation itself: no doubled readout…
	a.state.Volume = 0.9
	a.status = "volume 90%"
	a.statusKind = statusInfo
	a.statusOn = screenPlayer
	if got := a.volumeTail(); got != "" {
		t.Fatalf("confirmation volumeTail = %q; want suppressed", stripANSI(got))
	}
	// …but the same wording raised on another screen keeps the tail.
	a.statusOn = screenLibrary
	if got := stripANSI(a.volumeTail()); got != " · volume 90%" {
		t.Fatalf("foreign-screen volumeTail = %q; want the readout", got)
	}
	// …and so does a "volume" message at a non-info severity.
	a.statusOn = screenPlayer
	a.statusKind = statusWarn
	if got := stripANSI(a.volumeTail()); got != " · volume 90%" {
		t.Fatalf("warn-severity volumeTail = %q; want the readout", got)
	}
}
