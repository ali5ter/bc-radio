package tui

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/ali5ter/bc-radio/internal/contract"
)

// reSGR matches SGR colour sequences so tests can compare glyphs, not style.
var reSGR = regexp.MustCompile("\x1b\\[[0-9;]*m")

// stripANSI removes SGR sequences for glyph-level assertions.
func stripANSI(s string) string { return reSGR.ReplaceAllString(s, "") }

// zeroBands returns an empty analyser frame.
func zeroBands() []float64 { return make([]float64, contract.BandCount) }

// frameBands builds a band frame from up to contract.BandCount values.
func frameBands(vals ...float64) []float64 {
	b := zeroBands()
	for i, v := range vals {
		if i < len(b) {
			b[i] = v
		}
	}
	return b
}

// bandFrame builds a render frame carrying only bands (the scope is empty).
func bandFrame(bands []float64, w, h int) frame {
	return frame{bands: bands, w: w, h: h}
}

func TestMakeVisualizers(t *testing.T) {
	vs := makeVisualizers()
	if len(vs) != 4 {
		t.Fatalf("makeVisualizers = %d; want 4 (spectrum, particles, scope, interference)", len(vs))
	}
	seen := map[string]bool{}
	for _, v := range vs {
		if v.Name() == "" || seen[v.Name()] {
			t.Fatalf("visualizer names must be non-empty and unique: %v", vs)
		}
		seen[v.Name()] = true
	}
}

// The layout does no further sizing: a frame must land exactly w × h for
// every region it could be allotted, including the degenerate small ones.
func TestRenderDimensions(t *testing.T) {
	sizes := [][2]int{{80, 24}, {40, 10}, {15, 8}, {8, 3}, {5, 1}}
	for _, v := range makeVisualizers() {
		for _, s := range sizes {
			w, h := lipgloss.Size(v.Render(bandFrame(zeroBands(), s[0], s[1])))
			if w != s[0] || h != s[1] {
				t.Errorf("%s at %dx%d: got %dx%d", v.Name(), s[0], s[1], w, h)
			}
		}
	}
}

func TestSpectrumBarMath(t *testing.T) {
	const h = 11 // 88 steps of 1/8-cell resolution
	b := zeroBands()
	b[3] = 0.5 // half scale: 44 steps = 5 full rows + a half row
	lines := strings.Split(stripANSI(newSpectrum().Render(bandFrame(b, 16, h))), "\n")
	if len(lines) != h {
		t.Fatalf("got %d rows; want %d", len(lines), h)
	}
	var col strings.Builder
	for i := h - 1; i >= 0; i-- { // bottom to top
		rs := []rune(lines[i])
		if len(rs) < 16 {
			t.Fatalf("row %d is %d cells; want 16: %q", i, len(rs), lines[i])
		}
		col.WriteRune(rs[3])
	}
	// Five full rows of bar, then the row its edge sits in at 1/8 precision
	// (▄, the half row), then clear air. At w = 16 every band gets one
	// column and no gaps fit, so band 3 is the fourth cell.
	want := strings.Repeat("█", 5) + "▄" + strings.Repeat(" ", 5)
	if got := col.String(); got != want {
		t.Fatalf("band 3 column (bottom-up) = %q; want %q", got, want)
	}
}

// The peak cap hovers at the band's highest recent level and falls peakFall
// per render, drawn at the same 1/8-cell precision as the bar: after a
// full-scale frame the cap descends out of the top row a fraction at a
// time, so the top cell steps ▇ ▆ ▅ … before the row clears.
func TestSpectrumPeakFall(t *testing.T) {
	v := newSpectrum()
	b := zeroBands()
	b[3] = 1.0
	top := func(render string) rune {
		lines := strings.Split(stripANSI(render), "\n")
		rs := []rune(lines[0])
		return rs[3]
	}
	v.Render(bandFrame(b, 16, 11)) // full scale: bar and cap both at the top
	// One fall (0.992) still sits in the top row, seven-eighths filled.
	if r := top(v.Render(bandFrame(zeroBands(), 16, 11))); r != '▇' {
		t.Fatalf("top cell after 1 zero frame = %q; want ▇ (the falling cap)", r)
	}
	// Two falls (0.984), six-eighths.
	if r := top(v.Render(bandFrame(zeroBands(), 16, 11))); r != '▆' {
		t.Fatalf("top cell after 2 zero frames = %q; want ▆ (the falling cap)", r)
	}
	// And it keeps falling: the top row clears once the cap has descended
	// below its floor, ~0.091/0.008 renders, well under the bound.
	for i := 0; i < 200; i++ {
		if top(v.Render(bandFrame(zeroBands(), 16, 11))) == ' ' {
			return
		}
	}
	t.Fatal("the peak cap never left the top row in 200 frames")
}

