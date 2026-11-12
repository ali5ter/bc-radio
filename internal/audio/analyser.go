package audio

import (
	"math"
	"sync"

	"bc-radio/internal/contract"
)

// analyserWindow is the number of mono samples in one band frame. 2048
// samples at 44.1 kHz is ~46 ms, resolving down to ~22 Hz and yielding a
// new band frame every ~21 ms of audio. The pump feeds the analyser in
// device-buffer bursts, so the published frame is at most one device
// buffer old — the TUI eases toward these targets at its own 60 fps render
// rate, so the visual stays smooth.
const analyserWindow = 2048

// analyserFMin and analyserFMax bound the contract.BandCount log-spaced
// band centre frequencies, low to high. The spike (docs/spike.md) pins the
// band count and the Goertzel approach, not the layout; 20 Hz to 16 kHz
// covers the musical spectrum with good bass resolution and stays well
// under the 22.05 kHz Nyquist.
const (
	analyserFMin = 20.0
	analyserFMax = 16000.0
)

// analyser is the 16-band Goertzel analyser — the locked v1 decision
// (docs/spike.md): a lightweight Goertzel in pure Go, not an FFT library,
// run at real time.
//
// It is fed post-gain stereo frames by the pump (single goroutine, the
// device) and publishes contract.BandCount magnitudes in [0,1], ordered low
// to high frequency. Feed downmixes each frame to mono and accumulates
// samples; each time a full window is available it computes the Goertzel
// magnitude of every band and publishes the frame. Bands may be called
// concurrently (it is the TUI's 60 fps data source) and returns the latest
// published frame.
type analyser struct {
	window int
	// Real and imaginary parts of e^{2πi·f_k/rate} per band, precomputed.
	// The band filter is the Goertzel single-pole form run with this
	// complex coefficient (bandMag); the textbook two-term real recurrence
	// is numerically unstable in float64 for content near the bin, so the
	// complex form is used instead (same DTFT bin, bounded error).
	wr, wi []float64

	mu      sync.Mutex
	pending []float64 // mono samples not yet part of a completed window
	out     []float64 // latest published frame, len == contract.BandCount
}

// newAnalyser builds a contract.BandCount-band analyser sampling at the
// device rate.
func newAnalyser(rate int) *analyser {
	a := &analyser{
		window: analyserWindow,
		wr:     make([]float64, contract.BandCount),
		wi:     make([]float64, contract.BandCount),
		out:    make([]float64, contract.BandCount),
	}
	for k := 0; k < contract.BandCount; k++ {
		theta := 2 * math.Pi * bandFrequency(k) / float64(rate)
		a.wr[k] = math.Cos(theta)
		a.wi[k] = -math.Sin(theta)
	}
	return a
}

// bandFrequency returns the centre frequency of band k, log-spaced between
// analyserFMin and analyserFMax.
func bandFrequency(k int) float64 {
	if contract.BandCount <= 1 {
		return analyserFMin
	}
	return analyserFMin * math.Pow(analyserFMax/analyserFMin, float64(k)/float64(contract.BandCount-1))
}

// Feed adds post-gain stereo frames, downmixing to mono, and publishes one
// band frame per full window accumulated. It is intended to be called from
// the device pump only (single goroutine); pending is guarded by that
// single ownership, while the mutex guards the shared out frame against the
// concurrent Bands reader.
func (a *analyser) Feed(frames [][2]float64) {
	for i := range frames {
		a.pending = append(a.pending, (frames[i][0]+frames[i][1])/2)
	}
	// Publish complete windows, then compact the unconsumed tail in place
	// so the backing array does not grow without bound.
	n := 0
	for n+a.window <= len(a.pending) {
		a.publish(a.pending[n : n+a.window])
		n += a.window
	}
	copy(a.pending, a.pending[n:])
	a.pending = a.pending[:len(a.pending)-n]
}

// publish computes one band frame from a full window of mono samples. The
// mutex is held only for the band pass, so a concurrent Bands call waits at
// most the duration of one window's computation.
func (a *analyser) publish(win []float64) {
	n := float64(len(win))
	a.mu.Lock()
	for k := range a.wr {
		a.out[k] = bandMag(win, a.wr[k], a.wi[k], n)
	}
	a.mu.Unlock()
}

// Bands returns a copy of the latest published frame: contract.BandCount
// values in [0,1], low to high frequency. Before the first window
// completes it returns all zeros.
func (a *analyser) Bands() []float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]float64, contract.BandCount)
	copy(out, a.out)
	return out
}

// bandMag returns the normalised magnitude of the DTFT bin with unit
// coefficient w = (wr, wi) over the window, using the Goertzel single-pole
// recursion u ← w·(u + x) with w complex, kept as a pair of float64s.
//
// That is algebraically the textbook Goertzel filter (v[n] = x[n] + 2cosθ·
// v[n−1] − v[n−2] computes the same bin), but the textbook two-term real
// form is numerically unstable in float64 for content near the bin
// frequency: its rounding error is not damped, and a full-scale on-bin sine
// ends up reading 0.37–1.0 depending on window phase. The complex single-
// pole form's error stays proportional to the signal, so a full-scale sine
// at the bin centre reads ~1 across all window phases; other content is
// attenuated by the rectangular window's spectral leakage. The result is
// clamped to [0,1] as the contract requires.
func bandMag(x []float64, wr, wi, n float64) float64 {
	re, im := 0.0, 0.0
	for _, v := range x {
		re, im = wr*(re+v)-wi*im, wr*im+wi*(re+v)
	}
	mag := 2 * math.Hypot(re, im) / n
	if mag > 1 {
		mag = 1
	}
	return mag
}
