package audio

import (
	"math"
	"math/rand"
	"sync"
	"testing"

	"bc-radio/internal/contract"
)

// TestBandEdgeLayout pins the log-spaced layout: contract.BandCount+1
// strictly increasing edges from analyserFMin to analyserFMax.
func TestBandEdgeLayout(t *testing.T) {
	t.Parallel()
	prev := 0.0
	for k := 0; k <= contract.BandCount; k++ {
		f := bandEdge(k)
		if f <= prev {
			t.Errorf("edge %d = %v Hz is not above the previous %v Hz", k, f, prev)
		}
		prev = f
	}
	if math.Abs(bandEdge(0)-analyserFMin) > 1e-9 {
		t.Errorf("edge 0 = %v Hz, want %v", bandEdge(0), analyserFMin)
	}
	if math.Abs(bandEdge(contract.BandCount)-analyserFMax)/analyserFMax > 1e-9 {
		t.Errorf("top edge = %v Hz, want %v", bandEdge(contract.BandCount), analyserFMax)
	}
}

// TestBandsZeroBeforeFeed pins the all-zero initial frame.
func TestBandsZeroBeforeFeed(t *testing.T) {
	t.Parallel()
	a := newAnalyser(44100)
	for k, b := range a.Bands() {
		if b != 0 {
			t.Errorf("band %d = %v before any Feed, want 0", k, b)
		}
	}
}

// sineFrames builds amp-valued stereo sine frames at f, sampled at rate.
func sineFrames(f, amp float64, rate, n int) [][2]float64 {
	frames := make([][2]float64, n)
	for i := range frames {
		v := amp * math.Sin(2*math.Pi*f*float64(i)/float64(rate))
		frames[i][0], frames[i][1] = v, v
	}
	return frames
}

// argmax returns the index of the largest band.
func argmax(bands []float64) int {
	best := 0
	for j, b := range bands {
		if b > bands[best] {
			best = j
		}
	}
	return best
}

// bandCentre returns the geometric mid-point of band k's edges — the
// frequency a test sine uses to hit the band dead centre.
func bandCentre(k int) float64 {
	return math.Sqrt(bandEdge(k) * bandEdge(k+1))
}

// TestFeedSineAtBandCentre feeds a full-scale sine exactly at a band's
// centre: that band must read near 1 (a strong bin against the log
// scale) and the others stay well below it.
func TestFeedSineAtBandCentre(t *testing.T) {
	t.Parallel()
	const rate = 44100
	const k = 8
	a := newAnalyser(rate)
	a.Feed(sineFrames(bandCentre(k), 1, rate, 4*analyserWindow))
	bands := a.Bands()
	if bands[k] < 0.9 {
		t.Errorf("band %d = %v for a full-scale sine at its own centre, want >= 0.9", k, bands[k])
	}
	for j, b := range bands {
		if j != k && b > 0.5 {
			t.Errorf("band %d = %v, want < 0.5 (leakage must not rival the peak %v)", j, b, bands[k])
		}
	}
}

// TestFeedLogScale is the property the display depends on: the band level
// is a dB mapping of the power, so a level 10 dB down in power reads
// exactly logRange/10 lower. A full-scale sine clamps near 1, so the
// 10 dB-down version (amplitude × 10^(−10/20) ≈ 0.316) must land
// measurably below it — the quiet passages a music player must still show.
func TestFeedLogScale(t *testing.T) {
	t.Parallel()
	const rate = 44100
	const k = 8
	f := bandCentre(k)
	full := newAnalyser(rate)
	full.Feed(sineFrames(f, 1, rate, 4*analyserWindow))
	quiet := newAnalyser(rate)
	quiet.Feed(sineFrames(f, 1/math.Sqrt(10), rate, 4*analyserWindow))
	if full.Bands()[k] < 0.95 {
		t.Fatalf("full-scale band %d = %v; the calibration test needs it near 1", k, full.Bands()[k])
	}
	if got := quiet.Bands()[k]; got < full.Bands()[k]-0.35 {
		t.Errorf("band %d at 10 dB down = %v, want at least ~0.2 below the full-scale %v", k, got, full.Bands()[k])
	}
}

