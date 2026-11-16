package audio

import (
	"math"
	"sync"

	"github.com/ali5ter/bc-radio/internal/contract"
)

// ringSeconds is how much audio the analyser's ring buffer retains: 2
// seconds of mono at the device rate. It must cover the device's largest
// unplayed span (oto's default buffer, half a second) plus one transform
// window: every sample the ring keeps is either audible right now or still
// queued behind the audible point.
const ringSeconds = 2

// analyserWindow is the number of mono samples in one band frame. 2048
// samples at 44.1 kHz is ~46 ms of audio, which is the transform's
// frequency resolution (rate/window) and the analysis hop (50% overlap:
// a new frame every 23 ms of audible time, ~43 Hz).
const analyserWindow = 2048

// analyserFMin and analyserFMax bound the contract.BandCount log-spaced
// bands, low to high. 20 Hz to 16 kHz covers the musical spectrum with
// good bass resolution and stays well under the 22.05 kHz Nyquist.
const (
	analyserFMin = 20.0
	analyserFMax = 16000.0
)

// The band power-to-level calibration: a log (dB) scale, not linear.
// Real music spreads band power over 40–60 dB, so a linear scale reads
// the whole spectrum in the bottom few percent of the display (the v1.0
// Goertzel's defect). On the dB scale an equal power change moves the
// level by an equal amount, so the display tracks the music's shape and
// dynamics: silenceGate floors true silence, and logRange spans roughly
// the dynamic range of a mixed track.
const (
	logOffset = 10.0 // dB of band power that reads 0
	logRange  = 50.0 // dB span that maps 0 → 1
)

// silenceGate is the max-abs sample level below which a window is
// treated as silence: the publish path decays the last frame instead of
// running the FFT, so a muted or idle track settles to rest without
// amplifying the decoder's noise floor.
const silenceGate = 1e-5

// gateDecay is the fraction of its level a band retains per hop of
// audible time: 0.8 per 23 ms hop settles a vanished track to visual rest
// in about half a second (0.8^20 ≈ 0.01). The publish is anchored to
// audible time, so the decay rate tracks the music, not the poll rate.
const gateDecay = 0.8

// analyser is the 16-band FFT analyser: a lightweight pure-Go transform
// (fftInPlace, Hann window, no dependency) over a ring buffer of the audio
// the device plays.
//
// The device is a pull source with an internal buffer: it refills that
// buffer in one burst, then goes quiet for as long as the buffer takes to
// drain. Publishing on each feed burst — the v1.0 behaviour — meant the
// published frame, the target the whole visual chases, changed only twice
// a second: the bars rushed to a target, froze, rushed, froze. Instead the
// analyser keeps a ring buffer of everything the device has been given
// (post-gain, downmixed to mono) and anchors its reads on what is audible
// now — the samples already written minus whatever the device still holds
// unplayed — a position that advances in real time as the buffer drains.
//
// The pump is the sole writer (the device's goroutine); Bands and Scope
// are the readers (the TUI's 60 fps poll). A new band frame publishes once
// the audible position advances a hop (50% overlap, ~23 ms, ~43 Hz) — the
// TUI's poll drives it, so there is no second audio goroutine. Reset
// rebuilds the ring from zero for a replaced track.
type analyser struct {
	rate    int
	window  int
	hop     int
	size    int          // ring capacity: ringSeconds of samples at the device rate
	twiddle []complex128 // precomputed e^{−2πi·k/n}, window/2 entries
	hann    []float64    // Hann window, window entries
	cbuf    []complex128 // reusable FFT work buffer
	powers  []float64    // reusable |X|² buffer, window/2 entries
	win     []float64    // reusable mono window, window entries
	edges   []float64    // contract.BandCount+1 log-spaced band edges, Hz

	mu       sync.Mutex // guards ring, tail, written, lastEnd, out, unplayed
	ring     []float64  // mono, oldest to newest, at most size samples
	tail     int        // next write position in the ring
	written  int        // total mono samples appended since the last reset
	lastEnd  int        // ring position where the last published frame ends
	out      []float64  // latest published frame, len == contract.BandCount
	unplayed func() int // frames the device still holds unplayed; nil → 0
}

