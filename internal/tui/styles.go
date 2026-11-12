package tui

import (
	"fmt"
	"image/color"

	"bc-radio/internal/contract"
	"charm.land/lipgloss/v2"
)

// The palette is warm-on-dark: an amber accent for anything the user is
// acting on (the current track, a selection), a cool teal for the
// visualizer, and one muted tone for everything decorative. One accent and
// one cool colour keep the screen calm at 60 fps — a rainbow here would
// flicker into noise instead of music.
var (
	colorAccent = lipgloss.Color("#e8a15c") // current track, selection: warm amber
	colorVisual = lipgloss.Color("#58b3a2") // visualizer default: cool teal
	colorDim    = lipgloss.Color("#6e6a61") // hints, meta, quiet structure
	colorBad    = lipgloss.Color("#d9705f") // failures, warnings

	// The spectrum runs a gradient from the low to the high band colour;
	// the two ends are chosen so a full-scale low-mid mix reads as warm.
	spectrumLow  = "#3d6fb4" // deep blue: the bass end
	spectrumHigh = "#c96f3d" // warm copper: the treble end
)

// Derived styles. Every colour in the UI comes from here, never inline.
var (
	styleAccent   = lipgloss.NewStyle().Foreground(colorAccent)
	styleAccentB  = styleAccent.Bold(true)
	styleVisual   = lipgloss.NewStyle().Foreground(colorVisual)
	styleDim      = lipgloss.NewStyle().Foreground(colorDim)
	styleDimB     = styleDim.Bold(true)
	styleBad      = lipgloss.NewStyle().Foreground(colorBad)
	styleText     = lipgloss.NewStyle()
	styleSelected = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Background(lipgloss.Color("#2a2620"))
)

// bandColor returns the spectrum colour for band k of contract.BandCount,
// interpolated between spectrumLow and spectrumHigh. The ends are parsed
// once per call site; the cost is a handful of hex parses per frame, which
// is negligible next to the string work of a render.
func bandColor(k, n int) color.Color {
	lo, hi := parseHex(spectrumLow), parseHex(spectrumHigh)
	var f float64
	if n > 1 {
		f = float64(k) / float64(n-1)
	}
	mix := func(a, b int) int { return int(float64(a) + float64(b-a)*f + 0.5) }
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", mix(lo[0], hi[0]), mix(lo[1], hi[1]), mix(lo[2], hi[2])))
}

// parseHex reads a #rrggbb colour into three 0-255 values.
func parseHex(c string) [3]int {
	var v [3]int
	fmt.Sscanf(c[1:], "%02x%02x%02x", &v[0], &v[1], &v[2])
	return v
}

// styleBands is the per-band palette of the spectrum visualizer, low to
// high frequency, built once: the palette is static, so a render is string
// work only.
var styleBands = buildBandStyles()

// buildBandStyles returns one style per band, low to high frequency.
func buildBandStyles() []lipgloss.Style {
	styles := make([]lipgloss.Style, contract.BandCount)
	for k := range styles {
		styles[k] = lipgloss.NewStyle().Foreground(bandColor(k, contract.BandCount))
	}
	return styles
}