// TestFeedSineOffCentre feeds a 1 kHz sine (not a band centre): the
// closest band must be the peak, clearly above the runner-up. The level
// itself is whatever the log scale of an off-centre bin's power gives;
// the invariant is the selectivity, not the absolute value.
func TestFeedSineOffCentre(t *testing.T) {
	t.Parallel()
	const rate = 44100
	const f = 1000.0
	a := newAnalyser(rate)
	a.Feed(sineFrames(f, 1, rate, 4*analyserWindow))
	bands := a.Bands()
	peak := argmax(bands)
	want := 0
	for j := 1; j < contract.BandCount; j++ {
		if math.Abs(bandCentre(j)-f) < math.Abs(bandCentre(want)-f) {
			want = j
		}
	}
	if peak != want {
		t.Errorf("peak band = %d, want %d (nearest the %v Hz sine)", peak, want, f)
	}
	runner := float64(0)
	for j, b := range bands {
		if j != peak && b > runner {
			runner = b
		}
	}
	if bands[peak] < runner+0.15 {
		t.Errorf("peak %v is not clearly above the runner-up %v on the log scale", bands[peak], runner)
	}
}

// TestFeedClampsToUnit: content louder than full scale must clamp to 1.
func TestFeedClampsToUnit(t *testing.T) {
	t.Parallel()
	const rate = 44100
	const k = 8
	a := newAnalyser(rate)
	a.Feed(sineFrames(bandCentre(k), 1.5, rate, 2*analyserWindow))
	bands := a.Bands()
	if bands[k] != 1 {
		t.Errorf("band %d = %v for a 1.5x sine, want clamped to 1", k, bands[k])
	}
	for j, b := range bands {
		if b < 0 || b > 1 {
			t.Errorf("band %d = %v, want within [0,1]", j, b)
		}
	}
}

// TestFeedSilenceDecays pins the silence gate: after loud content, a
// silent window must not compute a frame of the noise floor — it decays
// the last frame toward rest, and sustained silence ends at zero. The
// decay is per hop of audible time advanced (here 12 hops, 0.8^12 ≈
// 0.07), so the settle rate tracks the music rather than the poll rate.
func TestFeedSilenceDecays(t *testing.T) {
	t.Parallel()
	const rate = 44100
	const k = 8
	a := newAnalyser(rate)
	a.Feed(sineFrames(bandCentre(k), 1, rate, 4*analyserWindow))
	loud := a.Bands()[k]
	if loud < 0.9 {
		t.Fatalf("setup: band %d = %v, want a loud frame to decay from", k, loud)
	}
	// Twelve hops of silence: one publish, decayed 0.8 per hop.
	a.Feed(sineFrames(440, 0, rate, 12*analyserWindow/2+analyserWindow))
	if got := a.Bands()[k]; got > 0.2 {
		t.Errorf("band %d after twelve silent hops = %v, want decayed below 0.2", k, got)
	}
	// And nothing to decay: a fresh analyser given only silence stays 0.
	fresh := newAnalyser(rate)
	fresh.Feed(sineFrames(440, 0, rate, 2*analyserWindow))
	for k, b := range fresh.Bands() {
		if b != 0 {
			t.Errorf("band %d = %v after silence-only Feed, want 0", k, b)
		}
	}
}

