package tui

import (
	"math"
	"strings"

	"charm.land/lipgloss/v2"
)

// kaleidoscope is the pattern from the issue's ascii-magic backlog
// (kaleidoscope): an eightfold mirrored field of fractal noise that turns and
// churns like a kaleidoscope. The upstream piece is a pure function of the
// wall clock on a fixed 80×32 grid, coloured by a 40-hue wheel; here the clocks
// are three integrated phases — the rotation, the churn, and the ripple — that
// one Render advances a frame and a paused Render holds (the house pause cue),
// and the pattern's parameters and colour are driven by the analyser instead of
// the wheel, so the field is the music:
//
//   - the bass spins the kaleidoscope and coarsens its shapes — heavy lows
//     turn it fast and read big, a thin low end slow and fine;
//   - the mid churns the liquid and cuts its detail — the noise field drifts
//     and the pattern's fine structure follows the body of the music;
//   - the treble tightens the radial rings — sparkle packs more rings into
//     the radius;
//   - the level is the field's density — a quiet passage is a sparse dusting
//     of dots, a loud one fills the panel to the heavy blocks.
//
// The colour is the scheme's four-colour heat ramp (dim, cool, visual, hot),
// coarse-stepped from the noise field and a slow time drift, so the
// kaleidoscope "shifts colour as it turns" inside the house palette — the
// upstream's 40-hue wheel would flicker into noise at 60 fps (styles.go).
//
// The field is a pure function of the render frame and the three integrated
// phase clocks (the rotation, the churn, and the ripple), with no per-cell
// randomness — the noise is a fixed function of the coordinates — so a paused
// Render is byte-identical to the last playing one and renders are
// deterministic in the input. The eightfold mirror is exact: the angle is
// folded to one sector and reflected, so the eight sectors are bit-identical,
// not float-approximate.
type kaleidoscope struct {
	rotT    float64 // integrated rotation angle, radians (the bass)
	churnT  float64 // integrated churn phase, the noise field's drift (the mid)
	rippleT float64 // integrated ripple phase, the radial rings' travel (the level)
	grid    []rune  // the field's glyphs, w*h, reused between frames
	tier    []int   // the style tier per cell, w*h: 0 air, 1 dim, 2 cool, 3 visual, 4 hot
}

// Tuning, sized for the 60 fps render cadence; the spatial terms are
// fractions of the panel radius so the piece scales with the region the
// layout allots, and the rates are in the music's band regions.
const (
	kaleidoFrame = 1.0 / 60.0 // one Render call == one frame == this much simulated time

	// The eightfold mirror: the angle is wrapped to one sector (τ/8) and
	// reflected to [0, τ/16], so the eight sectors are identical.
	kaleidoSegments = 8

	// Rotation, driven by the bass: a base turn at silence plus the bass's
	// added spin, integrated into the rotation clock.
	kaleidoRotRest = 0.10 // rad/s at silence: a slow turn
	kaleidoRotBass = 0.55 // the rad/s the bass adds

	// Churn, driven by the mid: a base drift plus the mid's added churn,
	// integrated into the churn clock that scrolls and breathes the noise
	// field — a quiet mid lets the liquid settle, a loud one churns it.
	kaleidoChurnRest = 0.30 // base churn rate
	kaleidoChurnMid  = 0.90 // the churn the mid adds

	// The noise field's sample range over the normalised radius, driven by
	// the bass: a wider range samples the finer end of the noise (fine
	// shapes), so the bass pulls it coarse — heavy lows read big and slow,
	// a thin low end fine and quick. ≈ the upstream's r·0.12 at no bass.
	kaleidoScaleRest = 3.0 // the sample range at no bass
	kaleidoScaleBass = 1.5 // the coarsening the bass applies to the range

	// The churn's drift, in the noise field's own units (scaled with the
	// sample range so it is scale-free): a slow breath across the field and
	// a steady scroll, the upstream's sin(t·0.3)·2 and t·0.25.
	kaleidoDriftAmp    = 2.0  // the breath's amplitude, in sample-range units
	kaleidoDriftScroll = 0.25 // the scroll's rate, in sample-range units

	// The radial ripple's spacing, driven by the treble, in ring cycles across
	// the panel radius so it is scale-free. ≈ the upstream's r·0.4 at no
	// treble.
	kaleidoRippleRest   = 1.6 // rings at no treble
	kaleidoRippleTreble = 3.0 // the rings the treble adds

	// The ripple's temporal rate, driven by the level: loud passages ripple
	// faster. rad/s of the ripple phase.
	kaleidoRippleRestRate = 0.0 // the ripple holds at silence
	kaleidoRippleRate     = 1.5 // the rad/s the level adds

	// The fine-detail multiplier on the noise in the value term, driven by the
	// mid: how finely the pattern's texture is cut. ≈ the upstream's n·14 at
	// no mid.
	kaleidoDetailRest = 14.0 // the base detail
	kaleidoDetailMid  = 10.0 // the detail the mid adds

	// Density: the level's gain over the pattern's intrinsic value, with the
	// dead zone and falloff that step it onto the glyph ramp. Most cells sit
	// under the dead zone in a quiet passage (a sparse dusting); a loud one
	// saturates to the heavy blocks.
	kaleidoGainRest  = 0.45 // the density gain at silence
	kaleidoGainLevel = 1.05 // the gain the level adds
	kaleidoDeadZone  = 0.40 // |field| below this is air
	kaleidoFalloff   = 1.30 // the |field| that fills a cell to the full block
)

