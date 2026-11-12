package audio

import (
	"math"
	"testing"

	"bc-radio/internal/contract"
)

// TestBandFrequencyLayout pins the log-spaced layout: contract.BandCount
// strictly increasing centres from analyserFMin to analyserFMax.
func TestBandFrequencyLayout(t *testing.T) {
	t.Parallel()
	prev := 0.0
	for k := 0; k < contract.BandCount; k++ {
		f := bandFrequency(k)
		if f <= prev {
			t.Errorf("band %d centre %v Hz is not above the previous %v Hz", k, f, prev)
		}
		prev = f
	}
	if got := bandFrequency(0); math.Abs(got-analyserFMin) > 1e-9 {
		t.Errorf("band 0 centre = %v Hz, want %v", got, analyserFMin)
	}
	last := bandFrequency(contract.BandCount - 1)
	if math.Abs(last-analyserFMax)/analyserFMax > 1e-9 {
		t.Errorf("top band centre = %v Hz, want %v", last, analyserFMax)
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

// TestFeedSineAtBandCentre feeds a full-scale sine exactly at a band's
// centre: that band must read ~1 and the others stay well below it.
func TestFeedSineAtBandCentre(t *testing.T) {
	t.Parallel()
	const rate = 44100
	const k = 8
	a := newAnalyser(rate)
	a.Feed(sineFrames(bandFrequency(k), 1, rate, 4*analyserWindow))
	bands := a.Bands()
	if bands[k] < 0.9 {
		t.Errorf("band %d = %v for a full-scale sine at its own centre, want >= 0.9", k, bands[k])
	}
	for j, b := range bands {
		if j != k && b > 0.2 {
			t.Errorf("band %d = %v, want < 0.2 (leakage must not rival the peak %v)", j, b, bands[k])
		}
	}
}

// TestFeedSineOffCentre feeds a 1 kHz sine (not a band centre): the closest
// band must be the peak, weak but clearly above the runner-up.
//
// The expected magnitude is far from 1 for two compounding reasons. First,
// a real sine is two ½-amplitude complex exponentials at ±1 kHz, and a
// rectangular window's bin response falls off with distance from those
// lines: the nearest band centre is 1103.78 Hz (band 9), so the bin is
// off-centre and reads only a few percent. Second, the bin's magnitude is
// window-phase-dependent — the two ½-amplitude components acquire
// opposite-sign window phases, so where the 2048-sample window lands on the
// sine changes the reading. Scanning every window start (direct DTFT, not
// Goertzel) the true peak magnitude ranges 0.033–0.038 and the
// peak/runner-up ratio dips to about 1.3. The bounds below straddle that
// true range so the test holds for any window alignment.
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
		if math.Abs(bandFrequency(j)-f) < math.Abs(bandFrequency(want)-f) {
			want = j
		}
	}
	if peak != want {
		t.Errorf("peak band = %d (%v Hz), want %d (%v Hz) for a %v Hz sine", peak, bandFrequency(peak), want, bandFrequency(want), f)
	}
	if bands[peak] < 0.02 || bands[peak] > 0.05 {
		t.Errorf("peak band = %v, want a weak off-centre 1 kHz reading in (0.02, 0.05)", bands[peak])
	}
	runner := float64(0)
	for j, b := range bands {
		if j != peak && b > runner {
			runner = b
		}
	}
	if bands[peak] < 1.3*runner {
		t.Errorf("peak %v is not clearly above the runner-up %v (worst window phase gives ratio ~1.3)", bands[peak], runner)
	}
}

// TestFeedClampsToUnit: content louder than full scale must clamp to 1.
func TestFeedClampsToUnit(t *testing.T) {
	t.Parallel()
	const rate = 44100
	const k = 8
	a := newAnalyser(rate)
	a.Feed(sineFrames(bandFrequency(k), 1.5, rate, 2*analyserWindow))
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

// TestFeedCompactsPending: full windows are published, the unconsumed tail
// is kept, and nothing accumulates without bound.
func TestFeedCompactsPending(t *testing.T) {
	t.Parallel()
	a := newAnalyser(44100)
	a.Feed(sineFrames(440, 0.5, 44100, 2*analyserWindow+500))
	if len(a.pending) != 500 {
		t.Fatalf("pending = %d samples after 2 full windows + 500, want 500", len(a.pending))
	}
	a.Feed(sineFrames(440, 0.5, 44100, analyserWindow))
	if len(a.pending) != 500 {
		t.Fatalf("pending = %d after one more window completed the earlier 500, want 500", len(a.pending))
	}
}

// TestFeedArbitraryChunkSizes: the analyser must publish correctly no matter
// how the pump splits the audio — including one sample at a time, which
// guards the publish loop against infinite spin.
func TestFeedArbitraryChunkSizes(t *testing.T) {
	t.Parallel()
	const rate = 44100
	const k = 8
	frames := sineFrames(bandFrequency(k), 1, rate, 2*analyserWindow)
	a := newAnalyser(rate)
	for i := range frames {
		a.Feed(frames[i : i+1])
	}
	bands := a.Bands()
	if bands[k] < 0.9 {
		t.Errorf("band %d = %v after frame-by-frame Feed, want >= 0.9", k, bands[k])
	}
	if len(a.pending) != 0 {
		t.Errorf("pending = %d after an exact multiple of the window, want 0", len(a.pending))
	}
}
