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

// TestBrowserPreviousRowDim pins the row above the selection: it must read
// dim like every other unselected row. An earlier design rendered it in
// the terminal's default colour, leaving a phantom highlight on the line
// above the bar (Alister's 2026-11-19 screenshot); the bar alone marks
// the position.
func TestBrowserPreviousRowDim(t *testing.T) {
	app, _, _ := newTestApp()
	b := app.browser
	b.artists = []contract.Artist{{Name: "Above"}, {Name: "Selected"}, {Name: "Below"}}
	b.selArtist, b.focus = 1, 0
	b.scroll = [3]int{}
	lines := strings.Split(app.browserColumn(0, 30, 3), "\n")
	// Line 0 is the column header; 1..3 are the data rows.
	if got := lines[1]; !strings.Contains(got, styleDim.Render("  Above")) {
		t.Errorf("the row above the selection must read dim like the other rows: %q", got)
	}
	if got := lines[3]; !strings.Contains(got, styleDim.Render("  Below")) {
		t.Errorf("a plain row reads dim: %q", got)
	}
	if got := lines[2]; !strings.Contains(got, styleAccentB.Render("→ Selected")) {
		t.Errorf("the selected row keeps its marker: %q", got)
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
	// A loaded track that is not playing is a paused radio: the line says
	// so, instead of claiming a playing radio over silence (2026-11-19).
	a.state = contract.State{Track: contract.Track{ID: "a1"}}
	if got := a.indexLine(); got != "◉ radio paused · 3 tracks · 2 bands" {
		t.Errorf("indexLine(ready, paused) = %q; want the paused line", got)
	}
	// Resumed: back to on.
	a.state.Playing = true
	if got := a.indexLine(); got != "◉ radio on · 3 tracks · 2 bands" {
		t.Errorf("indexLine(ready, resumed) = %q; want the on line", got)
	}
	a.state = contract.State{} // idle: the previous cases left a loaded track
	a.playlist.radio = false
	// Idle, off, empty queue: the app is quiet, so the line states what
	// will start it — the library (2026-11-19 re-test: Alister's
	// "play from library").
	if got := a.indexLine(); got != "○ radio off — l opens the library · 3 tracks · 2 bands" {
		t.Errorf("indexLine(ready, off) = %q; want the off line with its action", got)
	}
	// …or the queue, if one was built (space from idle starts its head).
	a.playlist.queue = []contract.Track{{ID: "q1"}}
	if got := a.indexLine(); got != "○ radio off — space plays the queue · 3 tracks · 2 bands" {
		t.Errorf("indexLine(ready, off, queued) = %q; want the queue action", got)
	}
	// With a track loaded the line is the plain state: off means the app
	// stops picking after it, and the up-next row says what comes next.
	a.state = contract.State{Track: contract.Track{ID: "a1"}}
	if got := a.indexLine(); got != "○ radio off · 3 tracks · 2 bands" {
		t.Errorf("indexLine(ready, off, loaded) = %q; want the plain off line", got)
	}
	// The on glyph needs a non-empty index: a radio on with nothing to draw
	// from is shown off, so the line is honest (hint included — the
	// library is the only source left).
	a.state = contract.State{}
	a.playlist.queue = nil
	a.playlist.radio = true
	a.playlist.SetIndex(nil)
	if got := a.indexLine(); got != "○ radio off — l opens the library · 0 tracks · 0 bands" {
		t.Errorf("indexLine(ready, on, empty) = %q; want the off line (no index)", got)
	}
}

// TestHeaderFits pins the wordmark header's geometry: every one of the six
// rows is exactly the terminal width — the art, the dash gap and the
// info block, left-aligned at the block's fixed column (issue #13) — so
// an overflowing title ellipses inside its budget instead of running off
// the edge. The volume readout is not among them: it reads once, in the
// status footer.
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
	// The info block hangs from the wordmark's left edge across the dash
	// gap — the title row reads art, gap, verb, title, and a right-
	// justified block would instead show dash fill between them.
	if got := stripANSI(rows[1]); !strings.HasPrefix(got, wordmarkR1+"─────") {
		t.Errorf("title row must begin with the wordmark and its gap: %q", got)
	}
	// An overflowing title ellipses inside its budget…
	if got := stripANSI(rows[1]); !strings.Contains(got, "…") {
		t.Errorf("an overflowing title should ellipsize, not cut: %q", got)
	}
	// …and a short one stops where it ends: the row's remaining cells are
	// quiet negative space, closing on the two-dash tail at the far right
	// (the box's right edge), never dash fill or content reaching the edge.
	a.state.Track.Title = "Short"
	if got := stripANSI(a.playerHeader()[1]); !strings.HasSuffix(got, "──") {
		t.Errorf("short-title row must close on the two-dash tail: %q", got)
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
	// A transient is a brief hint on its own background, padded full row
	// width (issue #14): compare the text, not the padding.
	a.status = "now: A — B"
	if got := strings.TrimRight(stripANSI(a.renderStatus()), " "); got != "now: A — B · volume 0%" {
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

	// On the player the row shows the transient it was raised on, on its
	// full-row background (compare the text, not the padding).
	if got := strings.TrimRight(stripANSI(a.renderStatus()), " "); got != "volume 90%" {
		t.Errorf("player row = %q; want the transient", got)
	}

	// The user moves to the library (l): the title row must not inherit the
	// player confirmation (the "Library   volume 90%" leak) — it reads the
	// section title instead.
	a.screen = screenLibrary
	if got := stripANSI(a.libraryTitleRow()); got != "Library (0 artists)" {
		t.Errorf("library title = %q; want the section title, not a player transient", got)
	}

	// A transient raised on the library shows in the library row…
	a.statusOn = screenLibrary
	if got := stripANSI(a.libraryTitleRow()); got != "volume 90%" {
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

// TestRenderHelp pins the player's hint row layout (issue #14): the
// essentials read left, quit among them, and ? help sits at the right
// edge, apart from them; the row is exactly the terminal width at every
// size.
func TestRenderHelp(t *testing.T) {
	cases := []struct {
		w    int
		keep []string
	}{
		// 80 columns: all six essentials fit in the left group, apart from
		// the right-aligned help.
		{80, []string{"space play/pause", "d radio", "l library", "v visual", "f fullscreen", "q quit", "? help"}},
		// 30 columns (the player minimum): the essentials fall away one by
		// one; quit survives beside help.
		{30, []string{"q quit", "? help"}},
	}
	for _, tc := range cases {
		a := &App{width: tc.w}
		got := stripANSI(a.renderHelp())
		if w, _ := lipgloss.Size(a.renderHelp()); w != tc.w {
			t.Errorf("renderHelp at %d cols is %d cells; want exactly %d: %q", tc.w, w, tc.w, got)
		}
		for _, s := range tc.keep {
			if !strings.Contains(got, s) {
				t.Errorf("renderHelp(%d) lost %q: %q", tc.w, s, got)
			}
		}
		// The dropped transport keys are not advertised on this row
		// (they are in the help overlay); help ends the row.
		if !strings.HasSuffix(got, " ? help") && !strings.HasSuffix(got, "? help") {
			t.Errorf("renderHelp(%d) must end in the help hint: %q", tc.w, got)
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

// TestUpNextBlank pins the footer's up-next row when nothing can be
// known: no queue head, no prepared draw — the row is a quiet blank line
// (the 2026-11-19 follow-up: the old pointers read as noise), and the
// same blank holds with the radio off and with an empty index.
func TestUpNextBlank(t *testing.T) {
	a := &App{width: 80, playlist: newPlaylist()}
	// Radio on with a non-empty index: no prepared draw yet, no queue.
	a.playlist.SetIndex([]contract.Track{{ID: "a1"}})
	a.playlist.radio = true
	if got := stripANSI(a.upNextRow()); got != "" {
		t.Fatalf("empty queue = %q; want a blank row", got)
	}
	// Radio off: nothing the row can promise.
	a.playlist.radio = false
	if got := stripANSI(a.upNextRow()); got != "" {
		t.Fatalf("empty queue, radio off = %q; want a blank row", got)
	}
	// Radio on but the index empty (still building): no pool, no preview.
	a.playlist.radio = true
	a.playlist.SetIndex(nil)
	if got := stripANSI(a.upNextRow()); got != "" {
		t.Fatalf("empty queue, empty index = %q; want a blank row", got)
	}
	// A prepared draw previews the real track it will play — the row
	// promises A and the next slot plays A, never a different B.
	a = &App{width: 80, playlist: newPlaylist()}
	a.playlist.SetIndex([]contract.Track{{ID: "a1", Title: "Radio Track", BandName: "RB", AlbumTitle: "RA"}})
	a.playlist.radio = true
	a.playlist.prepareRadioNext()
	if got := stripANSI(a.upNextRow()); got != "└─ Up next: 'Radio Track' by RB on 'RA'" {
		t.Fatalf("prepared radio draw = %q; want the previewed track", got)
	}
}

// TestUpNext pins the up-next row with a queue: it shows only the head —
// the one track that plays next — not the old multi-track preview list.
func TestUpNext(t *testing.T) {
	a := &App{width: 80, playlist: newPlaylist()}
	a.playlist.Add(contract.Track{ID: "1", Title: "T1", BandName: "B1", AlbumTitle: "A1"})
	a.playlist.Add(contract.Track{ID: "2", Title: "T2", BandName: "B2"})
	got := stripANSI(a.upNextRow())
	if got != "└─ Up next: 'T1' by B1 on 'A1'" {
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
	// 75/300 of the 7-cell bar (the 29-cell content budget minus the 15
	// cells of times, the 6 of the percent and the 1 of the space before
	// the tail) is 2 filled; the remaining 5 dashes are the bar's empty
	// cells.
	if n := strings.Count(got, "━"); n != 2 {
		t.Errorf("bar has %d filled cells; want 2: %q", n, got)
	}
	if n := strings.Count(got, "─"); n != 5 {
		t.Errorf("bar has %d empty cells; want 5: %q", n, got)
	}
	// The percent stops a space short of the tail: the row's last cell
	// before the block's tail is that space.
	if !strings.HasSuffix(got, " (25%) ") {
		t.Errorf("time row should end with the percent and a space: %q", got)
	}
	if w, _ := lipgloss.Size(a.headerTimeRow()); w != a.headerMax()-2 {
		t.Errorf("time bar is %d cells; want the %d-cell budget minus the tail: %q", w, a.headerMax()-2, got)
	}
	// Assembled in the block, the row closes with the block's standard
	// two-dash tail like the other info rows: exactly two dashes at the
	// edge, never the old doubled four (2026-11-19 screenshot).
	row := stripANSI(a.headerInfoRow(4, a.headerTimeRow()))
	trimmed := strings.TrimRight(row, "─")
	if tail := row[len(trimmed):]; tail != "──" {
		t.Errorf("time row tail = %q; want exactly two dashes: %q", tail, row)
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
	// Radio on with a non-empty index: the idle row names space. (The full
	// hint is 48 cells against a 47-cell budget at 100 columns, so the
	// final word ellipses at exactly 100; pin the stable prefix, which
	// carries the affordance.)
	a := &App{width: 100, playlist: newPlaylist()}
	a.playlist.SetIndex([]contract.Track{{ID: "a1"}})
	a.playlist.radio = true
	if got := stripANSI(a.playerHeader()[1]); !strings.Contains(got, "nothing playing — press space to start") {
		t.Fatalf("idle, radio on = %q; want the radio hint", got)
	}
	// Radio on but the index empty (still building): space would start
	// nothing, so the row points at the library instead.
	a = &App{width: 100, playlist: newPlaylist()}
	a.playlist.radio = true
	if got := stripANSI(a.playerHeader()[1]); !strings.Contains(got, "nothing playing — open the library (l)") {
		t.Fatalf("idle, radio on, no index = %q; want the library hint", got)
	}
	// The queue beats the radio in Start: it gets the hint instead. (The
	// full hint is 47 cells against the 44-cell budget at 100 columns, so
	// the final word ellipses; pin the stable prefix, which carries the
	// affordance — the hint reads on one line: a leading space, then the
	// words, at the block's fixed column.)
	a = &App{width: 100, playlist: newPlaylist()}
	a.playlist.Add(contract.Track{ID: "1", Title: "T1"})
	if got := stripANSI(a.playerHeader()[1]); !strings.Contains(got, "nothing playing — press space to play") {
		t.Fatalf("idle, queued = %q; want the queue hint", got)
	}
	// The leading space holds at every width: the hint sits where the
	// playing rows' verbs sit, never butting the dash gap.
	if got := stripANSI(a.playerHeader()[1]); !strings.Contains(got, "───── ") {
		t.Fatalf("idle row must carry the space after the gap: %q", got)
	}
	// Below the wordmark width the same wording rides the single-row title.
	a = &App{width: 30, playlist: newPlaylist()}
	if got := stripANSI(a.playerHeader()[0]); !strings.Contains(got, "nothing playing") {
		t.Fatalf("narrow idle title = %q; want the affordance", got)
	}
}

// TestMergeRows pins the help overlay's cell merge (issue #19): the
// overwriting row's cells win inside its rectangle, the base keeps the
// rest, and a style code is emitted only on a state change.
func TestMergeRows(t *testing.T) {
	base := strings.Repeat("x", 20)
	over := strings.Repeat("o", 5)
	want := "xxx" + strings.Repeat("o", 5) + strings.Repeat("x", 12)
	if got := mergeRows(base, over, 3, 5); got != want {
		t.Fatalf("mergeRows = %q; want %q", got, want)
	}
	// Styled over, plain base: the accent code opens before the box's cells
	// and resets after them — exactly two codes, never per-cell.
	styled := styleAccent.Render("ab")
	got := mergeRows(strings.Repeat("c", 6), styled, 1, 2)
	if g := stripANSI(got); g != "cabccc" {
		t.Fatalf("mergeRows = %q; want the base around the box", g)
	}
	if n := strings.Count(got, "\x1b["); n != 2 {
		t.Errorf("mergeRows emits %d SGR codes; want 2 (open + reset): %q", n, got)
	}
}

// TestComposeHelp pins the overlay placement (issue #19): the box centres
// over the base, rows above and below the box keep the base verbatim, and
// the rows inside the box's rectangle show the box over the base.
func TestComposeHelp(t *testing.T) {
	a := &App{width: 30, height: 10}
	baseLines := make([]string, 10)
	for i := range baseLines {
		baseLines[i] = strings.Repeat("z", 30)
	}
	base := strings.Join(baseLines, "\n")
	box := "╭──╮\n│ab│\n╰──╯" // 4 wide, 3 tall
	lines := strings.Split(a.composeHelp(base, box), "\n")
	if len(lines) != 10 {
		t.Fatalf("composeHelp gives %d rows; want 10", len(lines))
	}
	if lines[2] != strings.Repeat("z", 30) {
		t.Errorf("row above the box is not the base: %q", lines[2])
	}
	// Centred: top = (10-3)/2 = 3, left = (30-4)/2 = 13.
	want := strings.Repeat("z", 13) + "│ab│" + strings.Repeat("z", 13)
	if lines[4] != want {
		t.Errorf("box row = %q; want the box centred over the base", lines[4])
	}
	if lines[9] != strings.Repeat("z", 30) {
		t.Errorf("row below the box is not the base: %q", lines[9])
	}
}

// TestCheckOutRow pins the footer's second row: the playing track's band
// and its Bandcamp home, so a listener can find the release to support.
// The name and the URL carry the accent, the " at " between them stays
// quiet; blank when the band or its home is unknown — a name without a
// URL points nowhere, so the row stays quiet; a long band clips and the
// row stays at the terminal width.
func TestCheckOutRow(t *testing.T) {
	a := &App{width: 80}
	a.state.Track = contract.Track{Title: "T", BandName: "The Band", Subdomain: "theband.bandcamp.com"}
	if got := stripANSI(a.checkOutRow()); got != "├─ Check out The Band at https://theband.bandcamp.com/" {
		t.Fatalf("checkOutRow = %q; want the name and the link", got)
	}
	// The " at " separator reads quiet: only the name and the URL are
	// highlighted (compare the SGR, not the glyphs).
	if !strings.Contains(a.checkOutRow(), styleDim.Render(" at ")) {
		t.Fatalf("the ' at ' separator must carry the dim style, not the accent: %q", a.checkOutRow())
	}
	// No subdomain: the row is blank, never a name without a URL.
	a.state.Track.Subdomain = ""
	if got := stripANSI(a.checkOutRow()); got != "" {
		t.Fatalf("checkOutRow = %q; want blank without a URL", got)
	}
	// Nothing with a band name: the row stays blank.
	a.state.Track.BandName = ""
	if got := stripANSI(a.checkOutRow()); got != "" {
		t.Fatalf("checkOutRow = %q; want blank without a band", got)
	}
	// A long band ellipses; the row never overruns the terminal.
	a.width = 50
	a.state.Track = contract.Track{Title: "T", BandName: strings.Repeat("B", 60), Subdomain: "theband.bandcamp.com"}
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
