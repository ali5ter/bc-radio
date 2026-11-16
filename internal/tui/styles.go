package tui

import (
	"fmt"
	"image/color"

	"bc-radio/internal/contract"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// themeKind is the user's theme choice from the -theme flag.
type themeKind string

// The theme kinds -theme accepts.
const (
	themeAuto  themeKind = "auto"  // follow the terminal: NO_COLOR, then its background
	themeDark  themeKind = "dark"  // the warm palette, for dark backgrounds
	themeLight themeKind = "light" // the same hues, darkened for light backgrounds
	themeMono  themeKind = "mono"  // the terminal's default colour, hierarchy by weight
)

// ValidateTheme reports whether name is a theme kind, for command-line
// validation.
func ValidateTheme(name string) error {
	switch themeKind(name) {
	case themeAuto, themeDark, themeLight, themeMono:
		return nil
	}
	return fmt.Errorf("unknown theme %q (choose auto, dark, light or mono)", name)
}

// scheme is one resolved palette: the four semantic colours, the two ends of
// the spectrum gradient, and the mono flag that turns the gradient off.
// Every colour in the UI derives from a scheme, never from a literal.
type scheme struct {
	accent       color.Color // current track, selection: what the user is acting on
	visual       color.Color // the visualizer: the cool counterweight
	dim          color.Color // hints, meta, quiet structure
	bad          color.Color // failures, warnings
	spectrumLow  color.Color // the bass end of the band gradient
	spectrumHigh color.Color // the treble end
	mono         bool        // no colour at all: the hierarchy comes from weight
}

// The palette is warm-on-dark: an amber accent for anything the user is
// acting on, a cool teal for the visualizer, and one muted tone for
// everything decorative. One accent and one cool colour keep the screen
// calm at 60 fps — a rainbow here would flicker into noise instead of
// music. The light scheme keeps the same hues at a darker value: the same
// hues on white wash out, the darker values hold their contrast. The
// spectrum runs from a deep blue bass end to a warm copper treble end, so a
// full-scale low-mid mix reads as warm against the teal.
func darkScheme() scheme {
	return scheme{
		accent:       lipgloss.Color("#e8a15c"),
		visual:       lipgloss.Color("#58b3a2"),
		dim:          lipgloss.Color("#6e6a61"),
		bad:          lipgloss.Color("#d9705f"),
		spectrumLow:  lipgloss.Color("#3d6fb4"),
		spectrumHigh: lipgloss.Color("#c96f3d"),
	}
}

func lightScheme() scheme {
	return scheme{
		accent:       lipgloss.Color("#9a531a"),
		visual:       lipgloss.Color("#0e6e5c"),
		dim:          lipgloss.Color("#5d5950"),
		bad:          lipgloss.Color("#b23a28"),
		spectrumLow:  lipgloss.Color("#2e589c"),
		spectrumHigh: lipgloss.Color("#8f4718"),
	}
}

// monoScheme is the NO_COLOR palette: every colour is the terminal default,
// so nothing here may render a colour code — the hierarchy is carried by
// weight (bold for importance, faint for decoration) instead.
func monoScheme() scheme {
	s := scheme{mono: true}
	s.accent, s.visual, s.dim, s.bad, s.spectrumLow, s.spectrumHigh =
		lipgloss.NoColor{}, lipgloss.NoColor{}, lipgloss.NoColor{},
		lipgloss.NoColor{}, lipgloss.NoColor{}, lipgloss.NoColor{}
	return s
}

// resolveScheme maps a theme choice to a palette. auto honours the terminal:
// NO_COLOR (which collapses the colour profile to ASCII) or a non-TTY yields
// mono, otherwise the palette follows the background. The explicit kinds
// never do — an explicit choice is a choice.
func resolveScheme(kind themeKind, profile colorprofile.Profile, bgDark bool) scheme {
	switch kind {
	case themeLight:
		return lightScheme()
	case themeMono:
		return monoScheme()
	case themeAuto:
		if profile == colorprofile.ASCII || profile == colorprofile.NoTTY {
			return monoScheme()
		}
		if bgDark {
			return darkScheme()
		}
		return lightScheme()
	default:
		return darkScheme()
	}
}

// The live palette and styles. Every colour in the UI comes from here, never
// inline; applyTheme rebuilds all of them, so no call site knows which theme
// is active.
var (
	colorAccent, colorVisual, colorDim, colorBad color.Color
	styleAccent, styleAccentB, styleVisual       lipgloss.Style
	styleDim, styleDimB, styleBad, styleText     lipgloss.Style
	styleBands                                   []lipgloss.Style
	current                                      scheme
)

// init installs the dark palette at package load, so code that renders
// without going through New (the visualizer tests do) still has a valid
// palette: New and every render path that matters resolve their own theme.
func init() {
	applyTheme(darkScheme())
}

// applyTheme installs s as the live palette. It must run on the model
// goroutine only: New and Update both run there, and bubbletea v2 runs View
// on the same goroutine, so the rebuild can never race a render.
func applyTheme(s scheme) {
	current = s
	colorAccent, colorVisual, colorDim, colorBad = s.accent, s.visual, s.dim, s.bad
	styleText = lipgloss.NewStyle()
	if s.mono {
		// No SGR at all — the terminal default colour everywhere, with the
		// weight table above as the only hierarchy.
		styleAccent, styleAccentB = lipgloss.NewStyle().Bold(true), lipgloss.NewStyle().Bold(true)
		styleVisual = lipgloss.NewStyle()
		styleDim, styleDimB, styleBad =
			lipgloss.NewStyle().Faint(true), lipgloss.NewStyle().Bold(true), lipgloss.NewStyle().Bold(true)
	} else {
		styleAccent = lipgloss.NewStyle().Foreground(s.accent)
		styleAccentB = styleAccent.Bold(true)
		styleVisual = lipgloss.NewStyle().Foreground(s.visual)
		styleDim = lipgloss.NewStyle().Foreground(s.dim)
		styleDimB = styleDim.Bold(true)
		styleBad = lipgloss.NewStyle().Foreground(s.bad)
	}
	styleBands = buildBandStyles()
}

// bandColor returns the spectrum colour for band k of n, interpolated between
// the scheme's gradient ends. In mono it is the terminal default, so the
// gradient is weight-neutral and the bands are plain.
func bandColor(k, n int) color.Color {
	if current.mono {
		return lipgloss.NoColor{}
	}
	lo, hi := rgba(current.spectrumLow), rgba(current.spectrumHigh)
	var f float64
	if n > 1 {
		f = float64(k) / float64(n-1)
	}
	mix := func(a, b int) int { return int(float64(a) + float64(b-a)*f + 0.5) }
	return color.RGBA{R: uint8(mix(lo[0], hi[0])), G: uint8(mix(lo[1], hi[1])), B: uint8(mix(lo[2], hi[2])), A: 0xff}
}

// rgba expands a colour to 0-255 channels (the 16-bit RGBA values shrunk).
func rgba(c color.Color) [3]int {
	r, g, b, _ := c.RGBA()
	return [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
}

// buildBandStyles returns one style per band, low to high frequency, in the
// scheme's gradient. The palette is static between theme changes, so a
// render is string work only.
func buildBandStyles() []lipgloss.Style {
	styles := make([]lipgloss.Style, contract.BandCount)
	for k := range styles {
		styles[k] = lipgloss.NewStyle().Foreground(bandColor(k, contract.BandCount))
	}
	return styles
}
