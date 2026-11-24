package tui

import (
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ali5ter/bc-radio/internal/contract"
)

// interference is the pattern from the issue's ascii-magic backlog
// (wave-interference): two point sources send out circular waves that
// reinforce and cancel, carving the classic fan of nodal lines as the
// sources drift apart. The upstream piece is a pure function of the wall
// clock; here the clock and the pattern's physical parameters are driven
// by the analyser, so the field is the music:
//
//   - the bass sets the wavelength — heavy lows throw dense rings, a thin
//     low end sparse ones;
//   - the mid sets the ring emission rate, so the pattern's tempo follows
//     the body of the music;
//   - the treble sets the sources' separation — sparkle splits them into
//     the interference fan, and at rest they sit close together, reading
//     as one faint concentric pulse;
//   - the overall level is the field's amplitude, so a quiet passage is a
//     dim field and a loud one saturates.
//
// Like particles it holds state: a phase clock that advances one frame per
// Render. A paused Render holds the clock, so the frame is byte-identical
// to the last playing one — the house pause cue. Idle, the bands rest at
// zero and the field reads as the faint slow pulse the mapping above
// describes at zero input, so no special idle handling is needed.
//
// Each band region drives one parameter, and every parameter is a fraction
// of the panel width, so the piece reads the same at an 8-row pane or
// fullscreen. The two sources are identical, so the field is symmetric
// about the panel's centre column: only the right half is computed and
// mirrored, which halves the per-cell work and keeps the symmetry exact
// (float rounding would otherwise let the halves flicker against each
// other at the seam).
type interference struct {
	t    float64 // phase clock, seconds: one Render call advances it one frame
	grid []rune  // the field's glyphs, w*h, reused between frames
	tier []int   // the style tier per cell, w*h: 0 plain, 1 dim, 2/3 visual
}

// Tuning, sized for the 60 fps render cadence and a cell's 2:1 aspect.
const (
	interferenceFrame = 1.0 / 60.0 // one Render call == one frame == this much simulated time

	// Pattern parameters, as fractions of the panel width, so the piece
	// scales with the region the layout allots.
	lamWide     = 0.16 // wavelength at a thin low end
	lamBass     = 0.10 // the contraction the bass applies to the wavelength
	sepStill    = 0.05 // source separation at no treble
	sepTreble   = 0.18 // the separation the treble adds
	sepSway     = 0.05 // the slow sway the sources drift on regardless
	sepSwayRate = 0.25 // the sway's angular rate, rad/s
	omegaRest   = 2.5  // ring emission at silence, rad/s
	omegaMid    = 7.0  // the emission the mid adds, rad/s
	gainRest    = 0.35 // field amplitude at silence
	gainLevel   = 0.65 // the amplitude the level adds

	// The dead zone keeps the field out of the air below it: a nodal line
	// is blank space, not a faint line, so the fan reads against the dark
	// background instead of tiling the panel like a wallpaper.
	deadZone = 0.18 // |field| below this is air
	falloff  = 1.4  // the |field| that fills a cell to full block
)

// intChars is the glyph per style tier, weight order: air, then the three
// block densities the field's |amplitude| maps onto.
var intChars = []rune{' ', '▒', '▓', '█'}

// newInterference builds an interference visualizer.
func newInterference() *interference { return &interference{} }

// Name implements visualizer.
func (v *interference) Name() string { return "interference" }

// bandRegions reports the four region averages of an analyser frame: the
// bass, mid and treble thirds, and the level over the whole frame. The
// bands arrive eased in [0,1] from the app, so no further shaping is done
// here — the mapping is the visualizer's own.
func bandRegions(bands []float64) (bass, mid, treble, level float64) {
	avg := func(lo, hi int) float64 {
		if hi > len(bands) {
			hi = len(bands)
		}
		if hi <= lo {
			return 0
		}
		s := 0.0
		for k := lo; k < hi; k++ {
			s += bands[k]
		}
		return s / float64(hi-lo)
	}
	bass = avg(0, 6)
	mid = avg(6, 11)
	treble = avg(11, contract.BandCount)
	level = avg(0, len(bands))
	return
}

