// Package tui. The wordmark: the 48-cell box drawing of "bc-radio" that
// opens the player and library headers and closes the exit banner.
//
// The drawing is one outer box (its left edge is the leading ─/║/╚ column)
// containing the eight letter shapes of "bc radio" as their own box
// figures, the light ─ runs separating the letters. Its glyphs fall into
// two classes, coloured differently (issue #13): the light bar glyph ─ is
// the quiet structure — the box frame and the runs separating the
// letters, dim gray, like the dash rules that continue after the
// drawing; every other glyph (the heavy ═ runs, ║ and the corners) is
// part of a letter shape, painted in the theme's blue-to-peach gradient
// (the spectrum's own gradient, sampled by column), so the mark glows in
// the palette the music is drawn in. The heavy ═ is a letter part, not a
// separator: no separator in the drawing is heavy, so a ═ run always
// belongs to the letters and never reads as a quiet bar. The styled rows
// are precomputed per theme in applyTheme — a render is a plain
// concatenation, never a re-styles of 300 runes at 60 fps.
//
// The six raw strings are byte-exact transcriptions of the design files —
// one clean 48 × 6 box, every row the same width — and must never be
// retyped: box-drawing glyphs are indistinguishable when rendered, and a
// wrong one (╠ vs ║) corrupts the drawing. (A ragged transcription is the
// 2026-11-19 regression: rows that lost their letters' verticals shifted
// the header's left edge and the negative space stopped reading as one
// box; TestWordmarkRowsAreUniform pins the uniformity.)
package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// wordmark is the six rows of the bc-radio box drawing, each exactly 48
// cells wide — the six rows form one clean 48 × 6 box (TestWordmarkRowsAreUniform
// pins the uniformity: a ragged row shifts the header's edges). The letters
// are drawn in the double (heavy) box-drawing glyphs, with light ─ runs
// separating them — the light runs are the drawing's only light glyphs.
// Both consumers (the player/library headers and the exit banner) render
// all six rows verbatim, each glyph one cell.
const (
	wordmarkR0 = "─╔══╗──╔═══╗──────╔═══╗─╔═══╗─╔═══╗─╔══╗─╔═══╗──"
	wordmarkR1 = "─║╔╗║──║╔═╗║──────║╔═╗║─║╔═╗║─╚╗╔╗║─╚╣╠╝─║╔═╗║──"
	wordmarkR2 = "─║╚╝╚╗─║║─╚╝─╔══╗─║╚═╝║─║║─║║──║║║║──║║──║║─║║──"
	wordmarkR3 = "─║╔═╗║─║║─╔╗─╚══╝─║╔╗╔╝─║╚═╝║──║║║║──║║──║║─║║──"
	wordmarkR4 = "─║╚═╝║─║╚═╝║──────║║║╚╗─║╔═╗║─╔╝╚╝║─╔╣╠╗─║╚═╝║──"
	wordmarkR5 = "─╚═══╝─╚═══╝──────╚╝╚═╝─╚╝─╚╝─╚═══╝─╚══╝─╚═══╝──"
)

// wordmarkWidth is the drawing's width in cells; every row is exactly that
// wide (each glyph is one cell). It is the wide header layout's floor —
// below it the drawing cannot fit and the layout falls back to the
// single-row title.
const wordmarkWidth = 48

// wordmarkRows returns the six raw wordmark rows, in order.
func wordmarkRows() [6]string {
	return [6]string{wordmarkR0, wordmarkR1, wordmarkR2, wordmarkR3, wordmarkR4, wordmarkR5}
}

// isBarGlyph reports whether the rune is the drawing's quiet bar glyph:
// the light horizontal rule. The heavy ═ runs sit inside the letter
// shapes (a letter's top or bottom edge) — no separator in the drawing is
// heavy — so they are letters, not bars (the 2026-11-19 follow-up to
// issue #13: the ═ runs of the letters read as gray segments where the
// gradient belongs).
func isBarGlyph(r rune) bool {
	return r == '─'
}

// wordmarkBarStyle is the drawing's bar-glyph style for the scheme: dim
// gray in a colour palette (the same quiet gray as the dash rules that
// continue after the art), and a faint weight in mono, where the letters'
// bold weight carries the hierarchy instead.
func wordmarkBarStyle(s scheme) lipgloss.Style {
	if s.mono {
		return lipgloss.NewStyle().Faint(true)
	}
	return lipgloss.NewStyle().Foreground(s.dim)
}

// wordmarkGlyphStyle is the drawing's letter-glyph style for the scheme at
// fraction f along the row (0 = left edge, 1 = right edge): the theme's
// blue-to-peach gradient, sampled per cell. In mono it is a plain bold
// weight — the terminal default, no colour.
func wordmarkGlyphStyle(s scheme, f float64) lipgloss.Style {
	if s.mono {
		return lipgloss.NewStyle().Bold(true)
	}
	return lipgloss.NewStyle().Foreground(gradientColor(s, f))
}

// styledWordmark renders the six drawing rows in the scheme: bar glyphs
// dim, letter glyphs in the column-sampled gradient. The caller brings
// each row to its width (the headers fill after the drawing with the dim
// dash gap, the exit banner indents by one space).
func styledWordmark(s scheme) [6]string {
	arts := wordmarkRows()
	bar := wordmarkBarStyle(s)
	out := [6]string{}
	for i, row := range arts {
		r := []rune(row)
		var sb strings.Builder
		for x, ch := range r {
			if isBarGlyph(ch) {
				sb.WriteString(bar.Render(string(ch)))
				continue
			}
			f := float64(x) / float64(len(r)-1)
			sb.WriteString(wordmarkGlyphStyle(s, f).Render(string(ch)))
		}
		out[i] = sb.String()
	}
	return out
}
