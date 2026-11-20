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
		// A loaded track that is not playing is paused (idle — no track —
		// is not: the field rests at its faint baseline there by design).
		paused := a.state.Track.ID != "" && !a.state.Playing
		lines = append(lines,
			place(w, a.visualizers[a.visIdx].Render(frame{bands: a.bands, scope: a.scope, paused: paused, w: w, h: visH})))
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
// wordmark with its info block (title, band, album, time) when the
// terminal is wide enough, and the single-row title below that. It
// returns the block's rows, so the caller hands the visualizer exactly
// the rows that remain.
func (a *App) playerHeader() []string {
	if a.width >= wordmarkWidth {
		rows := make([]string, headerRows)
		rows[0] = a.headerArtRow(0)
		rows[1] = a.headerInfoRow(1, a.headerTrackRow())
		rows[2] = a.headerInfoRow(2, a.headerBandRow())
		rows[3] = a.headerInfoRow(3, a.headerAlbumRow())
		rows[4] = a.headerInfoRow(4, a.headerTimeRow())
		rows[5] = a.headerArtRow(5)
		return rows
	}
	return []string{a.narrowTitle()}
}

// libraryTitleRow is the section title the library screen carries under
// its header's space: the screen's name and the number of artists, or —
// while one is live — the transient raised on this screen, in its
// severity's colour. The focused column's count does not read here: each
// column already names its own, and the header above is the app's, which
// reflects the playback and does not move while the user browses (issue
// #15 follow-up).
func (a *App) libraryTitleRow() string {
	if a.status != "" && a.statusOn == screenLibrary {
		return styleForStatus(a.statusKind).Render(a.status)
	}
	return styleAccent.Render("Library") + styleDim.Render(fmt.Sprintf(" (%d artists)", len(a.browser.artists)))
}

// headerArtRow renders one of the wordmark's top or bottom rule rows: the
// drawing (bar glyphs quiet, letter glyphs in the theme's gradient), then
// dim dashes to the terminal edge — the art begins and ends on a run of
// ─, so the fill reads as the rule continuing.
func (a *App) headerArtRow(i int) string {
	fill := a.width - wordmarkWidth
	if fill < 0 {
		fill = 0
	}
	return wordmarkArt[i] + styleDim.Render(strings.Repeat("─", fill))
}

// headerGapCells is the dash gap between the wordmark and the info block:
// five cells, the sketch's drawing, which fixes the block's left edge at
// one column (the time row's ∿) regardless of what the rows carry.
const headerGapCells = 5

// headerGap is the gap's width for a terminal of w cells: the five-cell
// drawing, shrinking only in the few columns between the 48-cell wordmark
// and that gap, where the block's fixed column would fall off the edge.
func headerGap(w int) int {
	if g := w - wordmarkWidth; g < headerGapCells {
		if g < 0 {
			return 0
		}
		return g
	}
	return headerGapCells
}

// headerMax is the info block's cell budget: the terminal minus the
// wordmark and the gap. It floors at zero, where the drawing owns the row
// and the content earns cells as the terminal widens.
func (a *App) headerMax() int {
	m := a.width - wordmarkWidth - headerGap(a.width)
	if m < 0 {
		m = 0
	}
	return m
}

// headerContentMax is the space a content row may use: the block's budget
// minus the two-cell quiet tail headerInfoRow closes with at the far right
// (the tail marks the box's right edge, so the content stops short of it).
// It floors at zero.
func (a *App) headerContentMax() int {
	m := a.headerMax() - 2
	if m < 0 {
		m = 0
	}
	return m
}