// The cliamp layout takes its priority order: the bands first, then the
// one-column gaps between them. At w = 80 the 16 bands and 15 gaps fill the
// row — the floor remainder goes to the first band (5 columns, the rest 4) —
// so band 5 of 16 occupies cells 26..29, with a gap on either side.
func TestSpectrumLaneMapping(t *testing.T) {
	b := zeroBands()
	b[5] = 1.0
	lines := strings.Split(stripANSI(newSpectrum().Render(bandFrame(b, 80, 10))), "\n")
	rs := []rune(lines[9])
	if len(rs) != 80 {
		t.Fatalf("bottom row is %d cells; want 80", len(rs))
	}
	for c, r := range rs {
		want := ' '
		if c >= 26 && c < 30 { // band 5 of 16: cells 26..29
			want = '█'
		}
		if r != want {
			t.Fatalf("bottom cell %d = %q; want %q", c, r, want)
		}
	}
}

func TestSpectrumZeroFrameBlank(t *testing.T) {
	got := stripANSI(newSpectrum().Render(bandFrame(zeroBands(), 80, 24)))
	for i, r := range got {
		if r != ' ' && r != '\n' {
			t.Fatalf("zero frame cell %d = %q; want a blank field", i, r)
		}
	}
}

// The scope plots the newest samples at the right edge of the Braille dot
// grid, two dots per cell. The three samples {1, −1, 0} land on dot
// columns 157, 158, 159 of the 160-wide grid: the +1 dot at the top of the
// grid, the −1 dot at the bottom, the 0 dot on the centre row, with the
// sub-30-dot jump between the last two interpolated into the trace.
func TestScopeTrace(t *testing.T) {
	const w, h = 80, 11            // 160 × 44 dots; the centre row is 5
	samples := []float64{1, -1, 0} // oldest to newest
	lines := strings.Split(stripANSI(newScope().Render(frame{scope: samples, w: w, h: h})), "\n")
	if len(lines) != h {
		t.Fatalf("got %d rows; want %d", len(lines), h)
	}
	check := func(row int, x int, want rune) {
		rs := []rune(lines[row])
		if len(rs) != w || rs[x] != want {
			t.Errorf("row %d cell %d = %q; want %q", row, x, rs[x], want)
		}
	}
	check(0, 77, ' ')       // the column before the trace is clear air
	check(0, 78, '\u2810')  // the +1 sample: top dot row, right-hand dot
	check(10, 78, ' ')      // the −1 sample lands in the neighbouring cell…
	check(10, 79, '\u280f') // …with the interpolated line back up the column
	check(5, 78, '─')       // a trace that left the baseline leaves its mark there
	check(5, 79, '\u282c')  // the 0 sample on the baseline, with the line coming in
	check(5, 0, '─')        // the unfilled left of the buffer is the baseline
	check(0, 79, ' ')       // and the top of the trace column is clear above the trace
}

// An empty scope is the quiet baseline: one dashed centre row, blank above
// and below.
func TestScopeEmpty(t *testing.T) {
	lines := strings.Split(stripANSI(newScope().Render(bandFrame(zeroBands(), 80, 11))), "\n")
	for y, l := range lines {
		for x, r := range []rune(l) {
			want := ' '
			if y == 5 {
				want = '─'
			}
			if r != want {
				t.Fatalf("empty scope row %d cell %d = %q; want %q", y, x, r, want)
			}
		}
	}
}

func TestParticlesPopulation(t *testing.T) {
	v := newParticles()
	b := frameBands(1.0, 0.8, 0.6, 0.4) // strong bass: target population 256
	var got string
	for i := 0; i < 120; i++ { // two seconds at 60 fps: the field equilibrates
		got = v.Render(bandFrame(b, 80, 24))
	}
	nonSpace := 0
	for _, r := range stripANSI(got) {
		if r != ' ' && r != '\n' {
			nonSpace++
		}
	}
	if nonSpace == 0 {
		t.Fatal("loud bass should keep sparks alive")
	}
	if nonSpace > particleMaxPop {
		t.Fatalf("live particles = %d; the cap is %d", nonSpace, particleMaxPop)
	}
}

