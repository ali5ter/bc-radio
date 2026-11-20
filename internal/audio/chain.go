package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/ali5ter/bc-radio/internal/contract"
	"github.com/faiface/beep"
	"github.com/faiface/beep/mp3"
)

// resampleQuality is the beep resampler quality used when a source's sample
// rate differs from the device's. Quality 4 is "good quality, suitable for
// real-time" per beep's documentation. Bandcamp streams at 44.1kHz — the
// device rate — so this path is rarely taken, but it keeps arbitrary sources
// playable.
const resampleQuality = 4

// volume is the shared digital gain, 0..1. Every chain reads from it, so
// SetVolume affects the current track and all future ones without rebuilding
// any pipeline. It is the single source of truth for the engine's volume.
type volume struct {
	mu sync.Mutex
	g  float64
}

// set stores a gain, clamped to [0,1].
func (v *volume) set(g float64) {
	if g < 0 {
		g = 0
	}
	if g > 1 {
		g = 1
	}
	v.mu.Lock()
	v.g = g
	v.mu.Unlock()
}

// get returns the current gain.
func (v *volume) get() float64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.g
}

// volumeStage applies the shared gain to its upstream streamer. It is the
// tail of every chain, so the frames the pump reads from a chain are
// post-gain: the analyser (fed from those frames) always reflects what is
// actually heard.
type volumeStage struct {
	v *volume
	s beep.Streamer
}

// Stream implements beep.Streamer.
func (s *volumeStage) Stream(samples [][2]float64) (int, bool) {
	n, ok := s.s.Stream(samples)
	g := s.v.get()
	for i := range samples[:n] {
		samples[i][0] *= g
		samples[i][1] *= g
	}
	return n, ok
}

// Err implements beep.Streamer.
func (s *volumeStage) Err() error { return s.s.Err() }

// tailFrames is the number of frames a chain's tail will emit for a track
// of duration d at the device rate: the tail always runs at the device rate
// (the resampler, when present, converts to it), so the wall-clock duration
// times the device rate is the tail's output length. A non-positive
// duration (unknown) yields -1: the crossfade start cannot be timed, and
// the transition falls back to the plain gapless one.
func tailFrames(t contract.Track, deviceRate int) int {
	if t.Duration <= 0 {
		return -1
	}
	return int(float64(t.Duration) / float64(time.Second) * float64(deviceRate))
}

// chain is the audio pipeline for one track:
//
//	mp3 decoder → (resampler, only if the source rate differs) → volume
//
// The tail is the volume stage. When the source is exhausted the tail's
// Stream returns ok=false, which the pump detects as the end of the track.
type chain struct {
	track   contract.Track
	tail    beep.Streamer
	decoder beep.StreamSeekCloser // owns the stream body once decode succeeds
	body    io.ReadCloser         // closed directly only if no decoder was built

	// seekable is true when the decoder can move within the track: the
	// source rate is the device rate (no resampler in between — beep's
	// resampler has no flush, so a source seek would leave its stale
	// interpolation state behind) and the body the decoder indexed.
	// When false, seekTo is a no-op error.
	seekable bool

	// total is the frames the tail is expected to emit: the exact count
	// the decoder walked for a seekable chain, else the track's metadata
	// duration at the device rate — -1 when that is unknown. The pump's
	// crossfade start compares it against the frames already consumed; a
	// metadata estimate off by a few frames is inaudible (the mix is
	// continuous at both ends of the fade), and the walk's exact count
	// makes the timing and the seek clamp precise where the metadata is
	// not.
	total int

	closeOnce sync.Once // the pump and the engine (Stop) can both close a chain
}

// newChain opens the track's stream and builds its pipeline at deviceRate.
// A resampler is inserted only when the source sample rate differs from the
// device's. On any error the opened stream is closed.
func newChain(ctx context.Context, source contract.AudioSource, t contract.Track, deviceRate int, vol *volume) (*chain, error) {
	stream, err := source.Open(ctx, t)
	if err != nil {
		return nil, fmt.Errorf("audio: open %s — %s: %w", t.BandName, t.Title, err)
	}
	decoder, format, err := mp3.Decode(stream.Body)
	if err != nil {
		stream.Body.Close()
		return nil, fmt.Errorf("audio: decode %s — %s: %w", t.BandName, t.Title, err)
	}
	var s beep.Streamer = decoder
	resampled := int(format.SampleRate) != deviceRate
	if resampled {
		s = beep.Resample(resampleQuality, format.SampleRate, beep.SampleRate(deviceRate), decoder)
	}
	total := tailFrames(t, deviceRate)
	seekable := false
	if !resampled {
		// The walk the decoder did on Open (it indexes a seekable body
		// while building) counts the track's frames exactly at the
		// device rate — it is the seek's clamp and a tighter crossfade
		// clock than the metadata. A body the decoder could not index
		// reports 0 and stays unseekable.
		if n := decoder.Len(); n > 0 {
			total = n
			seekable = true
		}
	}
	return &chain{
		track:    t,
		tail:     &volumeStage{v: vol, s: s},
		decoder:  decoder,
		body:     stream.Body,
		total:    total,
		seekable: seekable,
	}, nil
}

// errNotSeekable is returned by seekTo for a chain that cannot move — a
// resampled source, or a body the decoder could not index.
var errNotSeekable = errors.New("audio: track is not seekable")

// seekTo moves the decoder to frame f at the device rate. It returns
// errNotSeekable when the chain is not seekable (the caller treats that as
// a no-op), or the decoder's error when f is out of range (the engine
// clamps before calling, so this is defence against a race).
func (c *chain) seekTo(f int) error {
	if c == nil || !c.seekable || c.decoder == nil {
		return errNotSeekable
	}
	return c.decoder.Seek(f)
}

// close releases the chain's resources. The mp3 decoder closes the stream
// body, so it is preferred; the body is closed directly only when no decoder
// was built. The pump (advance) and the engine (Stop) can both close the
// same chain; the Once makes the second call a no-op.
func (c *chain) close() {
	if c == nil {
		return
	}
	c.closeOnce.Do(func() {
		if c.decoder != nil {
			_ = c.decoder.Close()
			return
		}
		if c.body != nil {
			_ = c.body.Close()
		}
	})
}
