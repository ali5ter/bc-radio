package tui

import (
	"math"
	"math/rand"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ali5ter/bc-radio/internal/contract"
)

// metaballs is the pattern from the issue's ascii-magic backlog
// (metaballs): blobs that drift, merge and split like hot liquid. The
// upstream piece is a pure function of the wall clock — six blobs on fixed
// drifts; here the blobs are the music. Each of the contract.BandCount
// analyser bands owns one blob:
//
//   - the band's energy is the blob's size — loud bands swell, quiet ones
//     hold small motes;
//   - the band's frequency is its home altitude — bass low, treble high —
//     so the field's shape is the spectrum's shape, the particles'
//     "the pattern the eye reads is the shape of the music" re-expressed
//     as liquid;
//   - at rest the field is one small mote per band — sixteen faint dots,
//     the same resting population as the particles.
//
// Like interference it holds a phase clock (one Render advances it one
// frame; a paused Render holds it, so the frame is byte-identical — the
// house pause cue). The per-band drift frequencies and phases are drawn
// once from a fixed-seed RNG, so renders are a deterministic function of
// the input frames, and the blobs' positions are closed-form in the clock
// (no simulated state to drift out of sync).
//
// The blobs are the classic metaball field: the sum, over every blob, of
// r^2 / (dx^2 + dy^2 + 0.5), in a space where a cell is one unit wide and
// two tall. The field reads in three scheme colours — the dim halo, the
// visual (teal) body, and the scheme's warm spectrumHigh end for the hot
// core, the same warm-on-dark logic as the accent but for the field
// instead of user action — so a full-scale passage is a mass of teal
// liquid with a copper heart, and nothing ever reads as a rainbow.
type metaballs struct {
	rng  *rand.Rand
	ball []blob
	t    float64 // phase clock, seconds: one Render call advances it one frame
	grid []rune  // the field's glyphs, w*h, reused between frames
	tier []int   // the style tier per cell, w*h: 0 plain, 1 dim, 2 visual, 3 hot
}

// blob is one band's blob: its fixed drift (frequency and phase, drawn
// once at construction) — the position and radius are computed per frame
// from the clock and the band's energy.
type blob struct {
	fx, fy float64 // drift frequencies, rad/s
	px, py float64 // drift phases, radians
	home   float64 // home altitude, in half-cell units (1 = one row)
}

// Tuning, as fractions of the panel where it matters, so the piece scales
// with the region the layout allots; sized for the 60 fps render cadence.
const (
	metaballFrame = 1.0 / 60.0 // one Render call == one frame == this much simulated time

	// Blob size: a fraction of the panel height, in half-cell units
	// (two per row). The energy's response is squared, so a half-scale
	// band is a modest blob and a full-scale one a big one — with a
	// linear response, a mid-level mix of sixteen bands merges into a
	// mass that floods the panel, and the music has no dynamic range
	// left.
	metaballRestR  = 0.05 // the radius at a silent band: one faint mote
	metaballSpanR  = 0.26 // the radius a full-scale band adds
	metaballPulse  = 0.12 // the slow radius pulse, a fraction of the radius
	metaballPulseR = 1.3  // the pulse's angular rate, rad/s

	// Drift: how far and how fast a blob roams around its home altitude.
	// The homes span the panel — bass low, treble high — so the field's
	// shape is the spectrum's shape: a bunched set of homes stacks the
	// blobs into one band and the halos flood the rest of the pane.
	metaballHomeLo   = 0.12 // the highest home (treble), a fraction of the height
	metaballHomeSpan = 0.76 // the home spread from treble to bass
	metaballDriftX   = 0.36 // the x drift's amplitude, a fraction of the width
	metaballDriftY   = 0.15 // the y drift's amplitude, a fraction of the height
	metaballFreqLo   = 0.15 // slowest drift, rad/s
	metaballFreqHi   = 0.50 // fastest drift, rad/s

	// Field tiers. The body and core are *sum* thresholds: where two
	// blobs overlap, the sum of their fields exceeds them and the
	// bodies merge — the liquid. The mid and halo are *per-blob*: a
	// tier's reach is sqrt(r^2 / t - 0.5), which scales with the
	// blob's own radius, so a dot stays a dot at any panel size — and
	// a distant blob's 1/d^2 tail can never sum into a wallpaper (the
	// upstream piece's faint background is deliberate there; in a dim
	// terminal it is static noise, so it is dropped).
	metaballCore = 1.50 // the hot core: the scheme's warm end (sum)
	metaballBody = 1.00 // the blob's body (sum: overlapping blobs merge)
	metaballMid  = 0.70 // the falloff ring, a fraction of each blob's field
	metaballHalo = 1.50 // the halo's margin beyond the body edge, half-cells
)

