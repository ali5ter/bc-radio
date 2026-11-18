// Package tui. The exit banner: what the user sees when bc-radio quits
// cleanly, printed to the main screen after the alternate screen is torn
// down.
//
// The banner is the wordmark (1-space indented, in the accent) and a
// two-line thank-you: support the artists, and where the software lives.
// It prints only on a clean exit — a launch that fails goes to its error
// line instead, and the export path (-json/-list) never runs the
// interface at all.
package tui

import "strings"

// ExitBanner renders the exit banner, clipped to the terminal width the
// interface last reported (floor 40: before the first window-size message
// a.width is 0, and the wordmark row is 49 cells with its indent). The
// clip runs before styling — every glyph is one cell, so the rune clip
// cannot corrupt a styled string.
func (a *App) ExitBanner() string {
	w := a.width
	if w < 40 {
		w = 40
	}
	arts := wordmarkRows()
	rows := make([]string, 0, 10)
	for _, art := range arts {
		rows = append(rows, styleAccentB.Render(clip(" "+art, w)))
	}
	rows = append(rows,
		"",
		clip(" Thanks for using bc-radio. Support artists on BandCamp.com", w),
		styleDim.Render(clip(" GitHub · ali5ter/bc-radio", w)),
		"",
	)
	return strings.Join(rows, "\n")
}
