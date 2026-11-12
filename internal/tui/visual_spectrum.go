package tui

import (
	"strings"

	"bc-radio/internal/contract"
)

// spectrum is the classic bar spectrum: contract.BandCount vertical bars,
// one per band, low to high left to right, each filled from the baseline
// with full and half blocks and coloured along the low-to-high gradient.
type spectrum struct{}

// newSpectrum builds a spectrum visualizer.
func newSpectrum() *spectrum { return &spectrum{} }

// Name implements visualizer.
func (s *spectrum) Name() string { return "spectrum" }

// Render implements visualizer. Bar heights use half-block resolution, so
// a band value of v fills round(v · 2h) half-cells from the bottom. The
// bars tile the width in equal cells; the floor remainder is absorbed into
// the last bar so the output is exactly w wide.
func (s *spectrum) Render(bands []float64, w, h int) string {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	nBands := contract.BandCount
	if w < nBands {
		nBands = w // one cell per band; drop what does not fit
	}
	cellW := w / nBands
	if cellW < 1 {
		cellW = 1
	}
	var out strings.Builder
	for y := 0; y < h; y++ {
		used := 0
		cell := h - 1 - y // rows drawn top to bottom; cells counted from the bottom
		for k := 0; k < nBands; k++ {
			wk := cellW
			if k == nBands-1 {
				wk = w - used // absorb the floor remainder
			}
			if wk < 1 {
				wk = 0
			}
			var r rune
			if wk > 0 && k < len(bands) {
				n := int(bands[k]*(2*float64(h)) + 0.5)
				if n > 2*h {
					n = 2 * h
				}
				switch {
				case n >= 2*cell+2:
					r = '█'
				case n == 2*cell+1:
					r = '▄'
				default:
					r = ' '
				}
			}
			if wk > 0 {
				out.WriteString(styleBands[k].Render(strings.Repeat(string(r), wk)))
			}
			used += wk
		}
		if used < w {
			out.WriteString(strings.Repeat(" ", w-used))
		}
		out.WriteByte('\n')
	}
	return strings.TrimSuffix(out.String(), "\n")
}