// newMetaballs builds a metaballs visualizer with its deterministic seed.
func newMetaballs() *metaballs {
	m := &metaballs{
		rng:  rand.New(rand.NewSource(1)),
		ball: make([]blob, contract.BandCount),
	}
	for i := range m.ball {
		// The band's altitude: the low bands live toward the floor, the
		// high bands toward the ceiling, spread across the panel.
		m.ball[i].home = metaballHomeLo + metaballHomeSpan*(contract.BandCount-1-float64(i))/float64(contract.BandCount-1)
		m.ball[i].fx = metaballFreqLo + (metaballFreqHi-metaballFreqLo)*m.rng.Float64()
		m.ball[i].fy = metaballFreqLo + (metaballFreqHi-metaballFreqLo)*m.rng.Float64()
		m.ball[i].px = 2 * math.Pi * m.rng.Float64()
		m.ball[i].py = 2 * math.Pi * m.rng.Float64()
	}
	return m
}

// Name implements visualizer.
func (m *metaballs) Name() string { return "metaballs" }

// Render implements visualizer. It advances the phase clock (unless the
// frame is paused, which holds it), turns each band's energy into its
// blob's position and radius, accumulates the metaball field, and encodes
// it in runs of one style each.
func (m *metaballs) Render(f frame) string {
	w, h := f.w, f.h
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if cap(m.grid) < w*h {
		m.grid = make([]rune, w*h)
		m.tier = make([]int, w*h)
	}
	m.grid = m.grid[:w*h]
	m.tier = m.tier[:w*h]
	clear(m.grid)
	clear(m.tier)

	if !f.paused {
		m.t += metaballFrame
	}
	bands := f.bands
	// One blob per band: the position is a closed-form drift around the
	// band's home altitude; the radius is the band's energy, squared.
	// Everything is in half-cell units (a cell is one wide, two tall),
	// so the panel is w by 2h.
	n := len(m.ball)
	if len(bands) < n {
		n = len(bands)
	}
	bx := make([]float64, n)
	by := make([]float64, n)
	br2 := make([]float64, n)
	tha := make([]float64, n) // each blob's halo threshold, below
	panelH := 2 * float64(h)
	for i := 0; i < n; i++ {
		b := m.ball[i]
		e := bands[i] * bands[i] // the squared response, above
		r := panelH * (metaballRestR + metaballSpanR*e)
		r *= 1 + metaballPulse*math.Sin(metaballPulseR*m.t+float64(i))
		bx[i] = float64(w)*0.5 + float64(w)*metaballDriftX*math.Sin(b.fx*m.t+b.px)
		by[i] = panelH * (b.home + metaballDriftY*math.Sin(b.fy*m.t+b.py+float64(i)*1.3))
		br2[i] = r * r
		// The halo is a fixed margin beyond the body edge: the field
		// falls as r^2 / d^2, so the threshold at d = r + margin is
		// this — a margin that is scale-free, the body edge included.
		tha[i] = br2[i] / ((r+metaballHalo)*(r+metaballHalo) + 0.5)
	}

	for y := 0; y < h; y++ {
		dyBase := (float64(y) + 0.5) * 2 // a cell is two half-cell units tall
		for x := 0; x < w; x++ {
			px := float64(x) + 0.5
			var field float64
			mid, halo := false, false
			for i := 0; i < n; i++ {
				dx := px - bx[i]
				dy := dyBase - by[i]
				c := br2[i] / (dx*dx + dy*dy + 0.5)
				field += c
				if c >= metaballMid {
					mid = true
				}
				if c >= tha[i] {
					halo = true
				}
			}
			// The tier encodes the style run; the glyph encodes the
			// weight within it.
			t, g := 0, rune(' ')
			switch {
			case field >= metaballCore:
				t, g = 3, '█'
			case field >= metaballBody:
				t, g = 2, '▓'
			case mid:
				t, g = 2, '▒'
			case halo:
				t, g = 1, '░'
			}
			i := y*w + x
			m.grid[i] = g
			m.tier[i] = t
		}
	}

	// One style per run of equal tier, so a row is a handful of SGR
	// switches instead of one per cell (the particles' run encoding).
	plain := lipgloss.NewStyle()
	var out strings.Builder
	out.Grow(w * (h + 1))
	for y := 0; y < h; y++ {
		x := 0
		for x < w {
			t := m.tier[y*w+x]
			end := x + 1
			for end < w && m.tier[y*w+end] == t {
				end++
			}
			var style lipgloss.Style
			switch t {
			case 0:
				style = plain
			case 1:
				style = styleDim
			case 2:
				style = styleVisual
			default:
				style = styleHot
			}
			out.WriteString(style.Render(string(m.grid[y*w+x : y*w+end])))
			x = end
		}
		if y < h-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}
