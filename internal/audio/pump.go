package audio

import (
	"encoding/binary"
	"io"
	"math"

	"bc-radio/internal/contract"
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
// what makes track transitions gapless. With no current track it emits
// silence, so the device keeps running without a gap or a busy loop.
//
// Read never returns io.EOF or another error: the oto player would stop on
// either. It always fills the whole buffer and returns nil.
//
// The pump is the single owner of chain lifecycle: it is the only goroutine
// that decodes a chain, and it is the only one that closes a chain in
// normal operation (depart, on departure). Stop may also close chains on
// the way out, but it takes the engine's decode mutex, which a Read holds
// for its whole duration, so a close never races a decode of the same
// chain.
type pump struct {
	eng *Engine

	frames [][2]float64 // reusable scratch, grown on demand to the largest Read
	last   *chain       // last chain this pump set up to decode
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
			break
		}

		n, ok := cur.tail.Stream(out[filled:])
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
// always safe.
func (p *pump) advance(cur *chain) {
	if cur == p.last {
		return
	}
	if p.last != nil {
		p.depart(p.last)
	}
	if cur != nil {
		p.eng.activate(cur)
	}
	p.last = cur
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