// kaleidoChars is the glyph per density tier, in weight order: air, then a
// smooth ramp from the faintest dot to the full block — the same set the
// polished interference runs, so the quiet between the lit regions reads as a
// dusting of dots that fades into the dark.
var kaleidoChars = []rune{' ', '.', '·', '░', '▒', '▓', '█'}

// newKaleidoscope builds a kaleidoscope visualizer.
func newKaleidoscope() *kaleidoscope { return &kaleidoscope{} }

// Name implements visualizer.
func (k *kaleidoscope) Name() string { return "kaleidoscope" }

// Render implements visualizer. It advances the phase clocks (unless the frame
// is paused, which holds them — the house pause cue), turns the band regions
// into the pattern's parameters, folds each cell into the eightfold mirror,
// samples the noise field, and encodes the result in runs of one style each.
func (k *kaleidoscope) Render(f frame) string {
	w, h := f.w, f.h
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	if cap(k.grid) < w*h {
		k.grid = make([]rune, w*h)
		k.tier = make([]int, w*h)
	}
	k.grid = k.grid[:w*h]
	k.tier = k.tier[:w*h]
	clear(k.grid)
	clear(k.tier)

	bass, mid, treble, level := bandRegions(f.bands)

	// The pattern's rates, each from its own band region: the bass spins the
	// piece, the mid churns the liquid, the level ripples the rings and
	// gains its density.
	rotRate := kaleidoRotRest + kaleidoRotBass*bass
	churnRate := kaleidoChurnRest + kaleidoChurnMid*mid
	rippleRate := kaleidoRippleRestRate + kaleidoRippleRate*level
	gain := kaleidoGainRest + kaleidoGainLevel*level

	// Advance the three phase clocks (a paused Render holds them — the house
	// pause cue). Each is an integral of its rate, so a band that changes
	// tempo turns the piece smoothly, with no phase discontinuity.
	if !f.paused {
		k.rotT += rotRate * kaleidoFrame
		k.churnT += churnRate * kaleidoFrame
		k.rippleT += rippleRate * kaleidoFrame
	}

	// The pattern's spatial parameters, all panel-relative.
	cx, cy := 0.5*float64(w), 0.5*float64(h)
	// The far corner's aspect-corrected radius: the normaliser that makes the
	// spatial terms scale-free. A terminal cell is two pixels tall, so x is
	// halved to keep the field's circles round.
	rMax := math.Hypot(0.5*cx, 0.5*cy)
	// The noise field's sample range, coarsened by the bass.
	scale := kaleidoScaleRest - kaleidoScaleBass*bass
	// The ripple's spacing (the treble), in ring cycles across the
	// normalised radius, and the pattern's detail (the mid).
	rippleK := 2 * math.Pi * (kaleidoRippleRest + kaleidoRippleTreble*treble)
	detail := kaleidoDetailRest + kaleidoDetailMid*mid
	// The churn's drift, scaled with the sample range so it is scale-free.
	driftAmp := kaleidoDriftAmp * scale / kaleidoScaleRest
	driftScroll := kaleidoDriftScroll * scale / kaleidoScaleRest

	sector := (2 * math.Pi) / kaleidoSegments
	half := 0.5 * sector
	nChars := len(kaleidoChars)
	for y := 0; y < h; y++ {
		dy := float64(y) + 0.5 - cy
		for x := 0; x < w; x++ {
			dx := (float64(x) + 0.5 - cx) / 2 // the 2:1 cell aspect
			r := math.Hypot(dx, dy)
			rn := r / rMax
			// The eightfold mirror: rotate, fold to one sector, reflect.
			a := math.Atan2(dy, dx) + k.rotT
			a = math.Mod(a, sector)
			if a < 0 {
				a += sector
			}
			if a > half {
				a = sector - a
			}
			// The folded (u, v) sample of the noise field, scrolled and
			// breathing on the churn phase.
			u := math.Cos(a) * rn * scale
			v := math.Sin(a) * rn * scale
			n := kaleidoFbm(u+math.Sin(k.churnT)*driftAmp, v-k.churnT*driftScroll, 3)
			// The pattern's intrinsic value: the noise's detail and the
			// radial ripple, in [0,1]. The ripple's travel is its own
			// integrated phase, so a changing level changes its tempo
			// smoothly rather than jumping the rings.
			s := math.Sin(rn*rippleK + k.rippleT + n*6)
			val := 0.5 + 0.5*math.Sin(n*detail+s)
			// The density, level-gated, over the dead zone to the falloff.
			m := val * gain
			q := (m - kaleidoDeadZone) / (kaleidoFalloff - kaleidoDeadZone)
			i := y*w + x
			if q <= 0 {
				k.grid[i] = ' ' // air: a nodal region, not a faint line
				k.tier[i] = 0
				continue
			}
			if q > 1 {
				q = 1
			}
			// A lit cell is at least a dot: the ramp's opening space is air
			// only, so the field's faint end reads as a dusting, not blank.
			k.grid[i] = kaleidoChars[1+min(nChars-2, int(q*float64(nChars-1)))]
			// The colour heat is the field's own density: a lit cell's
			// temperature is its brightness — the faint end reads dim and
			// cool, the dense body the visual colour, and only the full
			// blocks reach the scheme's warm core (the metaballs'
			// heat-is-density, re-expressed). The density is a smooth,
			// churning function of the field, so the regions are broad, the
			// runs stay long, and they shift as the piece turns and churns.
			k.tier[i] = 1 + min(3, int(q*4))
		}
	}

	// One style per run of equal tier, so a row is a handful of SGR switches
	// instead of one per cell (the particles' run encoding).
	plain := lipgloss.NewStyle()
	var out strings.Builder
	out.Grow(w * (h + 1))
	for y := 0; y < h; y++ {
		x := 0
		for x < w {
			t := k.tier[y*w+x]
			end := x + 1
			for end < w && k.tier[y*w+end] == t {
				end++
			}
			var style lipgloss.Style
			// The heat ramp: the field's faint body is dim or the cool end,
			// its mid the visual colour, and only its hottest regions reach
			// the scheme's warm end.
			switch t {
			case 0:
				style = plain
			case 1:
				style = styleDim
			case 2:
				style = styleCool
			case 3:
				style = styleVisual
			default:
				style = styleHot
			}
			out.WriteString(style.Render(string(k.grid[y*w+x : y*w+end])))
			x = end
		}
		if y < h-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// kaleidoHash reproduces kit.js's hash2: the same 32-bit integer mix, so the
// noise field is the upstream piece's field, not a Go re-invention. JS's
// | 0, Math.imul and >>> are 32-bit; Go's int32 wraps mod 2^32 the same way,
// and uint32 gives the unsigned shifts.
func kaleidoHash(x, y, s int32) float64 {
	h := x*374761393 + y*668265263 + s*2147483647 // | 0
	hu := uint32(h)
	h = int32(int64(int32(hu^hu>>13)) * int64(1274126177)) // Math.imul
	hu = uint32(h)
	return float64(hu^hu>>16) / 4294967296
}

// kaleidoNoise is 2D value noise in 0..1, kit.js's noise2: the four corner
// hashes bilinearly blended on a smoothstep.
func kaleidoNoise(x, y float64, seed int32) float64 {
	xi := math.Floor(x)
	yi := math.Floor(y)
	xf := kaleidoSmooth(x - xi)
	yf := kaleidoSmooth(y - yi)
	a := kaleidoHash(int32(xi), int32(yi), seed)
	b := kaleidoHash(int32(xi+1), int32(yi), seed)
	c := kaleidoHash(int32(xi), int32(yi+1), seed)
	d := kaleidoHash(int32(xi+1), int32(yi+1), seed)
	return kaleidoLerp(kaleidoLerp(a, b, xf), kaleidoLerp(c, d, xf), yf)
}

// kaleidoFbm is fractal noise, a few octaves of kaleidoNoise in 0..1,
// kit.js's fbm (the seed the kaleidoscope uses is the default 0).
func kaleidoFbm(x, y float64, octaves int) float64 {
	var v, amp, norm float64
	f := 1.0
	amp = 0.5
	for i := 0; i < octaves; i++ {
		v += amp * kaleidoNoise(x*f, y*f, int32(i*17))
		norm += amp
		amp *= 0.5
		f *= 2
	}
	return v / norm
}

// kaleidoSmooth is the smoothstep, kit.js's smooth.
func kaleidoSmooth(t float64) float64 { return t * t * (3 - 2*t) }

// kaleidoLerp is a linear blend, kit.js's lerp.
func kaleidoLerp(a, b, t float64) float64 { return a + (b-a)*t }