// newAnalyser builds a contract.BandCount-band analyser sampling at the
// device rate, which must also be the rate of the unplayed frames the
// device reports — the two must agree for the audible-position math to
// hold.
func newAnalyser(rate int) *analyser {
	a := &analyser{
		rate:    rate,
		window:  analyserWindow,
		hop:     analyserWindow / 2,
		size:    rate * ringSeconds,
		ring:    make([]float64, rate*ringSeconds),
		twiddle: buildTwiddles(analyserWindow),
		hann:    buildHannWindow(analyserWindow),
		cbuf:    make([]complex128, analyserWindow),
		powers:  make([]float64, analyserWindow/2),
		win:     make([]float64, analyserWindow),
		edges:   make([]float64, contract.BandCount+1),
		out:     make([]float64, contract.BandCount),
	}
	for k := range a.edges {
		a.edges[k] = bandEdge(k)
	}
	return a
}

// bandEdge returns the lower edge of band k — k in 0..contract.BandCount —
// log-spaced between analyserFMin and analyserFMax. The 17 edges bound
// the 16 bands; band k spans [edge(k), edge(k+1)).
func bandEdge(k int) float64 {
	return analyserFMin * math.Pow(analyserFMax/analyserFMin, float64(k)/float64(contract.BandCount))
}

// Feed appends post-gain stereo frames, downmixed to mono, to the ring.
// It publishes nothing: the publish decision lives in Bands, because its
// trigger is the audible position, which only moves as the device drains
// its buffer — information the feed does not have. It is intended to be
// called from the device pump only (single goroutine).
func (a *analyser) Feed(frames [][2]float64) {
	a.mu.Lock()
	for i := range frames {
		a.ring[a.tail] = (frames[i][0] + frames[i][1]) / 2
		a.tail = (a.tail + 1) % a.size
	}
	a.written += len(frames)
	a.mu.Unlock()
}

// setUnplayed wires the device's buffer-depth probe into the analyser:
// unplayed reports the frames the device has already pulled but not yet
// played, at the analyser's sample rate, and the anchor subtracts them
// from what has been written to find what is audible now. It is called
// once at construction, before playback starts.
func (a *analyser) setUnplayed(f func() int) {
	a.mu.Lock()
	a.unplayed = f
	a.mu.Unlock()
}

// anchor returns the ring position of the sample that is reaching the
// speaker right now: everything written minus whatever the device still
// holds unplayed. The device drains at the sample rate, so the anchor
// advances in real time while audio plays, and sits still when the device
// does not pull (a full buffer, a pause).
func (a *analyser) anchor() int {
	a.mu.Lock()
	written := a.written
	f := a.unplayed
	a.mu.Unlock()
	end := written
	if f != nil {
		end -= f()
	}
	if end < 0 {
		// More is queued in the device than the ring knows about — a
		// reset that raced a pull burst: nothing is past the start.
		end = 0
	}
	return end
}

// copyEnding copies the len(dst) mono samples ending at ring position end
// into dst, oldest to newest, unwrapping the ring. The caller holds the
// mutex and guarantees end ≥ len(dst) (and thus ≥ 0).
func (a *analyser) copyEnding(dst []float64, end int) {
	n := len(dst)
	off := (end - n) % a.size
	first := min(a.size-off, n)
	copy(dst, a.ring[off:off+first])
	if first < n {
		copy(dst[first:], a.ring[:n-first])
	}
}

// publish computes one band frame from a full window of mono samples and
// writes it into out; a silent window decays the last frame instead, by
// one gateDecay factor per hop of audible time advanced, so the settle
// rate is a property of the music rather than of the poll. The caller
// must hold the mutex.
func (a *analyser) publish(win []float64, hops int) {
	// Silence gate: a max-abs scan is two orders of magnitude cheaper than
	// the transform and fires on idle, gaps, and a muted track. Rather
	// than compute a frame of the noise floor, decay the last one toward
	// rest (the display's own easing then settles it).
	maxAbs := 0.0
	for _, v := range win {
		if av := math.Abs(v); av > maxAbs {
			maxAbs = av
		}
	}
	if maxAbs < silenceGate {
		d := math.Pow(gateDecay, float64(hops))
		for k := range a.out {
			a.out[k] *= d
		}
		return
	}
	// Window the samples into the reusable complex buffer and transform in
	// place. The window is always a full window, so no zeroing is needed.
	for i := range a.window {
		a.cbuf[i] = complex(win[i]*a.hann[i], 0)
	}
	fftInPlace(a.cbuf, a.twiddle)
	// Power spectrum |X|² into the reusable buffer. Skipping the sqrt per
	// bin halves the work; the 10·log10 below absorbs the factor of two,
	// so band levels stay in [0,1]. Bin 0 (DC) is display noise.
	powers := a.powers
	powers[0] = 0
	half := a.window / 2
	for i := 1; i < half; i++ {
		re, im := real(a.cbuf[i]), imag(a.cbuf[i])
		powers[i] = re*re + im*im
	}
	binHz := float64(a.rate) / float64(a.window)
	for b := range a.out {
		a.out[b] = a.bandLevel(powers, binHz, b)
	}
}

