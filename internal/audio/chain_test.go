package audio

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

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

// TestTailFrames: the crossfade start's arithmetic — a wall-clock duration
// to device-rate frames, and -1 when the duration is unknown.
func TestTailFrames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		d    time.Duration
		rate int
		want int
	}{
		{2 * time.Second, 44100, 88200},
		{time.Second, 44100, 44100},
		{500 * time.Millisecond, 44100, 22050},
		{0, 44100, -1},
		{-time.Second, 44100, -1},
		{time.Second, 48000, 48000}, // the rate is the device's, whatever it is
	}
	for _, tc := range tests {
		got := tailFrames(contract.Track{Duration: tc.d}, tc.rate)
		if got != tc.want {
			t.Errorf("tailFrames(%v, %d) = %d, want %d", tc.d, tc.rate, got, tc.want)
		}
	}
}

// TestNewChainTotal: newChain sets the crossfade's total from the track's
// duration.
func TestNewChainTotal(t *testing.T) {
	t.Parallel()
	body := readFixture(t, "actually-you-can-track1.mp3")
	src := sourceFromBytes(body)
	tk := testTrack("t1")
	tk.Duration = 2 * time.Second
	c, err := newChain(context.Background(), src, tk, deviceRate, &volume{g: 1})
	if err != nil {
		t.Fatalf("newChain: %v", err)
	}
	defer c.close()
	if got := c.total; got != 2*deviceRate {
		t.Errorf("chain.total = %d, want %d (2 s at the device rate)", got, 2*deviceRate)
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

// TestNewChainSeekable: a body the decoder can index makes the chain
// seekable with an exact total — the walked frame count, which beats the
// (deliberately wrong) metadata duration for the crossfade clock and the
// seek clamp.
func TestNewChainSeekable(t *testing.T) {
	t.Parallel()
	body := readFixture(t, "actually-you-can-track1.mp3")
	tk := testTrack("t1")
	tk.Duration = 2 * time.Second // wrong on purpose: the walk must win
	c, err := newChain(context.Background(), sourceFromSeekableBytes(body), tk, deviceRate, &volume{g: 1})
	if err != nil {
		t.Fatalf("newChain: %v", err)
	}
	defer c.close()
	if !c.seekable {
		t.Fatal("chain is not seekable for an indexed body")
	}
	if c.total <= 0 || c.total == 2*deviceRate {
		t.Fatalf("chain.total = %d; want the walked count, not the %d of the metadata", c.total, 2*deviceRate)
	}
	if got := c.decoder.Len(); got != c.total {
		t.Fatalf("decoder.Len() = %d, want %d (the walk)", got, c.total)
	}
}

// TestChainSeek: seekTo moves the decoder — the position reads the target,
// and seeking home serves the fresh decode's first frames — and refuses a
// chain it cannot move (a body the decoder could not index, a resampled
// source) with errNotSeekable.
func TestChainSeek(t *testing.T) {
	t.Parallel()
	body := readFixture(t, "actually-you-can-track1.mp3")

	c, err := newChain(context.Background(), sourceFromSeekableBytes(body), testTrack("t1"), deviceRate, &volume{g: 1})
	if err != nil {
		t.Fatalf("newChain: %v", err)
	}
	defer c.close()

	// Baseline: the first frames of a fresh decode of the same body.
	fresh, err := newChain(context.Background(), sourceFromSeekableBytes(body), testTrack("t1"), deviceRate, &volume{g: 1})
	if err != nil {
		t.Fatalf("newChain (baseline): %v", err)
	}
	defer fresh.close()
	buf := make([][2]float64, 64)
	n, ok := fresh.tail.Stream(buf)
	if !ok || n <= 0 {
		t.Fatalf("baseline Stream = %d, %v", n, fresh.tail.Err())
	}
	want := make([][2]float64, n)
	copy(want, buf[:n])

	// Mid-track: the position must read the target frame.
	mid := c.total / 2
	if err := c.seekTo(mid); err != nil {
		t.Fatalf("seekTo(mid): %v", err)
	}
	if got := c.decoder.Position(); got != mid {
		t.Fatalf("Position after seekTo(%d) = %d", mid, got)
	}
	// Home: the decoder must serve the track from its start again.
	if err := c.seekTo(0); err != nil {
		t.Fatalf("seekTo(0): %v", err)
	}
	if got := c.decoder.Position(); got != 0 {
		t.Fatalf("Position after seekTo(0) = %d", got)
	}
	n2, ok2 := c.tail.Stream(buf)
	if !ok2 || n2 != n {
		t.Fatalf("Stream after seek home = %d, %v; want %d frames", n2, c.tail.Err(), n)
	}
	checkFrames(t, buf[:n2], want, 1e-12)

	// A body the decoder could not index is unseekable, and a resampled
	// source is unseekable even when its body was indexed (beep's
	// resampler has no flush: a source seek would leave its stale state).
	plain, err := newChain(context.Background(), sourceFromBytes(body), testTrack("t2"), deviceRate, &volume{g: 1})
	if err != nil {
		t.Fatalf("newChain (plain body): %v", err)
	}
	defer plain.close()
	if plain.seekable {
		t.Fatal("a non-seekable body must not make the chain seekable")
	}
	if err := plain.seekTo(0); !errors.Is(err, errNotSeekable) {
		t.Fatalf("seekTo on a plain body = %v, want errNotSeekable", err)
	}
	resampled, err := newChain(context.Background(), sourceFromSeekableBytes(body), testTrack("t3"), 48000, &volume{g: 1})
	if err != nil {
		t.Fatalf("newChain (resampled): %v", err)
	}
	defer resampled.close()
	if resampled.seekable {
		t.Fatal("a resampled chain must not be seekable")
	}
	if err := resampled.seekTo(0); !errors.Is(err, errNotSeekable) {
		t.Fatalf("seekTo on a resampled chain = %v, want errNotSeekable", err)
	}
}