// particlesSnapshot is the field's full state as a string, so the pause
// test can assert the simulation did (or did not) advance without
// depending on raster quantisation.
func particlesSnapshot(v *particles) string {
	var sb strings.Builder
	for _, l := range v.lane {
		for _, m := range l.motes {
			fmt.Fprintf(&sb, "%.17g %.17g %.17g %.17g %.17g|", m.x, m.y, m.vx, m.vy, m.age)
		}
		sb.WriteByte('/')
	}
	return sb.String()
}

// TestParticlesPauseFreeze pins the pause cue (2026-11-19): a loaded,
// paused track must freeze the field exactly like the spectrum and scope
// hold their pinned inputs — a paused Render re-rasterizes the motes
// where they are, without ageing, moving or respawning them, and the
// resume continues from where the field held.
func TestParticlesPauseFreeze(t *testing.T) {
	v := newParticles()
	b := frameBands(1.0, 0.8, 0.6, 0.4)
	// Let the field equilibrate in a loud passage.
	var playing string
	for i := 0; i < 120; i++ {
		playing = v.Render(bandFrame(b, 80, 24))
	}
	// Pause: the render is the frozen field, byte-identical to the last
	// playing one, and the simulation itself does not advance.
	snap := particlesSnapshot(v)
	f := bandFrame(b, 80, 24)
	f.paused = true
	frozen := v.Render(f)
	if frozen != playing {
		t.Fatal("a paused frame must re-rasterize the held field, byte for byte")
	}
	if got := particlesSnapshot(v); got != snap {
		t.Fatal("a paused frame must not advance the simulation")
	}
	// Repeated paused frames stay identical…
	if got := v.Render(f); got != frozen {
		t.Fatal("repeated paused frames must be identical")
	}
	// …and the resume moves the field again.
	v.Render(bandFrame(b, 80, 24))
	if got := particlesSnapshot(v); got == snap {
		t.Fatal("the resumed simulation must advance the field")
	}
}

func TestParticlesRestPopulation(t *testing.T) {
	v := newParticles()
	var got string
	for i := 0; i < 120; i++ {
		got = v.Render(bandFrame(zeroBands(), 80, 24))
	}
	nonSpace := 0
	for _, r := range stripANSI(got) {
		if r != ' ' && r != '\n' {
			nonSpace++
		}
	}
	if nonSpace < particleMinPop/2 {
		t.Fatalf("at rest the field should hold about %d sparks, saw %d", particleMinPop, nonSpace)
	}
}

// The pattern the redesign exists for: a loud band throws a dense plume in
// its own lane while the quiet lanes hold a sparse trickle, so the field
// reads as the spectrum's shape. A uniform random field would spread the
// same motes evenly and fail both bounds.
func TestParticlesLaneMapping(t *testing.T) {
	v := newParticles()
	b := zeroBands()
	b[3] = 1.0 // band 3 of 16 owns cells 15..19 of an 80-column panel
	var got string
	for i := 0; i < 120; i++ {
		got = v.Render(bandFrame(b, 80, 24))
	}
	lane, other := 0, 0
	for _, l := range strings.Split(stripANSI(got), "\n") {
		for x, r := range []rune(l) {
			if r == ' ' {
				continue
			}
			if x >= 15 && x < 20 {
				lane++
			} else {
				other++
			}
		}
	}
	if lane < 10 {
		t.Errorf("loud band's lane holds %d motes; want a dense plume (>= 10)", lane)
	}
	if other > 15 {
		t.Errorf("quiet lanes hold %d motes; want a sparse trickle (<= 15, one per lane)", other)
	}
}

// TestInterferencePauseFreeze pins the pause cue against the interference
// clock, like the particles' does against their field: a paused Render
// holds the phase clock, so the frame is byte-identical to the last
// playing one, and a resumed one moves again.
func TestInterferencePauseFreeze(t *testing.T) {
	v := newInterference()
	b := frameBands(1.0, 0.8, 0.6, 0.4, 0.2, 0.5, 0.3, 0.5, 0.3, 0.5, 0.4, 0.3, 0.4, 0.2, 0.4, 0.2)
	// Let the clock run through a loud passage.
	var playing string
	for i := 0; i < 120; i++ {
		playing = v.Render(bandFrame(b, 80, 24))
	}
	f := bandFrame(b, 80, 24)
	f.paused = true
	frozen := v.Render(f)
	if frozen != playing {
		t.Fatal("a paused frame must hold the clock, byte for byte")
	}
	if got := v.Render(f); got != frozen {
		t.Fatal("repeated paused frames must be identical")
	}
	if got := v.Render(bandFrame(b, 80, 24)); got == frozen {
		t.Fatal("the resumed clock must advance the pattern")
	}
}