// bandLevel maps band b's average power over its frequency range to [0,1]
// on the dB scale: (10·log10(power) + logOffset) / logRange, clamped.
func (a *analyser) bandLevel(powers []float64, binHz float64, b int) float64 {
	sum := a.bandPower(powers, a.edges[b]/binHz, a.edges[b+1]/binHz)
	if sum <= 0 {
		return 0
	}
	level := (10*math.Log10(sum) + logOffset) / logRange
	if level > 1 {
		return 1
	}
	if level < 0 {
		return 0
	}
	return level
}

// bandPower averages the power spectrum between two bin positions,
// linearly interpolated. The sample count scales with the band's bin
// span (two per bin, 4 to 32), so narrow bass bands and wide treble bands
// each average their own range rather than a fixed number of bins.
func (a *analyser) bandPower(powers []float64, loPos, hiPos float64) float64 {
	if len(powers) == 0 {
		return 0
	}
	maxPos := float64(len(powers) - 1)
	// Bin 0 is DC; the lowest edge (20 Hz) clamps to bin 1.
	loPos = math.Max(1.0, math.Min(maxPos, loPos))
	hiPos = math.Max(loPos, math.Min(maxPos, hiPos))
	span := hiPos - loPos
	if span <= 0 {
		return a.samplePower(powers, loPos)
	}
	count := int(math.Ceil(span * 2))
	if count < 4 {
		count = 4
	}
	if count > 32 {
		count = 32
	}
	var sum float64
	for i := 0; i < count; i++ {
		t := (float64(i) + 0.5) / float64(count)
		sum += a.samplePower(powers, loPos+t*span)
	}
	return sum / float64(count)
}

// samplePower linearly interpolates the power spectrum at a fractional bin
// position.
func (a *analyser) samplePower(powers []float64, pos float64) float64 {
	switch len(powers) {
	case 0:
		return 0
	case 1:
		return powers[0]
	}
	last := float64(len(powers) - 1)
	if pos <= 0 {
		return powers[0]
	}
	if pos >= last {
		return powers[len(powers)-1]
	}
	idx := int(pos)
	frac := pos - float64(idx)
	return powers[idx]*(1-frac) + powers[idx+1]*frac
}

// Bands returns the latest published frame: contract.BandCount values in
// [0,1], low to high frequency. The frame refreshes as the audible
// position advances — this call, driven by the TUI's 60 fps poll, is what
// publishes it — and decays to rest on silence. It is all zeros until a
// full window is past the audible point (just after a reset).
func (a *analyser) Bands() []float64 {
	end := a.anchor()
	a.mu.Lock()
	if end >= a.window && end-a.lastEnd >= a.hop {
		a.copyEnding(a.win, end)
		a.publish(a.win, (end-a.lastEnd)/a.hop)
		a.lastEnd = end
	}
	out := make([]float64, len(a.out))
	copy(out, a.out)
	a.mu.Unlock()
	return out
}

// Scope returns the mono samples ending at the audible position — the most
// recent post-gain audio, oldest to newest, in [-1,1], at most one window
// (2048 samples, ~46 ms at 44.1 kHz). It grows from the right edge while
// the window fills after a reset, and is empty before audio is produced.
// Display data only — safe for the TUI's 60 fps render loop to call
// concurrently with Feed.
func (a *analyser) Scope() []float64 {
	end := a.anchor()
	a.mu.Lock()
	n := min(end, a.window)
	out := make([]float64, n)
	if n > 0 {
		a.copyEnding(out, end)
	}
	a.mu.Unlock()
	return out
}

// Reset rebuilds the analyser from zero for a replaced track: the ring
// rewinds, the anchor falls to the start, and the frame clears, so nothing
// of the old track is attributed to the new one. The engine calls it when
// it flushes the device in Load.
func (a *analyser) Reset() {
	a.mu.Lock()
	a.tail = 0
	a.written = 0
	a.lastEnd = 0
	clear(a.out)
	a.mu.Unlock()
}
