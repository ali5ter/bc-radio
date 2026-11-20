package audio

import (
	"encoding/binary"
	"io"
	"math"

	"github.com/ali5ter/bc-radio/internal/contract"
)

// bytesPerFrame is the size of one interleaved float32 stereo frame in the
// byte buffer the device hands us: 2 channels × 4 bytes.
const bytesPerFrame = 8

// pump is the io.Reader handed to the oto device. The device's goroutine
// calls Read to pull audio in real time; device backpressure is what paces
// decoding at exactly one second of audio per second of wall time.
//
// Read decodes the engine's current chain into the buffer, feeds everything
// it emits — audio and silent tail alike — to the analyser, and encodes the
// result as float32 little-endian stereo. The analyser's ring must mirror
// exactly what the device plays for its audible-position anchor to stay
// honest, which is why the silent tail is fed too. When the current chain
// drains, the pump installs the
// prepared next chain (if any) and keeps filling the same buffer, which is
// what makes track transitions gapless. With a non-zero crossfade set, a
// drain that has a prepared next in time starts a crossfade instead (fade):
// the incoming track fades in on an equal-power curve while the outgoing
// one fades out over its own final frames, and the mix — never a raw chain
// — is what is emitted. With no current track it emits silence, so the
// device keeps running without a gap or a busy loop.
//
// Read never returns io.EOF or another error: the oto player would stop on
// either. It always fills the whole buffer and returns nil.
//
// The pump is the single owner of chain lifecycle: it is the only goroutine
// that decodes a chain, and it is the only one that closes a chain in
// normal operation (depart, on departure — a crossfade's fading-out ghost
// departs when the fade completes or is aborted). Stop may also close
// chains on the way out, but it takes the engine's decode mutex, which a
// Read holds for its whole duration, so a close never races a decode of the
// same chain.
type pump struct {
	eng *Engine

	frames [][2]float64 // reusable scratch, grown on demand to the largest Read
	last   *chain       // last chain this pump set up to decode
	fade   *fade        // in-flight crossfade; nil otherwise
}

// fade is a crossfade in progress: in (the engine's current chain) fades in
// while out — the chain it replaced, no longer referenced by the engine —
// fades out. total is the fade window in frames (the outgoing track's
// remaining length when the fade started, capped by the crossfade duration),
// and left the frames of it still to mix. Both tracks advance one frame per
// mixed frame, so the window is the fade's clock: the mix gains are a pure
// function of left/total, continuous at both ends (the incoming at 0 going
// to 1, the outgoing at 1 going to 0), which is what keeps the transition
// click-free.
type fade struct {
	out *chain // fading out; departs when the fade ends or is aborted
	in  *chain // fading in — the engine's current chain while the fade runs

	total int // the fade window, in frames
	left  int // frames of the window still to mix

	// outDrain is set once the outgoing stream has run dry (a metadata
	// duration that over-estimates the real track): its side of the mix is
	// silence from there, and the window runs out on silence rather than
	// re-reading a drained streamer.
	outDrain bool

	// reusable mix scratch for the two sides, grown on demand like frames.
	inScratch  [][2]float64
	outScratch [][2]float64
}

// newPump builds the pump for an engine.
func newPump(e *Engine) *pump { return &pump{eng: e} }

