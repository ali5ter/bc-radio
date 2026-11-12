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
		return tooSmall(w, h, "terminal too small — bc-radio needs 30 × 8")
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
			place(w, renderHelp()),
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

// renderStatus shows the transient status line, or nothing.
func (a *App) renderStatus() string {
	style := styleDim
	if strings.Contains(a.status, "failed") || strings.Contains(a.status, "error") {
		style = styleBad
	}
	return style.Render(a.status)
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
		sb.WriteString(styleDim.Render(" — add tracks from the library (l)"))
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

// renderHelp is the bottom keybinding hint row.
func renderHelp() string {
	return styleDim.Render(
		" space play/pause  n next  p prev  s shuffle  r repeat  v visual  f fullscreen  l library  +/− vol  ? help  q quit")
}

// tooSmall renders a centred notice for terminals the layout cannot use. The
// message is truncated to the width first: Place pads but never truncates, so
// an uncut notice would overflow the very area it is apologising for.
func tooSmall(w, h int, msg string) string {
	msg = styleDim.Render(msg)
	if lipgloss.Width(msg) > w {
		msg = lipgloss.NewStyle().MaxWidth(w).Render(msg)
	}
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, msg)
}

// viewLibrary assembles the browser screen: title, three columns, help.
func (a *App) viewLibrary() string {
	w, h := a.width, a.height
	if w < 60 || h < 10 {
		return tooSmall(w, h, "terminal too small — the library needs 60 × 10")
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
	help := place(w, styleDim.Render(
		" ↑/↓ select   ←/→ column   enter open / queue   x remove from queue   l or esc back"))
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
// rows with the selection marked.
func (a *App) browserColumn(col, cw, h int) string {
	b := a.browser
	n := b.columnCount()
	var lines []string
	for i := b.scroll[col]; i < n && len(lines) < h; i++ {
		mark := "  "
		style := styleDim
		if i == b.columnSel() {
			mark = "→ "
			style = styleAccentB
		} else if i == b.columnSel()-1 {
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

// viewHelp is the full help overlay.
func (a *App) viewHelp() string {
	lines := []string{
		styleAccentB.Render("Keybindings"),
		"",
		"space      play / pause",
		"n          next track",
		"p          previous track, or restart if within 5 s",
		"s          shuffle on / off",
		"r          repeat on / off",
		"v          next visualizer",
		"f          fullscreen visualizer",
		"l          library",
		"+ / -      volume",
		"?          this help",
		"q          quit",
		"",
		styleDimB.Render("library"),
		"  ↑/↓ select   ←/→ column   enter open / queue",
		"  x remove from queue   esc back",
		"",
		styleDim.Render("The visualizers react to the 16-band analyser:"),
		styleDim.Render(fmt.Sprintf("  v cycles %s", strings.Join(nameList(a.visualizers), " / "))),
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
