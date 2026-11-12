package tui

import (
	"math"
	"strings"
)

// waveform is a standing-wave display: each column of the width sums all
// bands as spatial harmonics, band k contributing (k+1) cycles across the
// width — the bass swings slowly, the treble shimmers. The sum is
// normalised by the total band energy, so the trace scales with loudness
// without clipping. At rest (no energy) it shows a plain dashed baseline.
type waveform struct{}

// newWaveform builds a waveform visualizer.
func newWaveform() *waveform { return &waveform{} }

// Name implements visualizer.
func (w *waveform) Name() string { return "waveform" }

// Render implements visualizer. One marker per column: a dot at the summed
// height, a dimmer dot on the baseline row, and a dash elsewhere on the
// baseline row.
func (w *waveform) Render(bands []float64, width, h int) string {
	if width < 1 {
		width = 1
	}
	if h < 1 {
		h = 1
	}
	centre := (h - 1) / 2
	amp := (h - 1) / 2
	sum := 0.0
	for _, b := range bands {
		sum += b
	}
	var out strings.Builder
	out.Grow(width * (h + 1))
	for y := 0; y < h; y++ {
		line := make([]rune, width)
		for x := range line {
			line[x] = ' '
		}
		if sum < 0.004 && y == centre {
			// Silence: the baseline row only.
			for x := range line {
				line[x] = '─'
			}
		} else if sum >= 0.004 {
			for x := 0; x < width; x++ {
				var v float64
				for k, b := range bands {
					v += b * math.Cos(2*math.Pi*float64(k+1)*float64(x)/float64(width))
				}
				row := centre + int(v/sum*float64(amp)+0.5)
				if row < 0 {
					row = 0
				}
				if row > h-1 {
					row = h - 1
				}
				if y == row {
					line[x] = '•'
				} else if y == centre {
					line[x] = '·'
				}
			}
		}
		// A line the trace touched gets the accent; quiet lines stay dim.
		style := styleDim
		if strings.ContainsRune(string(line), '•') {
			style = styleVisual
		}
		out.WriteString(style.Render(string(line)))
		if y < h-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}
