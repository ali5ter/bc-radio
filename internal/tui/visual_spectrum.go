package tui

import "strings"

// peakFall is how far a spectrum peak cap descends per render: 0.008 at
// 60 fps takes a full-scale cap down in about two seconds — long enough to
// read the decay, short enough that it does not smudge the bar below it.
const peakFall = 0.008

// blockLevels is the nine fractional block heights from empty to full. A
// cell is eight steps, so a bar edge lands at 1/8-cell precision — the
// resolution cliamp's bar spectrum uses, 4.5× finer than the old half
// cells, which is most of what makes the bars read as "high resolution".
var blockLevels = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// fracBlock returns the block glyph for a level within one row's span
// [bottom, top) of the display height: rows the level fully fills read █,
// the row its top edge sits in reads the matching fraction, and the rest
// read blank.
func fracBlock(level, bottom, top float64) rune {
	if level >= top {
		return '█'
	}
	if level > bottom {
		frac := (level - bottom) / (top - bottom)
		idx := int(frac * float64(len(blockLevels)-1))
		if idx < 0 {
			idx = 0
		}
		if idx > len(blockLevels)-1 {
			idx = len(blockLevels) - 1
		}
		return blockLevels[idx]
	}
	return ' '
}

// bandLayout reports how many of n bands get columns in a panel cols wide
// and how many one-column gaps fit between them. The bands take priority, so
// a narrow panel keeps every band it can and drops gaps first — the same
// layout cliamp uses for its bar spectrum, so the bars are wide and bold
// instead of one cell each.
func bandLayout(n, cols int) (visible, gaps int) {
	if n <= 0 || cols <= 0 {
		return 0, 0
	}
	visible = min(n, cols)
	gaps = min(visible-1, max(0, cols-visible))
	return visible, gaps
}

// bandWidth returns the character width for band b of n in a panel cols
// wide; the floor remainder goes to the leading bands, so the panel fills
// exactly with the visible bands plus their gaps.
func bandWidth(n, b, cols int) int {
	visible, gaps := bandLayout(n, cols)
	if b < 0 || b >= visible {
		return 0
	}
	bandCols := cols - gaps
	base := bandCols / visible
	extra := bandCols % visible
	if b < extra {
		return base + 1
	}
	return base
}

// bandGapAfter reports whether a one-column gap follows band b of n in a
// panel cols wide. When fewer gaps fit than there are slots between the
// visible bands, the gaps spread evenly over the slots.
func bandGapAfter(n, b, cols int) bool {
	visible, gaps := bandLayout(n, cols)
	slots := visible - 1
	if b < 0 || b >= slots || gaps <= 0 {
		return false
	}
	return (b+1)*gaps/slots > b*gaps/slots
}

// spectrum is the classic bar spectrum: contract.BandCount vertical bars,
// low to high left to right, each filled from the baseline with fractional
// block elements and coloured along the low-to-high gradient. Wide bars with
// one-column gaps (the cliamp layout) instead of one cell each, so the
// bands read as bold columns rather than a hairline comb. A peak cap hovers
// over each bar at the band's highest recent level, drawn at the same
// fractional resolution, and falls slowly (peakFall per render), so
// transients read as flashes with a visible decay instead of a blur.
type spectrum struct {
	peak []float64 // per-band highest level since the cap fell past it; [0,1]
}

// newSpectrum builds a spectrum visualizer.
func newSpectrum() *spectrum { return &spectrum{} }

// Name implements visualizer.
func (s *spectrum) Name() string { return "spectrum" }

// Render implements visualizer.
func (s *spectrum) Render(f frame) string {
	bands := f.bands
	w, h := f.w, f.h
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	// Grow the peak history to the band count; it is written in place
	// afterwards, so a reallocation here is a one-frame cost.
	if len(s.peak) != len(bands) {
		s.peak = make([]float64, len(bands))
	}
	for k, b := range bands {
		if b > s.peak[k] {
			s.peak[k] = b
		}
	}
	// A cap reads as detached only once it sits a full block step above the
	// bar; below that the two are the same mark at 1/8-cell resolution.
	minGap := 1.0 / (8.0 * float64(h))
	var out strings.Builder
	out.Grow(w * (h + 1))
	n := len(bands)
	for y := 0; y < h; y++ {
		rowBottom := float64(h-1-y) / float64(h)
		rowTop := float64(h-y) / float64(h)
		for k := 0; k < n; k++ {
			cell := fracBlock(bands[k], rowBottom, rowTop)
			// The cap occupies the one row its level sits in; on that row it
			// replaces the bar mark, and nowhere else.
			if p := s.peak[k]; p > bands[k]+minGap && p < rowTop {
				cell = fracBlock(p, rowBottom, rowTop)
			}
			if width := bandWidth(n, k, w); width > 0 {
				out.WriteString(styleBands[k].Render(strings.Repeat(string(cell), width)))
			}
			if k < n-1 && bandGapAfter(n, k, w) {
				out.WriteByte(' ')
			}
		}
		if y < h-1 {
			out.WriteByte('\n')
		}
	}
	// The caps fall after the frame is drawn, so this render shows the
	// level as high as it has been since they last fell.
	for k := range s.peak {
		s.peak[k] -= peakFall
		if s.peak[k] < 0 {
			s.peak[k] = 0
		}
	}
	return out.String()
}
