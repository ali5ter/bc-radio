package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// TestWordmarkRowsAreUniform pins the box invariant the header's alignment
// depends on: every wordmark row is exactly wordmarkWidth cells wide. A
// ragged row shifts the info block's left edge on that row only, and the
// header's negative space stops reading as one box (2026-11-19: rows 1, 3
// and 4 lost their letters' verticals in the transcription, and the header
// jagged — the design files are one clean 48 × 6 box).
func TestWordmarkRowsAreUniform(t *testing.T) {
	for i, row := range wordmarkRows() {
		if w := lipgloss.Width(row); w != wordmarkWidth {
			t.Errorf("row %d is %d cells; want %d (the 48 × 6 box): %q", i, w, wordmarkWidth, row)
		}
	}
}

// TestWordmarkTail pins the box's right edge: every row closes with the
// two-cell quiet tail the header's dash rules continue from.
func TestWordmarkTail(t *testing.T) {
	for i, row := range wordmarkRows() {
		if !strings.HasSuffix(row, "──") {
			t.Errorf("row %d does not end on the two-dash tail: %q", i, row)
		}
	}
}

// TestIsBarGlyph pins the two-class colour split: the only quiet bar glyph
// is the light ─; the heavy ═ runs and the verticals are letter shapes
// (the gradient), never quiet bars.
func TestIsBarGlyph(t *testing.T) {
	if !isBarGlyph('─') {
		t.Error("the light bar ─ must be a bar glyph (the quiet structure)")
	}
	if isBarGlyph('═') {
		t.Error("the heavy rule ═ is a letter part (the gradient), not a bar")
	}
	if isBarGlyph('║') {
		t.Error("the vertical ║ is a letter part (the gradient), not a bar")
	}
}