// headerInfoRow assembles one header info row: the wordmark, the dim dash
// gap, then the content left-aligned at the block's fixed column — the
// rows hang from one edge and sit inside the header's box, never
// right-justified against the terminal edge (issue #13). The row closes
// with a two-cell quiet tail at the far right, so the negative space
// between the content and the terminal edge reads as the box's interior
// (issue #13 follow-up). The row is padded to the terminal width; a final
// MaxWidth is a safety net for a content budget that overruns a very
// narrow wide-layout terminal.
func (a *App) headerInfoRow(i int, content string) string {
	if budget := a.headerContentMax(); budget >= 0 && lipgloss.Width(content) > budget {
		content = lipgloss.NewStyle().MaxWidth(budget).Render(content)
	}
	body := place(a.width-2, wordmarkArt[i]+styleDim.Render(strings.Repeat("─", headerGap(a.width)))+content)
	return body + styleDim.Render("──")
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

// headerTrackRow is the info block's first row: a leading space, the
// transport verb and the quoted title — or the idle affordance when
// nothing is playing, so the art is never a silent rectangle. Every row's
// content begins at the block's fixed column with a leading space, so the
// idle hint sits exactly where the playing rows' verbs sit (the
// 2026-11-19 alignment fix); the hint then takes the rest of the row's
// budget.
func (a *App) headerTrackRow() string {
	st := a.state
	if st.Track.Title == "" {
		hw := a.headerContentMax() - 1 // the leading space
		if hw < 1 {
			hw = 1
		}
		return styleDim.Render(" " + clip(a.idleStartHint(), hw))
	}
	verb := "▶"
	if !st.Playing {
		verb = "⏸"
	}
	titleW := a.headerContentMax() - 5 // the space, the verb, a space, and the two quotes
	if titleW < 1 {
		titleW = 1
	}
	return styleDim.Render(" "+verb+" ") + styleAccent.Render("'"+clip(st.Track.Title, titleW)+"'")
}

// headerBandRow is the info block's second row: the band, dim, hanging
// under the title (three spaces where the verb sat).
func (a *App) headerBandRow() string {
	if a.state.Track.BandName == "" {
		return ""
	}
	bw := a.headerContentMax() - 6 // "   by "
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
	aw := a.headerContentMax() - 8 // "   on " and the two quotes
	if aw < 1 {
		aw = 1
	}
	return styleDim.Render("   on '" + clip(a.state.Track.AlbumTitle, aw) + "'")
}

// headerTimeRow is the info block's fourth row: the position bar — a
// leading space, the ∿ glyph, the elapsed time, the filled/empty bar and
// the track percent, then the space before the tail. The bar takes every
// cell the times and the percent leave, so the row closes with the block's
// standard two-dash tail through headerInfoRow like every other info row —
// one tail, never a doubled one, with the space between the percent and
// the tail (issue #13 follow-up, 2026-11-19: the row used to carry its own
// tail and the block's, leaving four dashes after the percent where two
// belong, and the first fix let the percent butt the tail).
// With an unknown duration only the elapsed time shows, and with no track
// the row is empty.
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
	// The row's two-cell tail belongs to headerInfoRow (as on every other
	// info row), so the bar takes the remaining content space minus one
	// cell: the empty cells sit between the times and the percent, and the
	// trailing space separates the percent from the tail — the same close
	// as the title, band and album rows, where the text stops short of
	// the edge.
	barW := a.headerContentMax() - lipgloss.Width(prefix) - lipgloss.Width(suffix) - 1
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
	// The row ends with the space, so the percent stops short of the
	// block's tail; the other info rows get the same space from padding.
	return styleDim.Render(prefix) + bar + styleDim.Render(suffix+" ")
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
// words. A transient is a brief hint, so it sits on its own quiet
// background, full row width — the standing line, which is always
// present, does not (issue #14).
func (a *App) renderStatus() string {
	transient := ""
	kind := statusInfo
	if a.status != "" && a.statusOn == screenPlayer {
		transient = a.status
		kind = a.statusKind
	}
	var row string
	if a.state.Volume < 0.001 && a.state.Track.Title != "" {
		base := styleForStatus(statusOfIndex(a.indexState)).Render(a.indexLine())
		if transient == "" || kind == statusInfo {
			row = base + styleAccent.Render(muteSuffix)
		} else {
			row = styleForStatus(kind).Render(transient) + styleAccent.Render(muteSuffix)
		}
	} else if transient != "" {
		row = a.statusRow(styleForStatus(kind).Render(transient))
	} else {
		row = a.statusRow(styleForStatus(statusOfIndex(a.indexState)).Render(a.indexLine()))
	}
	if transient != "" {
		row = styleTransientBg.Render(place(a.width, row))
	}
	return row
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
		// A loaded track that is not playing is a paused radio: the line
		// says so, instead of claiming a playing radio over silence.
		// Idle (no track) is not paused — the radio is on and waiting.
		if a.state.Track.ID != "" && !a.state.Playing {
			radio = "◉ radio paused"
		}
	} else if a.state.Track.ID == "" {
		// Idle and nothing picking: the app is quiet, so the line states
		// what will start it — the queue, if one was built, else the
		// library (Alister's re-test, 2026-11-19). With a track loaded
		// the line stays the plain state: off means the app stops picking
		// after it, and the up-next row says what comes.
		if len(a.playlist.Queue()) > 0 {
			radio = "○ radio off — space plays the queue"
		} else {
			radio = "○ radio off — l opens the library"
		}
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
// lives on Bandcamp, so a listener can find the release to support. The
// prefix stays quiet; the band name and the URL carry the accent — the
// one thing the row is for — and the " at " between them stays quiet
// (issue #14 follow-up). Blank when nothing with a band *and* a Bandcamp
// home is playing: a name without a URL points nowhere, so the row stays
// quiet instead of pointing at a guess. The band clips to fit, the URL
// stays whole.
func (a *App) checkOutRow() string {
	t := a.state.Track
	if t.Title == "" || t.BandName == "" || t.Subdomain == "" {
		return ""
	}
	url := "https://" + t.Subdomain + "/"
	const prefix = "├─ Check out "
	// Cell widths, not byte lengths: the box-drawing prefix is 13 cells but
	// 17 bytes, and a byte budget would quietly shrink the band's space.
	bandW := a.width - lipgloss.Width(prefix) - len(" at ") - len(url)
	if bandW < 1 {
		bandW = 1
	}
	row := styleDim.Render(prefix) + styleAccent.Render(clip(t.BandName, bandW)) +
		styleDim.Render(" at ") + styleAccent.Render(url)
	return lipgloss.NewStyle().MaxWidth(a.width).Render(row)
}

// upNextRow is the footer's third row: what plays next — the head of the
// queue when there is one, otherwise the radio's prepared draw — in the
// issue's shape, 'title' by band on 'album', and all in quiet gray: the
// row reports, it does not advertise, and a track the user just queued
// from the library lands here without a flash (issue #14). When nothing
// can be known — no queue head, no prepared draw — the row is blank
// (issue #14 follow-up): the status line already says where the index
// stands, and a pointer to the library would read as noise.
func (a *App) upNextRow() string {
	t := a.playlist.upNext()
	if t.ID == "" {
		return ""
	}
	const prefix = "└─ Up next: "
	bandPart := ""
	if t.BandName != "" {
		bandPart = " by " + clip(t.BandName, 24)
	}
	albumPart := ""
	if t.AlbumTitle != "" {
		albumPart = " on '" + clip(t.AlbumTitle, 24) + "'"
	}
	titleW := a.width - lipgloss.Width(prefix) - lipgloss.Width(bandPart) - lipgloss.Width(albumPart) - 2 // the two quotes
	if titleW < 1 {
		titleW = 1
	}
	row := prefix + "'" + clip(t.Title, titleW) + "'" + bandPart + albumPart
	return styleDim.Render(lipgloss.NewStyle().MaxWidth(a.width).Render(row))
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

// hintCell renders one hint in the house style: the key in the bold
// accent, the description in dim, no leading separator.
func hintCell(h hint) string {
	return styleAccentB.Render(h.key) + styleDim.Render(" "+h.desc)
}

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
	cell := hintCell
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

// renderHelp is the player's bottom keybinding hint row. The essentials
// read left — the keys a listener reaches for without looking, quit among
// them — and ? help sits at the right edge, apart from them (issue #14:
// the row carried too many hints; 2026-11-19: help, not quit, closes the
// row). The dropped keys still work — n, p, s, r and +/− are in the help
// overlay, one ? away — and the help hint, like quit, is never dropped on
// a narrow terminal.
func (a *App) renderHelp() string {
	help := hintCell(hint{"?", "help", mustKeepHint})
	helpW := lipgloss.Width(help)
	left := fitHints(a.width-helpW-2, "  ",
		hint{"space", "play/pause", 90},
		hint{"d", "radio", 80},
		hint{"l", "library", 70},
		hint{"v", "visual", 60},
		hint{"f", "fullscreen", 40},
		hint{"q", "quit", mustKeepHint},
	)
	pad := a.width - helpW - lipgloss.Width(left)
	if pad < 0 {
		pad = 0
	}
	return left + styleDim.Render(strings.Repeat(" ", pad)) + help
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

// viewLibrary assembles the browser screen: the player's own header (the
// wordmark and the playback's info block — it reflects what is playing
// and does not change while the user browses), a blank row, the section
// title (or the transient raised on this screen), three columns, help.
func (a *App) viewLibrary() string {
	w, h := a.width, a.height
	if w < 60 || h < 12 {
		// Short for the same reason as the player's notice: it has to fit
		// the terminal it is apologising for. The floor grew two rows in
		// the 2026-11-19 layout (the blank row and the section title
		// under the header), so two data rows remain at the minimum.
		return tooSmall(w, h, "library needs 60 × 12")
	}
	header := a.playerHeader()
	// The wordmark header, the blank row, the section title and the help
	// row are fixed; the columns' data rows take the rest. Each column
	// renders one header row of its own on top of them, which the total
	// already accounts for.
	colH := h - len(header) - 4
	if colH < 1 {
		colH = 1
	}
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
	lines := append([]string(nil), header...)
	lines = append(lines, "", place(w, a.libraryTitleRow()), place(w, body), help)
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// browserColumn renders one browser column: a header, then the viewport of
// rows with the selection marked. Each column shows its own row count and
// its own selection marker — the focused column's values must not leak
// into the other two, or their headers lie and phantom markers appear
// wherever the focused selection happens to sit. The selected row of the
// focused column is a full-column bar in the scheme's highlight colour:
// one uniform colour edge to edge, where a reverse video inverted to the
// terminal's own background — white on a light terminal — and broke the
// single-colour bar (issue #15 follow-up).
func (a *App) browserColumn(col, cw, h int) string {
	b := a.browser
	n := b.columnCountAt(col)
	sel := b.columnSelAt(col)
	var lines []string
	for i := b.scroll[col]; i < n && len(lines) < h; i++ {
		row := clip(b.rowFor(col, i), cw-4)
		switch {
		case i == sel && col == b.focus:
			lines = append(lines, styleHighlight.Width(cw-2).Render(styleAccentB.Render("→ "+row)))
		case i == sel:
			lines = append(lines, styleAccentB.Render("→ "+row))
		default:
			// Every other row reads quiet — including the row above the
			// selection, which an earlier design rendered bright (Alister's
			// 2026-11-19 screenshot: a phantom highlight on the previous
			// line). The bar alone marks the position.
			lines = append(lines, styleDim.Render("  "+row))
		}
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
			"  ← / →      seek 10 s back / forward",
			styleDim.Render(fmt.Sprintf("  v cycles: %s", strings.Join(nameList(a.visualizers), " / "))),
		)
	}
	body := lipgloss.JoinVertical(lipgloss.Left, lines...)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorDim).
		Padding(1, 2).
		Render(body)
	return box
}

// composeHelp renders the help box centred over the current screen (issue
// #19): the cells inside the box take the box's glyphs and styles, the
// cells outside keep the live screen, so the playback stays visible around
// the modal instead of the box floating on a blank background.
func (a *App) composeHelp(base, box string) string {
	baseLines := strings.Split(strings.TrimRight(base, "\n"), "\n")
	boxLines := strings.Split(strings.TrimRight(box, "\n"), "\n")
	boxW := 0
	for _, l := range boxLines {
		if w := lipgloss.Width(l); w > boxW {
			boxW = w
		}
	}
	top := (a.height - len(boxLines)) / 2
	left := (a.width - boxW) / 2
	if top < 0 {
		top = 0
	}
	if left < 0 {
		left = 0
	}
	if top+len(boxLines) > len(baseLines) {
		top = len(baseLines) - len(boxLines)
	}
	if top < 0 {
		top = 0
	}
	var out []string
	for i, line := range baseLines {
		if i >= top && i-top < len(boxLines) {
			out = append(out, mergeRows(line, boxLines[i-top], left, boxW))
		} else {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// sgrSegment is one stretch of a styled row: the SGR code that applies to
// its runes ("" = the terminal default) and the runes themselves.
type sgrSegment struct {
	sgr   string
	runes []rune
}

// splitRow parses a lipgloss-rendered row into SGR segments. The renderer
// re-emits the code before every styled stretch and resets after it, and
// the rows carry no escape sequences but these, so a row is a flat list of
// (code, text) pairs.
func splitRow(row string) []sgrSegment {
	var segs []sgrSegment
	cur := sgrSegment{}
	rs := []rune(row)
	for i := 0; i < len(rs); {
		if rs[i] == '\x1b' && i+1 < len(rs) && rs[i+1] == '[' {
			j := i + 2
			for j < len(rs) && rs[j] != 'm' {
				j++
			}
			if j < len(rs) {
				segs = append(segs, cur)
				// A reset code is the default state, not a style: the
				// renderer resets after every styled stretch, so the state
				// the next stretch inherits is "" (mergeRows re-emits the
				// reset when a state leaves the default).
				code := string(rs[i : j+1])
				sgr := ""
				if code != "\x1b[m" && code != "\x1b[0m" {
					sgr = code
				}
				cur = sgrSegment{sgr: sgr}
				i = j + 1
				continue
			}
		}
		cur.runes = append(cur.runes, rs[i])
		i++
	}
	segs = append(segs, cur)
	return segs
}

// rowCells expands a styled row into its cells: one rune and the SGR state
// it renders in, per column.
func rowCells(row string) (runes []rune, sgrs []string) {
	for _, seg := range splitRow(row) {
		for _, r := range seg.runes {
			runes = append(runes, r)
			sgrs = append(sgrs, seg.sgr)
		}
	}
	return runes, sgrs
}

// mergeRows overwrites the cells [left, left+boxW) of base with over's
// cells, keeping base everywhere else, and emits a style code only when the
// cell's state actually changes from the previous cell — the output stays
// as thin as the inputs, so the merge costs nothing at 60 fps.
func mergeRows(base, over string, left, boxW int) string {
	baseRunes, baseSGRs := rowCells(base)
	overRunes, overSGRs := rowCells(over)
	width := len(baseRunes)
	if len(overRunes) > width {
		width = len(overRunes)
	}
	var b strings.Builder
	var cur string
	for col := 0; col < width; col++ {
		var r rune
		var sgr string
		switch {
		case col >= left && col-left < len(overRunes) && col < left+boxW:
			r, sgr = overRunes[col-left], overSGRs[col-left]
		case col < len(baseRunes):
			r, sgr = baseRunes[col], baseSGRs[col]
		default:
			r = ' '
		}
		if sgr != cur {
			// Returning to the default state must emit a reset: the
			// default's own code is the empty string, which would write
			// nothing and let the previous style bleed past the cell.
			if sgr == "" {
				b.WriteString("\x1b[m")
			} else {
				b.WriteString(sgr)
			}
			cur = sgr
		}
		b.WriteRune(r)
	}
	return b.String()
}

// nameList returns the display names of the visualizers.
func nameList(vs []visualizer) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = v.Name()
	}
	return out
}
