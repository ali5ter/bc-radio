package audio

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/ali5ter/bc-radio/internal/contract"
)

// TestNewChainDecodesFixture drives the real pipeline — MP3 decode at the
// device rate, no resampler — against the captured Bandcamp corpus.
func TestNewChainDecodesFixture(t *testing.T) {
	t.Parallel()
	body := readFixture(t, "actually-you-can-track1.mp3")
	src := sourceFromBytes(body)
	c, err := newChain(context.Background(), src, testTrack("t1"), deviceRate, &volume{g: 1})
	if err != nil {
		t.Fatalf("newChain: %v", err)
	}
	defer c.close()
	buf := make([][2]float64, 1024)
	n, ok := c.tail.Stream(buf)
	if !ok || n <= 0 {
		t.Fatalf("Stream = %d, %v; want frames of audio", n, c.tail.Err())
	}
	for i := 0; i < n; i++ {
		if math.Abs(buf[i][0]) > 1 || math.Abs(buf[i][1]) > 1 {
			t.Fatalf("frame %d = %v out of range at gain 1", i, buf[i])
		}
	}
	if err := c.tail.Err(); err != nil {
		t.Errorf("mid-stream Err = %v, want nil", err)
	}
}

// TestNewChainResamplesWhenRatesDiffer: the fixture is 44.1 kHz, so a 48 kHz
// device rate must take the resampler branch and still produce audio.
func TestNewChainResamplesWhenRatesDiffer(t *testing.T) {
	t.Parallel()
	body := readFixture(t, "bad-street.mp3")
	src := sourceFromBytes(body)
	c, err := newChain(context.Background(), src, testTrack("t2"), 48000, &volume{g: 1})
	if err != nil {
		t.Fatalf("newChain: %v", err)
	}
	defer c.close()
	buf := make([][2]float64, 1024)
	n, ok := c.tail.Stream(buf)
	if !ok || n <= 0 {
		t.Fatalf("Stream = %d, %v; want resampled frames", n, c.tail.Err())
	}
	for i := 0; i < n; i++ {
		if math.Abs(buf[i][0]) > 1 || math.Abs(buf[i][1]) > 1 {
			t.Fatalf("frame %d = %v out of range", i, buf[i])
		}
	}
}

// TestNewChainOpenFailure: a source error surfaces, wrapped.
func TestNewChainOpenFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	src := &fakeSource{openFn: func(context.Context, contract.Track) (contract.AudioStream, error) {
		return contract.AudioStream{}, boom
	}}
	if _, err := newChain(context.Background(), src, testTrack("t3"), deviceRate, &volume{}); !errors.Is(err, boom) {
		t.Errorf("newChain err = %v, want wrap of %v", err, boom)
	}
}

// TestNewChainDecodeFailureClosesBody: a body that is not MP3 fails the
// decode and the opened stream must be closed.
func TestNewChainDecodeFailureClosesBody(t *testing.T) {
	t.Parallel()
	r := &recordedCloser{Reader: strings.NewReader("not an mp3 at all")}
	src := &fakeSource{openFn: func(_ context.Context, _ contract.Track) (contract.AudioStream, error) {
		return contract.AudioStream{Body: r, ContentLength: -1}, nil
	}}
	if _, err := newChain(context.Background(), src, testTrack("t4"), deviceRate, &volume{}); err == nil {
		t.Fatal("newChain: want an error for a non-MP3 body")
	}
	if !r.Closed() {
		t.Error("newChain did not close the body after a decode failure")
	}
}

// TestVolumeStageAppliesSharedGain: the gain is shared, so a change affects
// every stage built on it, and applies to every frame it serves.
func TestVolumeStageAppliesSharedGain(t *testing.T) {
	t.Parallel()
	vol := &volume{g: 1}
	up := newScriptStreamer(16, 0.5, nil)
	s := &volumeStage{v: vol, s: up}
	buf := make([][2]float64, 16)
	if n, _ := s.Stream(buf); n != 16 {
		t.Fatalf("Stream = %d, want 16", n)
	}
	checkFrames(t, buf, constantFrames(16, 0.5), 1e-12)
	vol.set(0.25)
	up2 := newScriptStreamer(16, 0.5, nil)
	s2 := &volumeStage{v: vol, s: up2}
	if n, _ := s2.Stream(buf); n != 16 {
		t.Fatalf("Stream = %d, want 16", n)
	}
	checkFrames(t, buf, constantFrames(16, 0.125), 1e-12)
}

// constantFrames builds n frames with both channels set to v.
func constantFrames(n int, v float64) [][2]float64 {
	frames := make([][2]float64, n)
	for i := range frames {
		frames[i][0], frames[i][1] = v, v
	}
	return frames
}

// TestChainCloseIsIdempotent: the pump and the engine can both close a
// chain; the second call must be a no-op.
func TestChainCloseIsIdempotent(t *testing.T) {
	t.Parallel()
	body := readFixture(t, "bad-street.mp3")
	src := sourceFromBytes(body)
	c, err := newChain(context.Background(), src, testTrack("t5"), deviceRate, &volume{g: 1})
	if err != nil {
		t.Fatalf("newChain: %v", err)
	}
	c.close()
	c.close() // must not panic or double-close
}

// TestChainCloseNil: a nil chain is a legal no-op (a failed preparation).
func TestChainCloseNil(t *testing.T) {
	t.Parallel()
	var c *chain
	c.close() // must not panic
}
