// Package tui. The exit banner: what the user sees when bc-radio quits
// cleanly, printed to the main screen after the alternate screen is torn
// down.
//
// The banner is a blank line, the wordmark (1-space indented, in the
// header's own colours — quiet bars, letters in the theme's gradient) and
// a two-line thank-you: support the artists, and where the software
// lives. It prints only on a clean exit — a launch that fails goes to its
// error line instead, and the export path (-json/-list) never runs the
// interface at all.
package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// ExitBanner renders the exit banner, clipped to the terminal width the
// interface last reported (floor 40: before the first window-size message
// a.width is 0, and the wordmark row is 49 cells with its indent). The
// wordmark is the header's own styled drawing (bars dim, letters in the
// gradient); the clip is ANSI-aware — every glyph is one cell, so a cut
// cannot corrupt the drawing.
func (a *App) ExitBanner() string {
	w := a.width
	if w < 40 {
		w = 40
	}
	clip := lipgloss.NewStyle().MaxWidth(w)
	rows := make([]string, 0, 11)
	rows = append(rows, "")
	for _, art := range wordmarkArt {
		rows = append(rows, clip.Render(" "+art))
	}
	rows = append(rows,
		"",
		clip.Render(" Thanks for using bc-radio. Support artists on BandCamp.com"),
		styleDim.Render(clip.Render(" GitHub · ali5ter/bc-radio")),
		"",
	)
	return strings.Join(rows, "\n")
}