// nonBlank counts the field's non-space cells (newlines excluded).
func nonBlank(render string) int {
	n := 0
	for _, r := range stripANSI(render) {
		if r != ' ' && r != '\n' {
			n++
		}
	}
	return n
}

// At rest the field is the quiet end of the mapping: a faint, near-
// concentric pulse — the sources sit close, the gain is at its floor. It
// must be neither a dead panel nor a full one.
func TestInterferenceRest(t *testing.T) {
	v := newInterference()
	var got string
	for i := 0; i < 120; i++ {
		got = v.Render(bandFrame(zeroBands(), 80, 24))
	}
	cells := 80 * 24
	n := nonBlank(got)
	if n == 0 {
		t.Fatal("at rest the field should hold its faint pulse")
	}
	if n > cells/4 {
		t.Fatalf("at rest the field fills %d/%d cells; want a sparse pulse (<= %d)", n, cells, cells/4)
	}
}

// A full-scale passage is the loud end: dense rings, full gain — the field
// should mostly fill the panel and reach its brightest glyph.
func TestInterferenceLoud(t *testing.T) {
	v := newInterference()
	b := zeroBands()
	for k := range b {
		b[k] = 1.0
	}
	var got string
	for i := 0; i < 120; i++ {
		got = v.Render(bandFrame(b, 80, 24))
	}
	cells := 80 * 24
	if n := nonBlank(got); n < cells/2 {
		t.Fatalf("a full-scale passage should fill most of the panel, got %d/%d", n, cells)
	}
	if !strings.Contains(got, "█") {
		t.Fatal("a full-scale passage should reach the full-block glyph")
	}
}

// The point of the audio wiring: each band region must change what a frame
// draws, with the phase clock frozen (a paused Render is a pure function of
// the bands alone, at a fixed t).
func TestInterferenceAudioMapping(t *testing.T) {
	v := newInterference()
	v.Render(bandFrame(zeroBands(), 80, 24)) // give the clock a nonzero value
	base := zeroBands()
	for k := 6; k < 11; k++ {
		base[k] = 0.5 // a fixed mid, so only the region under test moves
	}
	frozen := func(mutate func(b []float64)) string {
		b := append([]float64(nil), base...)
		mutate(b)
		f := bandFrame(b, 80, 24)
		f.paused = true
		return v.Render(f)
	}
	set := func(lo, hi int, val float64) func(b []float64) {
		return func(b []float64) {
			for k := lo; k < hi; k++ {
				b[k] = val
			}
		}
	}
	reference := frozen(func(b []float64) {})
	for name, mutate := range map[string]func(b []float64){
		"bass (wavelength)":   set(0, 6, 1.0),
		"mid (emission rate)": set(6, 11, 1.0),
		"treble (separation)": set(11, 16, 1.0),
		"level (amplitude)":   set(0, 16, 1.0),
	} {
		if got := frozen(mutate); got == reference {
			t.Errorf("%s at full scale drew the reference frame; the region does not reach the pattern", name)
		}
	}
}

// Renders must be a pure function of the input frames: two fresh instances
// fed the same sequence draw identical frames, for every visualizer.
func TestVisualizersDeterministic(t *testing.T) {
	vs1, vs2 := makeVisualizers(), makeVisualizers()
	run := func(vs []visualizer, w, h int) string {
		var out strings.Builder
		for i := 0; i < 60; i++ {
			b := zeroBands()
			for k := range b {
				b[k] = 0.5 + 0.5*math.Sin(float64(i)/10.0+float64(k))
			}
			for _, v := range vs {
				out.WriteString(v.Render(bandFrame(b, w, h)))
			}
		}
		return out.String()
	}
	for i, name := range []string{"spectrum", "particles", "scope", "interference"} {
		if vs1[i].Name() != name {
			t.Fatalf("makeVisualizers()[%d] = %s; the test expects %s", i, vs1[i].Name(), name)
		}
	}
	a, b := run(vs1, 80, 24), run(vs2, 80, 24)
	if a != b {
		t.Fatal("two fresh instances produced different frames for the same input")
	}
}