// Read implements io.Reader. See the type comment for the contract.
//
// It holds the engine's decode mutex for its whole duration: Stop takes the
// same mutex around the chain closes, so the two can never act on a chain
// concurrently.
func (p *pump) Read(buf []byte) (int, error) {
	p.eng.decMu.Lock()
	defer p.eng.decMu.Unlock()

	nframes := len(buf) / bytesPerFrame
	if nframes == 0 {
		// Smaller than one stereo frame: silence.
		for i := range buf {
			buf[i] = 0
		}
		return len(buf), nil
	}
	if cap(p.frames) < nframes {
		p.frames = make([][2]float64, nframes)
	}
	out := p.frames[:nframes]

	// Install a prepared next track if we are idle and one is ready.
	p.eng.sync()

	filled := 0
	for filled < nframes {
		cur := p.eng.currentChain()

		// Reconcile the chain we were decoding with the one now current:
		// a drain that installed a next, or a Load that replaced it.
		p.advance(cur)

		if cur == nil {
			// Idle: nothing to decode; the tail is silenced below.
			p.abortFade()
			break
		}

		// A natural end within the crossfade window and a prepared next
		// becomes a crossfade, not a cut: the engine promotes the next to
		// current (TrackStarted) and the old track becomes the fade's ghost.
		p.tryFade(cur)

		n, ok := p.stream(cur, out[filled:])
		if n > 0 {
			p.eng.addPos(n)
		}
		filled += n
		// The stream is over: ok=false is end-of-stream in the beep
		// convention, and n==0 is a defensive guard against a streamer
		// that returns nothing yet claims there is more (which would spin
		// this loop forever). Report the chain's departure now — a drained
		// or failed track must not wait for the next Read — clear it as
		// last, and let the engine hand the slot to a prepared next; if
		// that leaves nothing to play, stop and let the tail silence below.
		if n == 0 || !ok {
			// The incoming track ended mid-fade (a short one): the fade
			// cannot complete, so the ghost departs with it.
			p.abortFade()
			p.depart(cur)
			p.last = nil
			p.eng.chainDrained(cur)
			if n == 0 && p.eng.currentChain() == nil {
				break
			}
		}
	}
	// The scratch survives between Reads; silence whatever the loop did not
	// fill, or audio from the previous Read would bleed into this one.
	for i := filled; i < nframes; i++ {
		out[i][0], out[i][1] = 0, 0
	}
	// Feed the whole buffer to the analyser — decoded audio and silent tail
	// alike: it must mirror exactly what the device plays (see the type
	// comment).
	p.eng.analyser.Feed(out)

	encodeFloat32(buf, out)
	return nframes * bytesPerFrame, nil
}

// advance reconciles the pump's last-decoded chain with cur, the chain now
// current. When they differ — a Load replaced it, or a gapless transition
// installed the next — it reports the departed chain and closes it, then
// activates (resets position, emits TrackStarted) a newly current one. It
// is called from Read only, between Stream calls, so closing p.last here is
// always safe. A crossfade's swap is the one deliberate exception: tryFade
// sets p.last to the incoming chain itself, so the fade's ghost (no longer
// current) is not reported as a departure — it departs when the fade ends
// or is aborted. Any other chain change while a fade is in flight (a Load
// or the incoming track's own end) aborts it first.
func (p *pump) advance(cur *chain) {
	if cur == p.last {
		return
	}
	if p.fade != nil {
		p.abortFade()
	}
	if p.last != nil {
		p.depart(p.last)
	}
	if cur != nil {
		p.eng.activate(cur)
	}
	p.last = cur
}

// stream reads the current chain into dst, or — while a crossfade is in
// flight — mixes the incoming chain (fading in) with the fade's ghost
// (fading out) on an equal-power curve. It returns the frames written and
// whether the current chain's stream is over; the ghost's end of stream is
// the fade's own business (outDrain), never the caller's.
func (p *pump) stream(cur *chain, dst [][2]float64) (int, bool) {
	f := p.fade
	if f == nil {
		return cur.tail.Stream(dst)
	}
	n := len(dst)
	if cap(f.inScratch) < n {
		f.inScratch = make([][2]float64, n)
	}
	in := f.inScratch[:n]
	if cap(f.outScratch) < n {
		f.outScratch = make([][2]float64, n)
	}
	out := f.outScratch[:n]

	nIn, okIn := f.in.tail.Stream(in)
	if nIn > n {
		nIn = n
	}
	nOut, okOut := 0, true
	if !f.outDrain {
		nOut, okOut = f.out.tail.Stream(out)
		if nOut > n {
			nOut = n
		}
		if nOut == 0 || !okOut {
			f.outDrain = true
		}
	}

	// The mix runs one frame per mixed frame: the gains are a function of
	// the window's remaining fraction — the incoming at 0 rising to 1, the
	// outgoing at 1 falling to 0 (equal power: cos² + sin² = 1) — so the
	// transition is continuous at both ends. The window advances with the
	// incoming track, and a drained ghost contributes silence for the rest
	// of it. The sum is clamped: equal power is constant *power*, not
	// constant peak, and two hot passages could otherwise clip the DAC.
	for i := 0; i < nIn; i++ {
		// x is the window's remaining fraction before this frame. It can
		// go negative when the window ends mid-buffer: clamp to 0, which
		// is full incoming gain — exactly where the curve lands as the
		// window closes, so no step at the clamp.
		x := (float64(f.left) - float64(i)) / float64(f.total)
		if x < 0 {
			x = 0
		}
		if x > 1 {
			x = 1
		}
		gainIn := math.Cos(x * fadeHalfPi)
		gainOut := math.Sin(x * fadeHalfPi)
		var o [2]float64
		if i < nOut {
			o = out[i]
		}
		dst[i][0] = clampMix(in[i][0]*gainIn + o[0]*gainOut)
		dst[i][1] = clampMix(in[i][1]*gainIn + o[1]*gainOut)
	}
	if f.left > nIn {
		f.left -= nIn
	} else {
		f.left = 0
	}
	// The window is spent: the ghost is done (drained, or cut at a gain of
	// 0 — the metadata ran short, and the cut is inaudible at 0).
	if f.left <= 0 {
		p.depart(f.out)
		p.fade = nil
	}
	return nIn, okIn
}