// TestFeedWhiteNoiseFlat is the flat-spectrum invariant: white noise has
// equal power in every band, so on the log scale all bands must read
// similarly — the spectrum of a hiss should be a flat line, not a hill.
func TestFeedWhiteNoiseFlat(t *testing.T) {
	t.Parallel()
	const rate = 44100
	a := newAnalyser(rate)
	rng := rand.New(rand.NewSource(1))
	frames := make([][2]float64, 4*analyserWindow)
	for i := range frames {
		v := rng.Float64()*2 - 1 // uniform [-1,1], flat spectrum
		frames[i][0], frames[i][1] = v, v
	}
	a.Feed(frames)
	bands := a.Bands()
	lo, hi := math.Inf(1), math.Inf(-1)
	for k, b := range bands {
		if b < 0.1 || b > 1 {
			t.Errorf("band %d = %v for white noise, want within [0.1, 1]", k, b)
		}
		lo, hi = math.Min(lo, b), math.Max(hi, b)
	}
	if hi-lo > 0.25 {
		t.Errorf("white-noise bands spread %v (%v..%v), want < 0.25 on the log scale", hi-lo, lo, hi)
	}
}

// TestFeedWrapsRing pins the ring's bookkeeping: written counts every
// sample appended exactly, tail wraps at the capacity, and the ring never
// grows — a long track must neither allocate nor accumulate.
func TestFeedWrapsRing(t *testing.T) {
	t.Parallel()
	const rate = 44100
	a := newAnalyser(rate)
	// Well past the capacity so the ring wraps several times; the chunk
	// deliberately does not divide the capacity.
	total := 3 * a.size
	chunk := a.size/3 + 1
	frames := sineFrames(440, 0.5, rate, total)
	for i := 0; i < total; i += chunk {
		end := min(i+chunk, total)
		a.Feed(frames[i:end])
	}
	if a.written != total {
		t.Fatalf("written = %d after feeding %d, want exact", a.written, total)
	}
	if a.tail != total%a.size {
		t.Fatalf("tail = %d, want %d (wrapped at the capacity)", a.tail, total%a.size)
	}
	if len(a.ring) != a.size {
		t.Fatalf("ring = %d samples, want the fixed capacity %d", len(a.ring), a.size)
	}
}

// TestFeedArbitraryChunkSizes: the analyser must publish correctly no
// matter how the pump splits the audio — including one sample at a time.
func TestFeedArbitraryChunkSizes(t *testing.T) {
	t.Parallel()
	const rate = 44100
	const k = 8
	frames := sineFrames(bandCentre(k), 1, rate, 2*analyserWindow)
	a := newAnalyser(rate)
	for i := range frames {
		a.Feed(frames[i : i+1])
	}
	bands := a.Bands()
	if bands[k] < 0.9 {
		t.Errorf("band %d = %v after frame-by-frame Feed, want >= 0.9", k, bands[k])
	}
}

// TestBandsAnchorOnUnplayed pins the redesign's core: the frame reflects
// what is audible now, not what was fed last. With a full buffer of
// silence queued behind a loud passage, the frame must still read the loud
// audio while it is ahead of the audible point — and decay only once the
// loud audio has actually passed it.
func TestBandsAnchorOnUnplayed(t *testing.T) {
	t.Parallel()
	const rate = 44100
	const k = 8
	a := newAnalyser(rate)
	a.Feed(sineFrames(bandCentre(k), 1, rate, 2*analyserWindow))
	// The device still holds the last window unplayed: the audible point
	// sits inside the loud audio, so the frame must be loud.
	a.unplayed = func() int { return analyserWindow }
	if got := a.Bands()[k]; got < 0.9 {
		t.Fatalf("band %d = %v with the loud window still ahead of the audible point, want loud", k, got)
	}
	// The buffer empties and silence plays instead: 14 hops of audible
	// silence decay the last frame to rest (0.8^14 ≈ 0.044).
	a.unplayed = nil
	a.Feed(make([][2]float64, 14*analyserWindow/2))
	if got := a.Bands()[k]; got > 0.1 {
		t.Errorf("band %d = %v after the loud audio has passed the audible point, want decayed below 0.1", k, got)
	}
}

