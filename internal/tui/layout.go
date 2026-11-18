package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// headerRows is the wordmark header's row count: six — the drawing's top
// and bottom rules, the title, the band, the album and the time. The
// single-row title is the only narrower form. headerRows + footerRows is
// the player screen's fixed floor: the minimum height is 10 rows because
// the view must equal the terminal height, or the frame tears.
const headerRows = 6

// footerRows is the player footer's row count: status, check-out link,
// up-next preview and the keybinding hints.
const footerRows = 4

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

// viewPlayer assembles the player screen: the header block, the
// visualizer region, then the four footer rows — or just header and
// visualizer in fullscreen (it drops the footer, not the wordmark). The
// view is exactly the terminal's size: header and footer reserve their
// rows, the visualizer gets the rest, and when the terminal is too
// small for the floor the layout is the tooSmall notice rather than a
// torn frame.
func (a *App) viewPlayer() string {
	w, h := a.width, a.height
	if w < 30 || h < headerRows+footerRows {
		// Short on purpose: below the 30-column, 10-row floor the notice
		// has to fit the very terminal it is apologising for, so it
		// names the app and the requirement ("bc-radio needs 30 × 10")
		// and nothing else — the longer wording ellipsised its own tail
		// at 25 columns.
		return tooSmall(w, h, "bc-radio needs 30 × 10")
	}
	header := a.playerHeader()
	fixed := len(header)
	if !a.fullscreen {
		fixed += footerRows
	}
	visH := h - fixed
	lines := append([]string(nil), header...)
	if visH >= 1 {
		lines = append(lines,
			place(w, a.visualizers[a.visIdx].Render(frame{bands: a.bands, scope: a.scope, w: w, h: visH})))
	}
	if !a.fullscreen {
		lines = append(lines,
			place(w, a.renderStatus()),
			place(w, a.checkOutRow()),
			place(w, a.upNextRow()),
			place(w, a.renderHelp()),
		)
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// playerHeader renders the player screen's header block: the 48-cell
// wordmark with its right-justified info block (title, band, album,
// time) when the terminal is wide enough, and the single-row title
// below that. It returns the block's rows, so the caller hands the
// visualizer exactly the rows that remain.
func (a *App) playerHeader() []string {
	if a.width >= wordmarkWidth {
		arts := wordmarkRows()
		rows := make([]string, headerRows)
		rows[0] = a.headerArtRow(arts[0])
		rows[1] = a.headerInfoRow(arts[1], a.headerTrackRow())
		rows[2] = a.headerInfoRow(arts[2], a.headerBandRow())
		rows[3] = a.headerInfoRow(arts[3], a.headerAlbumRow())
		rows[4] = a.headerInfoRow(arts[4], a.headerTimeRow())
		rows[5] = a.headerArtRow(arts[5])
		return rows
	}
	return []string{a.narrowTitle()}
}

// headerArtRow renders the wordmark's top or bottom rule: the drawing in
// the accent, then dim dashes to the terminal edge — the art begins and
// ends on a run of ─, so the fill reads as the rule continuing.
func (a *App) headerArtRow(art string) string {
	fill := a.width - wordmarkWidth
	if fill < 0 {
		fill = 0
	}
	return styleAccentB.Render(art) + styleDim.Render(strings.Repeat("─", fill))
}

// headerMax is the info block's cell budget: the terminal minus the
// wordmark and the three-dash gap minimum. It floors at zero at the
// 48-cell minimum, where the drawing owns the row and the content earns
// cells as the terminal widens.
func (a *App) headerMax() int {
	m := a.width - wordmarkWidth - 3
	if m < 0 {
		m = 0
	}
	return m
}

// headerInfoRow assembles one info-block row: the wordmark, a dim dash
// gap (at least three cells, filling the rest), and the content pinned
// to the terminal's right edge. The final MaxWidth is a safety net for a
// content budget that overruns a very narrow wide-layout terminal.
func (a *App) headerInfoRow(art, content string) string {
	gap := a.width - wordmarkWidth - lipgloss.Width(content)
	if gap < 3 {
		gap = 3
	}
	row := styleAccentB.Render(art) + styleDim.Render(strings.Repeat("─", gap)) + content
	return lipgloss.NewStyle().MaxWidth(a.width).Render(row)
}

// idleStartHint names what space would actually start: the queue beats
// the radio in Start, and the radio needs its index (space while the
// walk is still building starts nothing). Shared by the header's title
// row and the narrow single-row title.
func (a *App) idleStartHint() string {
	switch {
	case len(a.playlist.Queue()) > 0:
		return "nothing playing — press space to play the queue"
	case a.playlist.radio && len(a.playlist.Index()) > 0:
		return "nothing playing — press space to start the radio"
	}
	return "nothing playing — open the library (l)"
}

// headerTrackRow is the info block's first row: the transport verb and
// the quoted title — or the idle affordance when nothing is playing, so
// the art is never a silent rectangle.
func (a *App) headerTrackRow() string {
	st := a.state
	if st.Track.Title == "" {
		return styleDim.Render(clip(a.idleStartHint(), a.headerMax()))
	}
	verb := "▶"
	if !st.Playing {
		verb = "⏸"
	}
	titleW := a.headerMax() - 4 // "▶ " and the two quotes
	if titleW < 1 {
		titleW = 1
	}
	return styleDim.Render(verb+" ") + styleAccent.Render("'"+clip(st.Track.Title, titleW)+"'")
}

// headerBandRow is the info block's second row: the band, dim, hanging
// under the title.
func (a *App) headerBandRow() string {
	if a.state.Track.BandName == "" {
		return ""
	}
	bw := a.headerMax() - 6 // "   by "
	if bw < 1 {
		bw = 1
	}
	return styleDim.Render("   by " + clip(a.state.Track.BandName, bw))
}

// headerAlbumRow is the info block's third row: the album, dim, hanging
// under the band.
func (a *App) headerAlbumRow() string {
	if a.state.Track.AlbumTitle == "" {
		return ""
	}
	aw := a.headerMax() - 8 // "   on " and the two quotes
	if aw < 1 {
		aw = 1
	}
	return styleDim.Render("   on '" + clip(a.state.Track.AlbumTitle, aw) + "'")
}

// headerTimeRow is the info block's fourth row: the position bar — the
// elapsed time, the filled/empty bar, the track percent and the total.
// With an unknown duration only the elapsed time shows.
func (a *App) headerTimeRow() string {
	st := a.state
	if st.Track.Title == "" {
		return ""
	}
	cur := formatDuration(int(a.pos.Seconds()))
	if st.Track.Duration <= 0 {
		return styleDim.Render(" ∿ " + cur)
	}
	dur := formatDuration(int(st.Track.Duration.Seconds()))
	suffix := fmt.Sprintf(" (%d%%)", int(a.pos.Seconds()/st.Track.Duration.Seconds()*100+0.5))
	prefix := " ∿ " + cur + " / " + dur + " "
	bar := ""
	barW := a.headerMax() - lipgloss.Width(prefix) - lipgloss.Width(suffix)
	if barW >= 1 {
		frac := a.pos.Seconds() / st.Track.Duration.Seconds()
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		filled := int(frac*float64(barW) + 0.5)
		bar = styleVisual.Render(strings.Repeat("━", filled)) +
			styleDim.Render(strings.Repeat("─", barW-filled))
	}
	return styleDim.Render(prefix) + bar + styleDim.Render(suffix)
}

// narrowTitle is the single-row title for terminals narrower than the
// wordmark: app name, transport glyph, now playing — the volume reads
// in the footer's status tail, so the row carries no volume field. The
// now-playing fields are allocated a share of the middle and clipped
// inside their share (the way the library columns clip), so a long title
// ellipses in the middle of the row instead of running off the edge.
func (a *App) narrowTitle() string {
	st := a.state
	var sb strings.Builder
	sb.WriteString(styleAccentB.Render("bc-radio"))
	sb.WriteString(styleDim.Render("   "))
	fixed := 11 // "bc-radio   "
	if st.Track.Title != "" {
		verb := "▶"
		if !st.Playing {
			verb = "⏸"
		}
		sb.WriteString(styleAccentB.Render(verb + " "))
		fixed += 2 // "▶ " or "⏸ "
		// band : title : album as 2 : 3 : 2 of what the separators leave —
		// the title is the field that overflows in practice, so it gets
		// the largest share.
		mid := a.width - fixed
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
		mid := a.width - fixed
		if mid < 1 {
			mid = 1
		}
		sb.WriteString(styleDim.Render(clip(a.idleStartHint(), mid)))
	}
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

// volumeTail is the status row's dim volume readout: the volume is
// read once, here, so the header carries no volume field. The tail is
// omitted when muted (the mute hint already says the volume is down)
// and for the "volume N%" confirmation itself (that would read "volume
// 90% · volume 90%").
func (a *App) volumeTail() string {
	if a.state.Volume < 0.001 && a.state.Track.Title != "" {
		return ""
	}
	if a.status != "" && a.statusOn == screenPlayer &&
		a.statusKind == statusInfo && strings.HasPrefix(a.status, "volume ") {
		return ""
	}
	return styleDim.Render(fmt.Sprintf(" · volume %d%%", int(a.state.Volume*100+0.5)))
}

// statusRow appends the volume tail, clipping the message to the width
// left so the readout at the edge stays whole.
func (a *App) statusRow(msg string) string {
	tail := a.volumeTail()
	if tail == "" {
		return msg
	}
	return lipgloss.NewStyle().MaxWidth(a.width-lipgloss.Width(tail)).Render(msg) + tail
}

// renderStatus composes the player's status row (the footer's first):
// the severity-coloured message and the dim volume tail, where one
// applies. A transient is only this row's when it was raised on this
// screen (statusOn) — one set on the library must not surface here, and
// the standing line takes the row instead. At volume zero the mute hint
// owns the row over an *info* transient: the "volume 0%" confirmation
// is redundant with the tail, while the hint explains the blank
// visualizer — the exact moment it is needed most. A warn or bad
// transient keeps the row (an alarm is never masked by a hint); the
// hint rides at its tail, where a wide line clips the hint first, never
// the alarm. With neither, the radio's standing line. The style comes
// from the severity recorded when the message was raised, never from its
// words.
func (a *App) renderStatus() string {
	transient := ""
	kind := statusInfo
	if a.status != "" && a.statusOn == screenPlayer {
		transient = a.status
		kind = a.statusKind
	}
	if a.state.Volume < 0.001 && a.state.Track.Title != "" {
		base := styleForStatus(statusOfIndex(a.indexState)).Render(a.indexLine())
		if transient == "" || kind == statusInfo {
			return base + styleAccent.Render(muteSuffix)
		}
		return styleForStatus(kind).Render(transient) + styleAccent.Render(muteSuffix)
	}
	if transient != "" {
		return a.statusRow(styleForStatus(kind).Render(transient))
	}
	return a.statusRow(styleForStatus(statusOfIndex(a.indexState)).Render(a.indexLine()))
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

// checkOutRow is the footer's second row: where the playing track's band
// lives on Bandcamp, so a listener can find the release to support. All
// dim; blank when nothing with a band name is playing. The band clips to
// fit, the URL stays whole.
func (a *App) checkOutRow() string {
	t := a.state.Track
	if t.Title == "" || t.BandName == "" {
		return ""
	}
	urlPart := ""
	if t.Subdomain != "" {
		urlPart = " at https://" + t.Subdomain + "/"
	}
	const prefix = "├─ Check out "
	bandW := a.width - len(prefix) - len(urlPart)
	if bandW < 1 {
		bandW = 1
	}
	row := prefix + clip(t.BandName, bandW) + urlPart
	return styleDim.Render(lipgloss.NewStyle().MaxWidth(a.width).Render(row))
}

// upNextRow is the footer's third row: what plays next — the head of the
// queue when there is one, otherwise what the radio draws (or the
// library, when the radio has no pool). The title is accent and shrinks
// first on overflow; the band and album ride dim at its tail.
func (a *App) upNextRow() string {
	const prefix = "└─ Up next: "
	q := a.playlist.Queue()
	if len(q) == 0 {
		msg := "open the library (l) to queue tracks"
		if a.playlist.radio && len(a.playlist.Index()) > 0 {
			msg = "the radio plays next"
		}
		return styleDim.Render(prefix + msg)
	}
	t := q[0]
	bandPart := ""
	if t.BandName != "" {
		bandPart = " by " + clip(t.BandName, 20)
	}
	albumPart := ""
	if t.AlbumTitle != "" {
		albumPart = " from '" + clip(t.AlbumTitle, 20) + "'"
	}
	titleW := a.width - len(prefix) - len(bandPart) - len(albumPart) - 2 // the two quotes
	if titleW < 1 {
		titleW = 1
	}
	row := styleAccent.Render(prefix+"'"+clip(t.Title, titleW)+"'") +
		styleDim.Render(bandPart+albumPart)
	return lipgloss.NewStyle().MaxWidth(a.width).Render(row)
}

// hint is one keybinding on a bottom help row: its key, its description,
// and how much it matters. fitHints drops the least important hints first
// when the row cannot fit the terminal width.
type hint struct {
	key      string
	desc     string
	priority int
}

// mustKeepHint is the priority floor of fitHints: hints at or above it
// (quit and help) are never dropped, because a hint row that cannot say
// how to quit or open the help overlay is worse than a short one.
const mustKeepHint = 100

// fitHints joins the hints with sep and, while the result is wider than w
// cells, drops one whole hint at a time, the lowest priority first.
// Display order is preserved, so the keys read in the same order at every
// width, and no surviving key or label is ever cut mid-word. That is the
// point: the alternative (letting place() MaxWidth-cut the static row)
// silently hides whatever keys sit past the terminal edge — at 80 columns
// the old player row lost "l library", "+/- vol", "? help" and "q quit"
// to it. If even the must-keep hints do not fit, the row is clipped
// (ANSI-aware, with no ellipsis to spare cells for) rather than
// overflowing the row width the layout relies on.
func fitHints(w int, sep string, hints ...hint) string {
	cell := func(h hint) string {
		return styleAccentB.Render(h.key) + styleDim.Render(" "+h.desc)
	}
	// Copy: the drop loop rewrites the slice in place, and the caller's
	// backing array must not be clobbered (the hints are usually a
	// freshly built variadic slice, but a reused one would be corrupted).
	live := make([]hint, len(hints))
	copy(live, hints)
	width := func(hs []hint) int {
		n := 0
		for i, h := range hs {
			if i > 0 {
				n += len(sep)
			}
			n += lipgloss.Width(cell(h))
		}
		return n
	}
	for width(live) > w {
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
	var sb strings.Builder
	for i, h := range live {
		if i > 0 {
			sb.WriteString(sep)
		}
		sb.WriteString(cell(h))
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(sb.String())
}

// renderHelp is the player's bottom keybinding hint row, fitted to the
// terminal width. The keys read in the app's house style: the key in the
// bold accent, the description in dim. The transport keys, the radio (the
// app's identity) and the library drop in that order; shuffle, repeat,
// volume, the visualizer and fullscreen are the first casualties, quit and
// help never drop.
func (a *App) renderHelp() string {
	return fitHints(a.width, "  ",
		hint{"space", "play/pause", 90},
		hint{"n", "next", 85},
		hint{"p", "prev", 70},
		hint{"s", "shuffle", 40},
		hint{"r", "repeat", 45},
		hint{"d", "radio", 80},
		hint{"v", "visual", 30},
		hint{"f", "fullscreen", 20},
		hint{"l", "library", 60},
		hint{"+/-", "vol", 35},
		hint{"?", "help", mustKeepHint},
		hint{"q", "quit", mustKeepHint},
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
		hint{"↑/↓", "select", 90},
		hint{"←/→", "column", 80},
		hint{"enter", "queue (starts if idle)", 85},
		hint{"x", "remove from queue", 50},
		hint{"l or esc", "back", 60},
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