// fadeHalfPi is π/2: the sweep of the mix curve across the fade window.
const fadeHalfPi = math.Pi / 2

// clampMix limits a mixed sample to [-1,1]: the equal-power sum of two
// full-scale signals can peak at √2, which would clip.
func clampMix(v float64) float64 {
	if v > 1 {
		return 1
	}
	if v < -1 {
		return -1
	}
	return v
}

// tryFade starts a crossfade on cur when its last frames fall inside the
// crossfade window and a prepared next is ready: the engine promotes the
// next to current, and cur becomes the fade's ghost. Everything else — the
// crossfade off, an unknown track length, a window not yet reached, or a
// next not prepared in time — is a silent no-op, so the transition degrades
// to the plain gapless one.
func (p *pump) tryFade(cur *chain) {
	e := p.eng
	if p.fade != nil || e.crossfade <= 0 || cur.total <= 0 {
		return
	}
	remaining := cur.total - e.posFrames()
	if remaining <= 0 || remaining > e.crossFrames() {
		return
	}
	in := e.beginCrossfade(cur)
	if in == nil {
		return
	}
	p.fade = &fade{out: cur, in: in, total: remaining, left: remaining}
	// The ghost is not a departure (it is fading, not gone), so advance
	// must not report it when the swap lands on the incoming chain.
	p.last = in
}

// skipFade starts a user-skip crossfade: the same equal-power mix as a
// natural end (stream), but the window is the full crossfade duration —
// anchored at the moment of the skip, not at the outgoing track's final
// frames — so a skip mid-track fades over the same time it would at the
// end. The ghost is cut at the window's end (at a gain of 0, inaudible —
// stream's window-spent path), never waited on, even though its stream is
// still full. The caller must ensure no fade is in flight (FadeNext
// does); the ghost is not a departure, so last moves to the incoming
// chain, exactly as tryFade does.
func (p *pump) skipFade(ghost, in *chain, total int) {
	p.fade = &fade{out: ghost, in: in, total: total, left: total}
	p.last = in
}

// abortFade ends an in-flight crossfade without its normal completion — the
// engine lost the incoming track (a Load, or its own end) or the engine is
// stopping: the ghost departs (its end event, or a failure when its tail
// died) and the mix stops. Safe to call with no fade.
func (p *pump) abortFade() {
	if p.fade == nil {
		return
	}
	p.depart(p.fade.out)
	p.fade = nil
}

// depart reports a chain that is no longer playing —
// EventPlaybackFailed if its tail died in an error (a dead stream, a decode
// failure), EventTrackEnded if it drained or was cut cleanly — and closes
// it. It is the single departure point: Read calls it the moment a stream
// is exhausted, advance calls it when a chain is replaced. A chain departs
// at most once: after the Read path, p.last is nil, so advance cannot
// report the same chain again.
func (p *pump) depart(cur *chain) {
	ev := contract.Event{Kind: contract.EventTrackEnded, Track: cur.track}
	if err := cur.tail.Err(); err != nil {
		ev.Kind = contract.EventPlaybackFailed
		ev.Err = err
	}
	p.eng.emit(ev)
	cur.close()
}

// encodeFloat32 writes the first len(buf)/bytesPerFrame frames of src into
// buf as little-endian float32 stereo; any trailing bytes stay silence.
func encodeFloat32(buf []byte, src [][2]float64) {
	n := len(buf) / bytesPerFrame
	if n > len(src) {
		n = len(src)
	}
	for i := 0; i < n; i++ {
		off := i * bytesPerFrame
		binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(float32(src[i][0])))
		binary.LittleEndian.PutUint32(buf[off+4:], math.Float32bits(float32(src[i][1])))
	}
	for i := n * bytesPerFrame; i < len(buf); i++ {
		buf[i] = 0
	}
}

var _ io.Reader = (*pump)(nil)
