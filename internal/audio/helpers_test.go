package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"bc-radio/internal/contract"
	"github.com/faiface/beep"
)

// fakeDevice is a device that never pulls: tests drive the pump's Read
// manually and assert on the control calls the engine made.
type fakeDevice struct {
	mu    sync.Mutex
	calls []string
}

func (d *fakeDevice) record(call string) {
	d.mu.Lock()
	d.calls = append(d.calls, call)
	d.mu.Unlock()
}

func (d *fakeDevice) play()  { d.record("play") }
func (d *fakeDevice) pause() { d.record("pause") }
func (d *fakeDevice) flush() { d.record("flush") }
func (d *fakeDevice) close() { d.record("close") }

// Calls returns the recorded control calls in order.
func (d *fakeDevice) Calls() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.calls))
	copy(out, d.calls)
	return out
}

var _ device = (*fakeDevice)(nil)

// fakeSource is a contract.AudioSource for tests: Open delegates to openFn
// and records the tracks it was asked for.
type fakeSource struct {
	mu     sync.Mutex
	openFn func(ctx context.Context, t contract.Track) (contract.AudioStream, error)
	opens  []contract.Track
}

// Open implements contract.AudioSource.
func (s *fakeSource) Open(ctx context.Context, t contract.Track) (contract.AudioStream, error) {
	s.mu.Lock()
	s.opens = append(s.opens, t)
	fn := s.openFn
	s.mu.Unlock()
	return fn(ctx, t)
}

// Opened returns the tracks Open was called with, in order.
func (s *fakeSource) Opened() []contract.Track {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]contract.Track, len(s.opens))
	copy(out, s.opens)
	return out
}

var _ contract.AudioSource = (*fakeSource)(nil)

// sourceFromBytes is a fakeSource that serves body for every track.
func sourceFromBytes(body []byte) *fakeSource {
	return &fakeSource{openFn: func(_ context.Context, _ contract.Track) (contract.AudioStream, error) {
		return contract.AudioStream{Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}, nil
	}}
}

// recordedCloser is a stream body that records whether Close was called.
type recordedCloser struct {
	io.Reader
	mu     sync.Mutex
	closed bool
}

// Close implements io.Closer and records the call.
func (r *recordedCloser) Close() error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	return nil
}

// Closed reports whether Close was called.
func (r *recordedCloser) Closed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// scriptStreamer is a scripted beep.Streamer: it returns amp-valued frames
// until its budget is exhausted, then end-of-stream. Err always returns err.
//
// Stream follows the beep convention the production stages (mp3 decoder,
// resampler, volume) all satisfy: the bool is true only when frames were
// delivered, and (0, false) signals end-of-stream.
type scriptStreamer struct {
	total int
	left  int
	amp   float64
	err   error
}

// newScriptStreamer builds a streamer that serves total amp-valued frames.
func newScriptStreamer(total int, amp float64, err error) *scriptStreamer {
	return &scriptStreamer{total: total, left: total, amp: amp, err: err}
}

// Stream implements beep.Streamer.
func (s *scriptStreamer) Stream(buf [][2]float64) (int, bool) {
	if s.left <= 0 {
		return 0, false
	}
	n := len(buf)
	if n > s.left {
		n = s.left
	}
	for i := range buf[:n] {
		buf[i][0], buf[i][1] = s.amp, s.amp
	}
	s.left -= n
	return n, true
}

// Err implements beep.Streamer.
func (s *scriptStreamer) Err() error { return s.err }

var _ beep.Streamer = (*scriptStreamer)(nil)

// testTrack builds a minimal contract.Track for tests.
func testTrack(id string) contract.Track {
	return contract.Track{ID: id, Title: id, BandName: "Test Band", Subdomain: "test.bandcamp.com"}
}

// newTestEngine builds an Engine on a fake device: no real audio is opened
// and the pump is driven manually from the test.
func newTestEngine(t *testing.T, src contract.AudioSource) (*Engine, *fakeDevice) {
	t.Helper()
	dev := &fakeDevice{}
	e, err := New(src, WithDevice(dev))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e, dev
}

// setCurrent replaces the engine's current chain: the test-only shortcut for
// what Load installs, minus the fetch.
func setCurrent(e *Engine, c *chain) {
	e.mu.Lock()
	e.cur = c
	e.next = nil
	e.pos = 0
	e.mu.Unlock()
}

// drainEvents returns all events currently buffered on the engine's channel.
// After Stop the channel is closed and the drain terminates.
func drainEvents(e *Engine) []contract.Event {
	var out []contract.Event
	ch := e.Events()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		default:
			return out
		}
	}
}

// readFixture reads a file from the shared testdata/audio corpus.
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "audio", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// mpegFrameLen returns the byte length of the MPEG-1 Layer III frame at
// off in b, or ok=false if the bytes do not start such a frame. The fixtures
// are 128 kbps CBR 44.1 kHz, so the length is 417 or 418 (the padding bit
// varies) — it is always looked up, never assumed constant.
func mpegFrameLen(b []byte, off int) (int, bool) {
	if len(b)-off < 4 || b[off] != 0xFF {
		return 0, false
	}
	version := (b[off+1] >> 3) & 3 // 3 is MPEG-1
	layer := (b[off+1] >> 1) & 3   // 1 is Layer III
	if version != 3 || layer != 1 {
		return 0, false
	}
	bitrates := [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	sampleRates := [4]int{44100, 48000, 32000, 0}
	br := bitrates[b[off+2]>>4]
	sr := sampleRates[(b[off+2]>>2)&3]
	if br == 0 || sr == 0 {
		return 0, false
	}
	return 144*br*1000/sr + int((b[off+2]>>1)&1), true
}

// cutAtFrames returns the fixture's bytes through the first n complete MPEG
// frames — a clean end-of-stream for the decoder (the fixtures have no ID3
// header, so the file starts on a frame sync).
func cutAtFrames(t *testing.T, name string, n int) []byte {
	t.Helper()
	b := readFixture(t, name)
	off := 0
	for i := 0; i < n; i++ {
		l, ok := mpegFrameLen(b, off)
		if !ok || off+l > len(b) {
			t.Fatalf("%s: no complete frame %d at offset %d", name, i, off)
		}
		off += l
	}
	return b[:off]
}

// decodeFrames converts an encoded device buffer back to frames for
// assertions.
func decodeFrames(t *testing.T, buf []byte) [][2]float64 {
	t.Helper()
	n := len(buf) / bytesPerFrame
	frames := make([][2]float64, n)
	for i := 0; i < n; i++ {
		frames[i][0] = float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[i*bytesPerFrame:])))
		frames[i][1] = float64(math.Float32frombits(binary.LittleEndian.Uint32(buf[i*bytesPerFrame+4:])))
	}
	return frames
}

// checkFrames asserts every frame of got equals want within tol.
func checkFrames(t *testing.T, got, want [][2]float64, tol float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d frames, want %d", len(got), len(want))
	}
	for i := range got {
		if math.Abs(got[i][0]-want[i][0]) > tol || math.Abs(got[i][1]-want[i][1]) > tol {
			t.Fatalf("frame %d = %v, want %v", i, got[i], want[i])
		}
	}
}
