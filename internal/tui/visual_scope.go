package tui

import (
	"strings"
)

// brailleBit maps a (dot row, dot column) in the 4×2 Braille dot grid to
// its bit, so a character cell can carry eight dots — four rows, two
// columns. The bits follow the standard U+28xx pattern: dots 1–4 in the
// left column and 5–8 in the right, top to bottom. (cliamp's own table is
// transposed, which shears a straight trace into a zigzag; we keep the
// standard mapping so the scope line reads smooth.)
var brailleBit = [4][2]rune{
	{0x01, 0x10}, // row 0
	{0x02, 0x20}, // row 1
	{0x04, 0x40}, // row 2
	{0x08, 0x80}, // row 3
}

// scope is a time-domain oscilloscope of the most recent post-gain mono
// samples — the analyser's window ending at what is audible now, via the
// contract's Scope. The
// trace plots raw [-1,1] values on a Braille dot grid, two dots per column
// and four per row (the resolution cliamp's scope uses): the region has a
// fixed scale, so a loud passage fills it and a quiet one sits near the
// baseline. Where the band-driven visuals show what the music is, the scope
// shows what it is doing, sample by sample.
type scope struct {
	dots []bool // the dot mask, dotCols*dotRows, reused between frames
}

// newScope builds a scope visualizer.
func newScope() *scope { return &scope{} }

// Name implements visualizer.
func (s *scope) Name() string { return "scope" }

// Render implements visualizer. Each sample plots one dot in the grid,
// newest at the right edge, so the trace fills the width from the right as
// the window grows; values map linearly from ±1 to the top and bottom of
// the region. Consecutive points interpolate their connecting line, and the
// centre row carries a dashed baseline wherever the trace is not.
func (s *scope) Render(f frame) string {
	samples := f.scope
	w, h := f.w, f.h
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	dotCols, dotRows := 2*w, 4*h
	n := len(samples)
	// The most recent dotCols samples, one per dot column, oldest on the
	// left: sample i of the window plots in column dotCols-n+i, so while
	// the window is still filling in the trace grows from the right.
	m := min(n, dotCols)
	if cap(s.dots) < dotCols*dotRows {
		s.dots = make([]bool, dotCols*dotRows)
	} else {
		s.dots = s.dots[:dotCols*dotRows]
		clear(s.dots)
	}
	prevX, prevY := 0, 0
	first := true
	for i := n - m; i < n; i++ {
		x := dotCols - n + i
		v := samples[i]
		if v < -1 {
			v = -1
		}
		if v > 1 {
			v = 1
		}
		y := int((1.0 - v) * 0.5 * float64(dotRows-1))
		if y < 0 {
			y = 0
		}
		if y > dotRows-1 {
			y = dotRows - 1
		}
		s.dots[y*dotCols+x] = true
		// Interpolate between consecutive points for a smooth trace; a jump
		// steeper than 30 dots is a spike, and its endpoints are enough.
		if !first {
			dx, dy := x-prevX, y-prevY
			steps := max(absInt(dx), absInt(dy))
			if steps > 0 && steps < 30 {
				for st := 1; st < steps; st++ {
					mx := prevX + dx*st/steps
					my := prevY + dy*st/steps
					if mx >= 0 && mx < dotCols && my >= 0 && my < dotRows {
						s.dots[my*dotCols+mx] = true
					}
				}
			}
		}
		prevX, prevY, first = x, y, false
	}
	centre := (h - 1) / 2
	var out strings.Builder
	out.Grow(w * (h + 1))
	for y := 0; y < h; y++ {
		line := make([]rune, w)
		trace := false
		for x := 0; x < w; x++ {
			var br rune = '\u2800'
			for dr := 0; dr < 4; dr++ {
				for dc := 0; dc < 2; dc++ {
					if s.dots[(y*4+dr)*dotCols+x*2+dc] {
						br |= brailleBit[dr][dc]
					}
				}
			}
			switch {
			case br != '\u2800':
				line[x] = br
				trace = true
			case y == centre:
				// The centre row is dashed wherever the trace is not,
				// whether or not the trace touches it.
				line[x] = '─'
			default:
				line[x] = ' '
			}
		}
		style := styleDim
		if trace {
			style = styleVisual
		}
		out.WriteString(style.Render(string(line)))
		if y < h-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// absInt returns the absolute value of x.
func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
