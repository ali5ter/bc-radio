package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// fixedRows is the number of rows outside the visualizer the player screen
// reserves: title, separator, status, progress, queue, help.
const fixedRows = 6

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
		place(w, a.visualizers[a.visIdx].Render(a.bands, w, visH)),
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

// renderTitle is the top row: app name, transport glyph, now playing,
// volume.
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
		sb.WriteString(styleText.Render(st.Track.BandName))
		sb.WriteString(styleDim.Render(" — "))
		sb.WriteString(styleAccent.Render(st.Track.Title))
		if st.Track.AlbumTitle != "" {
			sb.WriteString(styleDim.Render(" · " + st.Track.AlbumTitle))
		}
	} else {
		sb.WriteString(styleDim.Render("nothing playing — open the library (l)"))
	}
	sb.WriteString(styleDim.Render(fmt.Sprintf("   vol %3d%%", int(st.Volume*100+0.5))))
	return place(a.width, sb.String())
}

// renderStatus shows the transient status line, or — when none is set —
// the radio's standing status.
func (a *App) renderStatus() string {
	text := a.status
	if text == "" {
		text = a.indexLine()
	}
	style := styleDim
	if strings.Contains(a.status, "failed") || strings.Contains(a.status, "error") ||
		strings.Contains(text, "unavailable") {
		style = styleBad
	}
	return style.Render(text)
}

// indexLine is the standing status-row content: the radio's state and the
// size of the index it draws from. The not-yet-ready states (before the
// follow list lands, and while the walk runs) both read "building",
// because from the user's view the index does not exist until it does.
func (a *App) indexLine() string {
	switch a.indexState {
	case indexIdle, indexBuilding:
		return "building track index…"
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

// renderProgress is the position bar: 1:23 ━━━━──── 4:56.
func (a *App) renderProgress() string {
	st := a.state
	if st.Track.Title == "" {
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
// At 80 columns the "x remove from queue" hint drops (it is documented in
// the help overlay) so the navigation hints, including "l or esc back",
// stay whole; the select/queue hints are the last to go.
func (a *App) renderLibraryHelp() string {
	return fitHints(a.width, "   ",
		hint{"↑/↓ select", 90},
		hint{"←/→ column", 80},
		hint{"enter open / queue", 85},
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
	sb.WriteString(styleDim.Render("   Library   " + a.libraryStatus()))
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

// libraryStatus is the dim annotation in the library title row.
func (a *App) libraryStatus() string {
	if s := a.status; s != "" {
		return s
	}
	switch a.browser.focus {
	case 0:
		return fmt.Sprintf("%d artists", len(a.browser.artists))
	case 1:
		return fmt.Sprintf("%d releases", len(a.browser.albums))
	default:
		return fmt.Sprintf("%d tracks", len(a.browser.tracks))
	}
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
			styleDim.Render(fmt.Sprintf("  v cycles: %s (16-band analyser)", strings.Join(nameList(a.visualizers), " / "))),
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
