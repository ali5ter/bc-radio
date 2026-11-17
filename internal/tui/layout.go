package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// fixedRows is the number of rows outside the visualizer the player screen
// reserves: title, separator, status, progress, queue, help.
const fixedRows = 6

// spinFrames is the braille spinner the building line cycles through on the
// render tick: steady motion, so a cold build never reads as a hung screen.
var spinFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// formatDuration renders a duration as m:ss, the radio shorthand.
func formatDuration(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

// clip truncates s to n display cells (rough: one cell per rune), adding an
// ellipsis rune when it cuts. Good enough for terminal lines of names.
func clip(s string, n int) string {
	if n < 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n < 2 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// place normalizes each line of s to exactly w cells: short lines are padded,
// long ones truncated. Lip Gloss's PlaceHorizontal only pads, and its Width
// option wraps rather than cuts, so neither alone keeps a line at its region
// width; this enforces the invariant the layout relies on (a frame wider than
// its allotment would tear the screen).
func place(w int, s string) string {
	if w < 1 {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if lipgloss.Width(l) > w {
			l = lipgloss.NewStyle().MaxWidth(w).Render(l)
		}
		lines[i] = lipgloss.PlaceHorizontal(w, lipgloss.Left, l)
	}
	return strings.Join(lines, "\n")
}

// viewPlayer assembles the player screen: title, separator, the visualizer
// region, then status, progress, queue and help — or just title plus
// visualizer in fullscreen.
func (a *App) viewPlayer() string {
	w, h := a.width, a.height
	if w < 30 || h < 8 {
		// Short on purpose: below the 30-column minimum the notice has to
		// fit the very terminal it is apologising for, so it names the
		// app and the requirement ("bc-radio needs 30 × 8") and nothing
		// else — the longer wording ellipsised its own tail at 25 columns.
		return tooSmall(w, h, "bc-radio needs 30 × 8")
	}
	// Fullscreen keeps the title and separator rows, so the visualizer gets
	// everything but those two.
	visH := h - fixedRows
	if a.fullscreen {
		visH = h - 2
	}
	if visH < 1 {
		visH = 1
	}
	lines := []string{
		a.renderTitle(),
		place(w, styleDim.Render(strings.Repeat("─", w))),
		place(w, a.visualizers[a.visIdx].Render(frame{bands: a.bands, scope: a.scope, w: w, h: visH})),
	}
	if !a.fullscreen {
		lines = append(lines,
			place(w, a.renderStatus()),
			place(w, a.renderProgress()),
			place(w, a.renderQueue()),
			place(w, a.renderHelp()),
		)
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// titleFixed is the title row's fixed cells: the app name with its gap, the
// transport verb (2 only when a track is loaded), and the volume readout.
// The name fields get whatever is left, so the volume can never be the part
// that is cut at the terminal edge (F9).
func titleFixed(withTrack bool) int {
	n := 11 + 11 // "bc-radio   " + "   vol %3d%%"
	if withTrack {
		n += 2 // "▶ " or "⏸ "
	}
	return n
}

// renderTitle is the top row: app name, transport glyph, now playing,
// volume. The now-playing fields are allocated a share of the middle and
// clipped inside their share (the way the library columns clip), so a long
// title ellipses in the middle of the row instead of silently costing the
// volume readout at the edge.
func (a *App) renderTitle() string {
	st := a.state
	var sb strings.Builder
	sb.WriteString(styleAccentB.Render("bc-radio"))
	sb.WriteString(styleDim.Render("   "))
	if st.Track.Title != "" {
		verb := "▶"
		if !st.Playing {
			verb = "⏸"
		}
		sb.WriteString(styleAccentB.Render(verb + " "))
		// band : title : album as 2 : 3 : 2 of what the separators leave —
		// the title is the field that overflows in practice, so it gets
		// the largest share.
		mid := a.width - titleFixed(true)
		if mid < 6 {
			mid = 6
		}
		if st.Track.AlbumTitle != "" {
			cw := mid - 6 // " — " and " · "
			if cw < 3 {
				cw = 3
			}
			bw := cw * 2 / 7
			tw := cw * 3 / 7
			aw := cw - bw - tw
			sb.WriteString(styleText.Render(clip(st.Track.BandName, bw)))
			sb.WriteString(styleDim.Render(" — "))
			sb.WriteString(styleAccent.Render(clip(st.Track.Title, tw)))
			sb.WriteString(styleDim.Render(" · " + clip(st.Track.AlbumTitle, aw)))
		} else {
			cw := mid - 3 // " — "
			if cw < 3 {
				cw = 3
			}
			bw := cw * 2 / 7
			sb.WriteString(styleText.Render(clip(st.Track.BandName, bw)))
			sb.WriteString(styleDim.Render(" — "))
			sb.WriteString(styleAccent.Render(clip(st.Track.Title, cw-bw)))
		}
	} else {
		mid := a.width - titleFixed(false)
		if mid < 1 {
			mid = 1
		}
		sb.WriteString(styleDim.Render(clip("nothing playing — open the library (l)", mid)))
	}
	sb.WriteString(styleDim.Render(fmt.Sprintf("   vol %3d%%", int(st.Volume*100+0.5))))
	return place(a.width, sb.String())
}

// statusOfIndex maps the standing index state to the severity of its line:
// a failed walk is a hard failure (the radio has no pool to draw from), so
// the line keeps the alarm colour; every other state reads as quiet.
func statusOfIndex(s indexState) statusKind {
	if s == indexFailed {
		return statusBad
	}
	return statusInfo
}

// styleForStatus maps a status severity to its style.
func styleForStatus(k statusKind) lipgloss.Style {
	switch k {
	case statusBad:
		return styleBad
	case statusWarn:
		return styleWarn
	default:
		return styleDim
	}
}

// muteSuffix is the turn-up hint the standing status line appends when the
// audio is silenced: the analyser sits after the gain, so at volume zero
// there is genuinely nothing to draw, and the hint says what brings the
// visualizer back.
const muteSuffix = " — muted: +/− raises the volume"

// renderStatus composes the player's status row. A transient is only this
// row's when it was raised on this screen (statusOn) — one set on the
// library must not surface here, and the standing line takes the row
// instead. At volume zero the mute hint owns the row over an *info*
// transient: the "volume 0%" confirmation is redundant with the vol
// readout in the title row, while the hint explains the blank visualizer —
// the exact moment it is needed most. A warn or bad transient keeps the row
// (an alarm is never masked by a hint); the hint rides at its tail, where a
// wide line clips the hint first, never the alarm. With neither, the radio's
// standing line. The style comes from the severity recorded when the
// message was raised, never from its words.
func (a *App) renderStatus() string {
	transient := ""
	kind := statusInfo
	if a.status != "" && a.statusOn == screenPlayer {
		transient = a.status
		kind = a.statusKind
	}
	muted := a.state.Volume < 0.001 && a.state.Track.Title != ""
	if muted {
		base := styleForStatus(statusOfIndex(a.indexState)).Render(a.indexLine())
		if transient == "" || kind == statusInfo {
			return base + styleAccent.Render(muteSuffix)
		}
		return styleForStatus(kind).Render(transient) + styleAccent.Render(muteSuffix)
	}
	if transient != "" {
		return styleForStatus(kind).Render(transient)
	}
	return styleForStatus(statusOfIndex(a.indexState)).Render(a.indexLine())
}

// indexLine is the standing status-row content: the radio's state and the
// size of the index it draws from. The not-yet-ready states (before the
// follow list lands, and while the walk runs) both read "building",
// because from the user's view the index does not exist until it does.
func (a *App) indexLine() string {
	switch a.indexState {
	case indexIdle:
		return "building track index…"
	case indexBuilding:
		return a.buildingLine()
	case indexFailed:
		return "track index unavailable — the radio plays the queue only"
	}
	n := len(a.playlist.Index())
	radio := "○ radio off"
	if a.playlist.radio && n > 0 {
		radio = "◉ radio on"
	}
	return fmt.Sprintf("%s · %d tracks · %d bands", radio, n, a.playlist.IndexBands())
}

// buildingLine is the standing line while the walk runs: the spinner keeps
// it moving, and the per-band counter shows how far the walk is — the wait
// reads as progress instead of a frozen screen. Without a counter (a
// library that cannot report) the spinner alone keeps it alive.
func (a *App) buildingLine() string {
	line := "building track index…"
	if a.indexTotal > 0 {
		line = fmt.Sprintf("building track index… %d/%d bands", a.indexDone, a.indexTotal)
	}
	return spinFrames[a.spin%len(spinFrames)] + " " + line
}

// renderProgress is the position bar: 1:23 ━━━━──── 4:56.
func (a *App) renderProgress() string {
	st := a.state
	if st.Track.Title == "" {
		// Idle: say what space will do — the radio is the app's primary
		// action, and the idle screen otherwise points only at the library.
		// Name only what space would actually start: the queue beats the
		// radio in Start, and the radio needs its index (space while the
		// index is still building starts nothing).
		switch {
		case len(a.playlist.Queue()) > 0:
			return styleDim.Render("  waiting — press space to play the queue")
		case a.playlist.radio && len(a.playlist.Index()) > 0:
			return styleDim.Render("  waiting — press space to start the radio")
		}
		return styleDim.Render("  waiting")
	}
	var sb strings.Builder
	sb.WriteString(styleDim.Render("  "))
	sb.WriteString(styleText.Render(formatDuration(int(a.pos.Seconds()))))
	if st.Track.Duration > 0 {
		barW := a.width/2 - 12
		if barW < 10 {
			barW = 10
		}
		frac := a.pos.Seconds() / st.Track.Duration.Seconds()
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		filled := int(frac*float64(barW) + 0.5)
		sb.WriteString(styleVisual.Render(strings.Repeat("━", filled)))
		sb.WriteString(styleDim.Render(strings.Repeat("─", barW-filled)))
		sb.WriteString(styleText.Render(" " + formatDuration(int(st.Track.Duration.Seconds()))))
	}
	return sb.String()
}

// renderQueue previews the head of the queue.
func (a *App) renderQueue() string {
	q := a.playlist.Queue()
	var sb strings.Builder
	sb.WriteString(styleDimB.Render("  up next "))
	if len(q) == 0 {
		if a.playlist.radio && len(a.playlist.Index()) > 0 {
			sb.WriteString(styleDim.Render(" — the radio plays next"))
		} else {
			sb.WriteString(styleDim.Render(" — add tracks from the library (l)"))
		}
		return sb.String()
	}
	max := len(q)
	if max > 3 {
		max = 3
	}
	for i := 0; i < max; i++ {
		t := q[i]
		sb.WriteString(styleAccent.Render(clip(t.Title, 24)))
		sb.WriteString(styleDim.Render(" — " + clip(t.BandName, 20) + "   "))
	}
	if len(q) > 3 {
		sb.WriteString(styleDim.Render(fmt.Sprintf("+%d more", len(q)-3)))
	}
	return sb.String()
}

// hint is one keybinding on a bottom help row: its text and how much it
// matters. fitHints drops the least important hints first when the row
// cannot fit the terminal width.
type hint struct {
	text     string
	priority int
}

// mustKeepHint is the priority floor of fitHints: hints at or above it
// (quit and help) are never dropped, because a hint row that cannot say
// how to quit or open the help overlay is worse than a short one.
const mustKeepHint = 100

// fitHints joins the hints with sep (after a leading space) and, while the
// result is wider than w cells, drops one whole hint at a time, the lowest
// priority first. Display order is preserved, so the keys read in the same
// order at every width, and no surviving key or label is ever cut
// mid-word. That is the point: the alternative (letting place() MaxWidth-
// cut the static row) silently hides whatever keys sit past the terminal
// edge — at 80 columns the old player row lost "l library", "+/− vol",
// "? help" and "q quit" to it. If even the must-keep hints do not fit, the
// row is clipped with an ellipsis rather than overflowing the row width
// the layout relies on.
func fitHints(w int, sep string, hints ...hint) string {
	join := func(hs []hint) string {
		s := " "
		for i, h := range hs {
			if i > 0 {
				s += sep
			}
			s += h.text
		}
		return s
	}
	// Copy: the drop loop rewrites the slice in place, and the caller's
	// backing array must not be clobbered (the hints are usually a
	// freshly built variadic slice, but a reused one would be corrupted).
	live := make([]hint, len(hints))
	copy(live, hints)
	for lipgloss.Width(join(live)) > w {
		drop := -1
		for i, h := range live {
			if h.priority < mustKeepHint && (drop == -1 || h.priority < live[drop].priority) {
				drop = i
			}
		}
		if drop == -1 {
			break // only the must-keep hints remain
		}
		live = append(live[:drop], live[drop+1:]...)
	}
	return styleDim.Render(clip(join(live), w))
}

// renderHelp is the player's bottom keybinding hint row, fitted to the
// terminal width. The transport keys, the radio (the app's identity) and
// the library drop in that order; shuffle, repeat, volume, the visualizer
// and fullscreen are the first casualties, quit and help never drop.
func (a *App) renderHelp() string {
	return fitHints(a.width, "  ",
		hint{"space play/pause", 90},
		hint{"n next", 85},
		hint{"p prev", 70},
		hint{"s shuffle", 40},
		hint{"r repeat", 45},
		hint{"d radio", 80},
		hint{"v visual", 30},
		hint{"f fullscreen", 20},
		hint{"l library", 60},
		hint{"+/− vol", 35},
		hint{"? help", mustKeepHint},
		hint{"q quit", mustKeepHint},
	)
}

// renderLibraryHelp is the library's bottom hint row, fitted the same way.
// The enter hint names what actually happens: queueing from the library
// starts at once when the engine is idle (the contract's Queue-on-idle
// behaviour), so the wording primes that expectation instead of the older
// "open / queue", which read as queue-and-carry-on-browsing (issue #10).
// At 80 columns the "x remove from queue" hint drops (it is documented in
// the help overlay) so the navigation hints, including "l or esc back",
// stay whole; the select/queue hints are the last to go.
func (a *App) renderLibraryHelp() string {
	return fitHints(a.width, "   ",
		hint{"↑/↓ select", 90},
		hint{"←/→ column", 80},
		hint{"enter queue (starts if idle)", 85},
		hint{"x remove from queue", 50},
		hint{"l or esc back", 60},
	)
}

// tooSmall renders a centred notice for terminals the layout cannot use. The
// message is clipped to the width with an ellipsis first: Place pads but
// never truncates, so an uncut notice would overflow the very area it is
// apologising for, and a mid-word cut without … reads as a glitch.
func tooSmall(w, h int, msg string) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, styleDim.Render(clip(msg, w)))
}

// viewLibrary assembles the browser screen: title, three columns, help.
func (a *App) viewLibrary() string {
	w, h := a.width, a.height
	if w < 60 || h < 10 {
		// Short for the same reason as the player's notice: it has to fit
		// the terminal it is apologising for.
		return tooSmall(w, h, "library needs 60 × 10")
	}
	var sb strings.Builder
	sb.WriteString(styleAccentB.Render("bc-radio"))
	sb.WriteString(styleDim.Render("   Library   "))
	sb.WriteString(a.libraryStatus())
	title := place(w, sb.String())

	colH := h - 3 // title, columns, help
	cw := w / 3
	if cw < 18 {
		cw = 18
	}
	cols := make([]string, 3)
	for i := range cols {
		cols[i] = a.browserColumn(i, cw, colH)
	}
	body := lipgloss.JoinHorizontal(lipgloss.Left, cols...)
	help := place(w, a.renderLibraryHelp())
	return lipgloss.JoinVertical(lipgloss.Left, title, place(w, body), help)
}

// libraryStatus is the annotation in the library title row. The transient
// shows only when it was raised on this screen — a player confirmation
// (a "volume 90%" set before an l) must not surface here, which was the
// "Library   volume 90%" leak; otherwise the focused column's count reads
// dim, and a transient in the row takes the severity's own colour, the
// same mapping the player's status row uses.
func (a *App) libraryStatus() string {
	if a.status != "" && a.statusOn == screenLibrary {
		return styleForStatus(a.statusKind).Render(a.status)
	}
	text := ""
	switch a.browser.focus {
	case 0:
		text = fmt.Sprintf("%d artists", len(a.browser.artists))
	case 1:
		text = fmt.Sprintf("%d releases", len(a.browser.albums))
	default:
		text = fmt.Sprintf("%d tracks", len(a.browser.tracks))
	}
	return styleDim.Render(text)
}

// browserColumn renders one browser column: a header, then the viewport of
// rows with the selection marked. Each column shows its own row count and
// its own selection marker — the focused column's values must not leak
// into the other two, or their headers lie and phantom markers appear
// wherever the focused selection happens to sit.
func (a *App) browserColumn(col, cw, h int) string {
	b := a.browser
	n := b.columnCountAt(col)
	sel := b.columnSelAt(col)
	var lines []string
	for i := b.scroll[col]; i < n && len(lines) < h; i++ {
		mark := "  "
		style := styleDim
		if i == sel {
			mark = "→ "
			style = styleAccentB
		} else if i == sel-1 {
			style = styleText
		}
		lines = append(lines, style.Render(mark+clip(b.rowFor(col, i), cw-4)))
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	name := "Artists"
	if col == 1 {
		name = "Albums"
	} else if col == 2 {
		name = "Tracks"
	}
	header := styleAccentB.Render(" " + name)
	if n > 0 {
		header += styleDim.Render(fmt.Sprintf(" (%d)", n))
	}
	return place(cw, lipgloss.JoinVertical(lipgloss.Left,
		header, lipgloss.JoinVertical(lipgloss.Left, lines...)))
}

// viewHelp is the full help overlay. Sections are scoped to the screen the
// overlay is open on, because several keys are no-ops off their own screen
// (visualizer and transport keys act on the player; browser keys act on the
// library): advertising a key that does nothing would train a false mental
// model, so each section lists only the keys that work there.
func (a *App) viewHelp() string {
	lines := []string{
		styleAccentB.Render("Keybindings"),
		"",
		styleDimB.Render("anywhere"),
		"  ?          this help",
		"  q          quit",
		"  ctrl+c     quit",
		"",
	}
	if a.screen == screenLibrary {
		lines = append(lines,
			styleDimB.Render("library"),
			"  ↑/↓ select   ←/→ column   enter open / queue",
			"  x remove from queue   esc back to player",
		)
	} else {
		lines = append(lines,
			styleDimB.Render("player"),
			"  space      play / pause",
			"  n          next track",
			"  p          previous track, or restart if within 5 s",
			"  s          shuffle on / off",
			"  r          repeat on / off",
			"  d          radio on / off (random tracks from your follows)",
			"  v          next visualizer",
			"  f          fullscreen visualizer",
			"  l          library",
			"  + / -      volume",
			styleDim.Render(fmt.Sprintf("  v cycles: %s", strings.Join(nameList(a.visualizers), " / "))),
		)
	}
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorDim).
		Padding(1, 2).
		Render(body)
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, box)
}

// nameList returns the display names of the visualizers.
func nameList(vs []visualizer) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Name()
	}
	return out
}
