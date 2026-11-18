// Package tui. The wordmark and exit banner of the player screen.
//
// The wordmark is a 48-cell, 6-row box drawing of "bc-radio". It is shared
// by the player header (drawn whenever the terminal is wide enough, in
// both normal and fullscreen) and the exit banner (1-space indented,
// printed after the alternate screen is torn down); the two are the same
// six rows, so one constant serves both.
//
// These strings are byte-exact transcriptions of the design files and must
// never be retyped: box-drawing glyphs are indistinguishable when rendered,
// and a wrong one (╠ vs ║) corrupts the drawing. Regenerate with:
//
//	python3 -c "..." over exit-message.sh
//
// or by copying from the checked-in design reference in the repo root.
package tui

// wordmark is the six rows of the bc-radio box drawing, each exactly 48
// cells wide. The art is drawn in the double (heavy) box-drawing glyphs,
// with light ─ runs separating the letters — the light runs are its only
// light glyphs. The time bar (━) that sits beside it in the design file
// belongs to the live header's info block, not to the drawing. Both
// consumers (the player header and the exit banner) render all six rows
// verbatim.
const (
	wordmarkR0 = "─╔══╗──╔═══╗──────╔═══╗─╔═══╗─╔═══╗─╔══╗─╔═══╗──"
	wordmarkR1 = "─║╔╗║──║╔═╗║──────║╔═╗║─║╔═╗║─╚╗╔╗║─╚╣╠╝─║╔═╗║──"
	wordmarkR2 = "─║╚╝╚╗─║║─╚╝─╔══╗─║╚═╝║─║║─║║──║║║║──║║──║║─║║──"
	wordmarkR3 = "─║╔═╗║─║║─╔╗─╚══╝─║╔╗╔╝─║╚═╝║──║║║║──║║──║║─║║──"
	wordmarkR4 = "─║╚═╝║─║╚═╝║──────║║║╚╗─║╔═╗║─╔╝╚╝║─╔╣╠╗─║╚═╝║──"
	wordmarkR5 = "─╚═══╝─╚═══╝──────╚╝╚═╝─╚╝─╚╝─╚═══╝─╚══╝─╚═══╝──"
)

// wordmarkWidth is the drawing's width in cells; every row is exactly that
// wide (each glyph is one cell). It is the wide player layout's floor —
// below it the drawing cannot fit and the layout falls back to the
// single-row title.
const wordmarkWidth = 48

// wordmarkRows returns the six wordmark rows, in order. The caller brings
// each to the terminal width: the header fills after the drawing (dim
// dashes, then the right-justified info block — or plain dashes on the
// top and bottom rules), the exit banner indents by one space.
func wordmarkRows() [6]string {
	return [6]string{wordmarkR0, wordmarkR1, wordmarkR2, wordmarkR3, wordmarkR4, wordmarkR5}
}