// Render implements visualizer. It advances the phase clock (unless the
// frame is paused, which holds it), turns the band regions into the
// pattern's physical parameters, rasterizes the field over the right half
// of the panel and mirrors it, draws the source markers, and encodes the
// result in runs of one style each.
func (v *interference) Render(f frame) string {
	w, h := f.w, f.h
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if cap(v.grid) < w*h {
		v.grid = make([]rune, w*h)
		v.tier = make([]int, w*h)
	}
	v.grid = v.grid[:w*h]
	v.tier = v.tier[:w*h]
	clear(v.grid)
	clear(v.tier)

	if !f.paused {
		v.t += interferenceFrame
	}
	bass, mid, treble, level := bandRegions(f.bands)

	// The pattern's physical parameters, all panel-relative.
	lam := float64(w) * (lamWide - lamBass*bass)
	k := 2 * math.Pi / lam
	phi := (omegaRest + omegaMid*mid) * v.t
	gain := gainRest + gainLevel*level
	// The sources sit on the horizontal centre line, drifting apart with
	// the treble and swaying slowly on their own, so a resting field still
	// breathes.
	d := float64(w) * (sepStill + sepTreble*treble + sepSway*math.Sin(sepSwayRate*v.t))
	// The two sources are identical, so while they sit closer than a
	// wavelength their fields add coherently — at up to twice the
	// single-source amplitude, which would keep the resting field lit to
	// the panel's edges. Normalising by the coherence keeps the field's
	// total energy steady as the sources merge and split; well separated
	// (the loud fan) the factor is 1 and the sources decay independently.
	u := d / lam
	coherent := 1 + 0.8*math.Exp(-u*u)
	cx := 0.5 * float64(w)
	s1x, s2x := cx-d, cx+d
	// A terminal cell is two pixels tall, so the field lives in pixel
	// space: x + 1/2 across, (y + 1/2) * 2 down; the sources' line is at
	// pixel row h, the panel's vertical centre.
	sy := float64(h)

	half := w / 2 // the centre column (odd widths) or the first right column
	for y := 0; y < h; y++ {
		dy := (float64(y)+0.5)*2 - sy
		for x := half; x < w; x++ {
			px := float64(x) + 0.5
			// Both sources' circular waves, with the upstream piece's
			// near-field falloff expressed in wavelengths so it is
			// scale-invariant — 1/sqrt(1 + 2r/lambda): the upstream
			// 1/sqrt(1 + r/lambda) is too gentle for a dark panel, where
			// a field that never reaches the dead zone would tile the
			// whole region like a wallpaper; doubled, the rings die out
			// about a wavelength and a half from their source.
			r1 := math.Hypot(px-s1x, dy)
			r2 := math.Hypot(px-s2x, dy)
			a := math.Sin(k*r1-phi)/math.Sqrt(1+2*r1/lam) +
				math.Sin(k*r2-phi)/math.Sqrt(1+2*r2/lam)
			m := math.Abs(a) * gain / coherent
			t := 0
			if m >= deadZone {
				t = 1 + int((m-deadZone)/(falloff-deadZone)*3)
				if t > 3 {
					t = 3
				}
			}
			// The mirrored cell gets the identical field (the sources are
			// identical, so the field is symmetric about the centre
			// column): the pattern is exact, not float-approximate.
			i := y*w + x
			j := y*w + (w - 1 - x)
			v.grid[i], v.tier[i] = intChars[t], t
			v.grid[j], v.tier[j] = intChars[t], t
		}
	}
	// The emitting sources themselves: a bright core with a dim shoulder
	// on each side, so the eye can track the drift.
	mrow := h / 2
	set := func(c int, g rune, t int) {
		if c < 0 || c >= w || mrow < 0 || mrow >= h {
			return
		}
		i := mrow*w + c
		v.grid[i], v.tier[i] = g, t
	}
	for _, sx := range [2]float64{s1x, s2x} {
		c := int(sx)
		set(c-1, '(', 1)
		set(c, '●', 3)
		set(c+1, ')', 1)
	}

	// One style per run of equal tier, so a row is a handful of SGR
	// switches instead of one per cell (the particles' run encoding).
	plain := lipgloss.NewStyle()
	var out strings.Builder
	out.Grow(w * (h + 1))
	for y := 0; y < h; y++ {
		x := 0
		for x < w {
			t := v.tier[y*w+x]
			end := x + 1
			for end < w && v.tier[y*w+end] == t {
				end++
			}
			var style lipgloss.Style
			switch t {
			case 0:
				style = plain
			case 1:
				style = styleDim
			default:
				style = styleVisual
			}
			out.WriteString(style.Render(string(v.grid[y*w+x : y*w+end])))
			x = end
		}
		if y < h-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}
