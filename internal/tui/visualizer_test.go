package tui

import (
	"math"
	"regexp"
	"strings"
	"testing"

	"bc-radio/internal/contract"
	"charm.land/lipgloss/v2"
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

func TestMakeVisualizers(t *testing.T) {
	vs := makeVisualizers()
	if len(vs) != 3 {
		t.Fatalf("makeVisualizers = %d; the PRD requires at least three", len(vs))
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
			w, h := lipgloss.Size(v.Render(zeroBands(), s[0], s[1]))
			if w != s[0] || h != s[1] {
				t.Errorf("%s at %dx%d: got %dx%d", v.Name(), s[0], s[1], w, h)
			}
		}
	}
}

func TestSpectrumBarMath(t *testing.T) {
	const h = 11 // 22 half-cells
	b := zeroBands()
	b[3] = 0.5 // half scale: 11 half-cells = 5 full rows + 1 half row
	lines := strings.Split(stripANSI(newSpectrum().Render(b, 16, h)), "\n")
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
	want := strings.Repeat("█", 5) + "▄" + strings.Repeat(" ", 5)
	if got := col.String(); got != want {
		t.Fatalf("band 3 column (bottom-up) = %q; want %q", got, want)
	}
}

// At w ≥ 16 the bars tile the width in equal cells and the floor remainder
// is absorbed into the last bar: band k occupies cells k·cellW .. (k+1)·cellW.
func TestSpectrumLaneMapping(t *testing.T) {
	b := zeroBands()
	b[5] = 1.0
	lines := strings.Split(stripANSI(newSpectrum().Render(b, 80, 10)), "\n")
	rs := []rune(lines[9])
	if len(rs) != 80 {
		t.Fatalf("bottom row is %d cells; want 80", len(rs))
	}
	for c, r := range rs {
		want := ' '
		if c >= 25 && c < 30 { // band 5 of 16: cells 25..29
			want = '█'
		}
		if r != want {
			t.Fatalf("bottom cell %d = %q; want %q", c, r, want)
		}
	}
}

func TestSpectrumZeroFrameBlank(t *testing.T) {
	got := stripANSI(newSpectrum().Render(zeroBands(), 80, 24))
	for i, r := range got {
		if r != ' ' && r != '\n' {
			t.Fatalf("zero frame cell %d = %q; want a blank field", i, r)
		}
	}
}

func TestWaveformSilence(t *testing.T) {
	lines := strings.Split(stripANSI(newWaveform().Render(zeroBands(), 80, 9)), "\n")
	if len(lines) != 9 {
		t.Fatalf("got %d rows; want 9", len(lines))
	}
	for y, l := range lines {
		rs := []rune(l)
		if len(rs) != 80 {
			t.Fatalf("row %d is %d cells; want 80", y, len(rs))
		}
		for x, r := range rs {
			want := ' '
			if y == 4 { // the centre row
				want = '─'
			}
			if r != want {
				t.Fatalf("cell %d,%d = %q; want %q (silence shows the baseline only)", x, y, r, want)
			}
		}
	}
}

func TestWaveformTrace(t *testing.T) {
	b := zeroBands()
	for i := range b {
		b[i] = 0.5
	}
	got := stripANSI(newWaveform().Render(b, 80, 11))
	if !strings.Contains(got, "•") {
		t.Fatal("a full-scale frame should draw a trace of dots")
	}
	if !strings.Contains(got, "·") {
		t.Fatal("a full-scale frame should keep the quiet baseline")
	}
}

func TestParticlesPopulation(t *testing.T) {
	v := newParticles()
	b := frameBands(1.0, 0.8, 0.6, 0.4) // strong bass: target population 256
	var got string
	for i := 0; i < 120; i++ { // two seconds at 60 fps: the field equilibrates
		got = v.Render(b, 80, 24)
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

func TestParticlesRestPopulation(t *testing.T) {
	v := newParticles()
	var got string
	for i := 0; i < 120; i++ {
		got = v.Render(zeroBands(), 80, 24)
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

// Renders must be a pure function of the input frames: two fresh instances
// fed the same sequence draw identical frames.
func TestVisualizersDeterministic(t *testing.T) {
	vs1, vs2 := makeVisualizers(), makeVisualizers()
	run := func(vs []visualizer, w, h int) string {
		var out strings.Builder
		for i := 0; i < 60; i++ {
			b := zeroBands()
			for k := range b {
				b[k] = 0.5 + 0.5*math.Sin(float64(i)/10.0+float64(k))
			}
			out.WriteString(vs[0].Render(b, w, h))
		}
		return out.String()
	}
	for i, name := range []string{"spectrum", "waveform", "particles"} {
		if vs1[i].Name() != name {
			t.Fatalf("makeVisualizers()[%d] = %s; the test expects %s", i, vs1[i].Name(), name)
		}
	}
	a, b := run(vs1, 80, 24), run(vs2, 80, 24)
	if a != b {
		t.Fatal("two fresh instances produced different frames for the same input")
	}
}