// TestReset pins the track-replacement reset: a loud analyser must return
// to a blank slate — zero frame, empty scope — so nothing of the old track
// leaks into the new one.
func TestReset(t *testing.T) {
	t.Parallel()
	const rate = 44100
	const k = 8
	a := newAnalyser(rate)
	a.Feed(sineFrames(bandCentre(k), 1, rate, 2*analyserWindow))
	if got := a.Bands()[k]; got < 0.9 {
		t.Fatalf("setup: band %d = %v, want loud before the reset", k, got)
	}
	a.Reset()
	for k, b := range a.Bands() {
		if b != 0 {
			t.Errorf("band %d = %v after Reset, want 0", k, b)
		}
	}
	if s := a.Scope(); len(s) != 0 {
		t.Errorf("Scope after Reset = %d samples, want empty", len(s))
	}
}

// TestScopeReturnsAudibleWindow: Scope returns the samples ending at the
// audible position (what was fed, minus whatever the device still holds
// unplayed), oldest to newest, in [-1,1], at most one window. A ramp makes
// the answer sample-exact.
func TestScopeReturnsAudibleWindow(t *testing.T) {
	t.Parallel()
	// A ramp from 0 to 1 over more than two full windows.
	const total = 2*analyserWindow + 1000
	frames := make([][2]float64, total)
	for i := range frames {
		v := float64(i) / float64(total)
		frames[i][0], frames[i][1] = v, v
	}
	a := newAnalyser(44100)
	if s := a.Scope(); s == nil || len(s) != 0 {
		t.Fatalf("Scope before any Feed = %#v, want an empty non-nil slice", s)
	}
	// While the audible span is shorter than a window, the scope is the
	// whole span: the trace grows from the right edge while it fills (the
	// post-reset and post-Load fill).
	partial := newAnalyser(44100)
	partial.Feed(frames[:analyserWindow/2])
	s := partial.Scope()
	if len(s) != analyserWindow/2 {
		t.Fatalf("partial Scope = %d samples, want half a window (the whole span)", len(s))
	}
	for i, v := range s {
		if want := float64(i) / float64(total); v != want {
			t.Fatalf("partial scope[%d] = %v, want %v (ramp sample %d)", i, v, want, i)
		}
	}
	// Feed in three bursts so the ring is written in pieces.
	a.Feed(frames[:analyserWindow])
	a.Feed(frames[analyserWindow : 2*analyserWindow])
	a.Feed(frames[2*analyserWindow:])
	// Nothing is unplayed (no device script): the audible position is the
	// feed's end, so the scope is the most recent full window.
	checkWindow := func(start int) {
		t.Helper()
		got := a.Scope()
		if len(got) != analyserWindow {
			t.Fatalf("Scope = %d samples, want one full window", len(got))
		}
		for i, v := range got {
			want := float64(start+i) / float64(total)
			if v != want {
				t.Fatalf("scope[%d] = %v, want %v (ramp sample %d)", i, v, want, start+i)
			}
		}
	}
	checkWindow(total - analyserWindow)
	// The device still holds 1000 frames unplayed: the window retreats by
	// exactly that many.
	a.unplayed = func() int { return 1000 }
	checkWindow(total - 1000 - analyserWindow)
	// The returned slice is a copy: mutating it must not affect later calls.
	got := a.Scope()
	got[0] = 0.42
	if v := a.Scope()[0]; v != float64(total-1000-analyserWindow)/float64(total) {
		t.Errorf("Scope[0] = %v after mutating a returned copy, want the original", v)
	}
}

// TestFeedReadsRace exercises the concurrency contract: one writer (the
// pump) feeds while readers (the TUI's poll) take Bands and Scope. It is a
// smoke test for the mutex — run it with -race — and makes no value
// assertions.
func TestFeedReadsRace(t *testing.T) {
	t.Parallel()
	a := newAnalyser(44100)
	frames := sineFrames(440, 0.5, 44100, 1024)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			a.Feed(frames)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			a.Bands()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			a.Scope()
		}
	}()
	wg.Wait()
}
