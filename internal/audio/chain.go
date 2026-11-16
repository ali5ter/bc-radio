package audio

import (
	"context"
	"fmt"
	"io"
	"sync"

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
	if int(format.SampleRate) != deviceRate {
		s = beep.Resample(resampleQuality, format.SampleRate, beep.SampleRate(deviceRate), decoder)
	}
	return &chain{
		track:   t,
		tail:    &volumeStage{v: vol, s: s},
		decoder: decoder,
		body:    stream.Body,
	}, nil
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
