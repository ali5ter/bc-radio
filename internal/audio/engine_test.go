package audio

import (
	"bytes"
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/ali5ter/bc-radio/internal/contract"
)

// sameStrings reports whether two string slices are equal.
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestNewStartsDevice: the engine starts its pull loop on construction.
func TestNewStartsDevice(t *testing.T) {
	t.Parallel()
	e, dev := newTestEngine(t, &fakeSource{})
	if got := dev.Calls(); !sameStrings(got, []string{"play"}) {
		t.Errorf("device calls after New = %v, want [play]", got)
	}
	_ = e
}

// TestWithCrossfade: the option stores the duration, and crossFrames
// converts it to device-rate frames — 0 stays 0 (the gapless default).
func TestWithCrossfade(t *testing.T) {
	t.Parallel()
	e, _ := newTestEngine(t, &fakeSource{})
	WithCrossfade(2 * time.Second)(e)
	if e.crossfade != 2*time.Second {
		t.Errorf("crossfade = %v, want 2s", e.crossfade)
	}
	if got := e.crossFrames(); got != 2*deviceRate {
		t.Errorf("crossFrames = %d, want %d", got, 2*deviceRate)
	}
	WithCrossfade(0)(e)
	if got := e.crossFrames(); got != 0 {
		t.Errorf("crossFrames after WithCrossfade(0) = %d, want 0", got)
	}
}

// TestLoadFailure: a failed Load reports EventPlaybackFailed, returns the
// wrapped error, and leaves the current track and the device alone.
func TestLoadFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("upstream exploded")
	src := &fakeSource{openFn: func(context.Context, contract.Track) (contract.AudioStream, error) {
		return contract.AudioStream{}, boom
	}}
	e, dev := newTestEngine(t, src)
	setCurrent(e, &chain{track: testTrack("current"), tail: newScriptStreamer(16, 1, nil)})

	err := e.Load(context.Background(), testTrack("new"))
	if !errors.Is(err, boom) {
		t.Fatalf("Load err = %v, want wrap of %v", err, boom)
	}
	got := drainEvents(e)
	if len(got) != 1 || got[0].Kind != contract.EventPlaybackFailed {
		t.Fatalf("events = %v, want one EventPlaybackFailed", got)
	}
	if !errors.Is(got[0].Err, boom) {
		t.Errorf("event err = %v, want %v", got[0].Err, boom)
	}
	if st := e.State(); st.Track.ID != "current" {
		t.Errorf("State track = %q, want current (a failed Load must not touch it)", st.Track.ID)
	}
	if got := dev.Calls(); !sameStrings(got, []string{"play"}) {
		t.Errorf("device calls = %v, want [play] only (no flush on failure)", got)
	}
}

// TestLoadSuccess: a good Load flushes the device (clearing up to half a
// second of the previous track) and starts the new one.
func TestLoadSuccess(t *testing.T) {
	t.Parallel()
	body := cutAtFrames(t, "actually-you-can-track1.mp3", 16)
	src := sourceFromBytes(body)
	e, dev := newTestEngine(t, src)
	setCurrent(e, &chain{track: testTrack("old"), tail: newScriptStreamer(16, 1, nil)})

	want := testTrack("new")
	if err := e.Load(context.Background(), want); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := dev.Calls(); !sameStrings(got, []string{"play", "flush"}) {
		t.Errorf("device calls = %v, want [play flush]", got)
	}
	st := e.State()
	if st.Track != want {
		t.Errorf("State track = %v, want %v", st.Track, want)
	}
	if !st.Playing {
		t.Error("State.Playing = false after Load, want true")
	}
	if st.Position != 0 {
		t.Errorf("State.Position = %v, want 0 for a fresh track", st.Position)
	}
}

// TestLoadClosesSupersededNext: an explicit Load drops the queued next; a
// preparation that has already installed its chain must be released, not
// leaked.
func TestLoadClosesSupersededNext(t *testing.T) {
	t.Parallel()
	body := cutAtFrames(t, "actually-you-can-track1.mp3", 16)
	src := sourceFromBytes(body)
	e, _ := newTestEngine(t, src)

	rc := &recordedCloser{Reader: bytes.NewReader(body)}
	e.mu.Lock()
	e.cur = &chain{track: testTrack("cur"), tail: newScriptStreamer(16, 1, nil)}
	e.next = &pending{
		chain: &chain{track: testTrack("next"), tail: newScriptStreamer(1, 1, nil), body: rc},
		ready: make(chan struct{}),
	}
	e.mu.Unlock()

	if err := e.Load(context.Background(), testTrack("other")); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !rc.Closed() {
		t.Error("the superseded next chain's body was not closed by Load")
	}
}

// TestQueueWhileIdleLoads: an idle Queue is a Load.
func TestQueueWhileIdleLoads(t *testing.T) {
	t.Parallel()
	body := cutAtFrames(t, "actually-you-can-track1.mp3", 16)
	src := sourceFromBytes(body)
	e, dev := newTestEngine(t, src)

	want := testTrack("t1")
	e.Queue(want)
	st := e.State()
	if st.Track != want {
		t.Errorf("State track = %v, want %v", st.Track, want)
	}
	if !st.Playing {
		t.Error("State.Playing = false after an idle Queue, want true")
	}
	if got := dev.Calls(); !sameStrings(got, []string{"play", "flush"}) {
		t.Errorf("device calls = %v, want [play flush]", got)
	}
}

// TestQueuePreparesNext: a non-idle Queue fetches and decodes the next track
// in the background and lands it in the next slot.
func TestQueuePreparesNext(t *testing.T) {
	t.Parallel()
	body := cutAtFrames(t, "bad-street.mp3", 16)
	src := sourceFromBytes(body)
	e, _ := newTestEngine(t, src)
	setCurrent(e, &chain{track: testTrack("a"), tail: newScriptStreamer(64, 1, nil)})

	want := testTrack("b")
	e.Queue(want)
	e.mu.Lock()
	pd := e.next
	e.mu.Unlock()
	if pd == nil {
		t.Fatal("Queue did not set the next slot")
	}
	<-pd.ready
	if pd.chain == nil {
		t.Fatal("the queued track prepared no chain")
	}
	if pd.chain.track != want {
		t.Errorf("prepared track = %v, want %v", pd.chain.track, want)
	}
	if opens := src.Opened(); len(opens) != 1 || opens[0] != want {
		t.Errorf("opens = %v, want [%v]", opens, want)
	}
}

// TestQueueSupersedeReleasesChain: queuing again replaces the in-flight
// preparation, which must release its own fetched stream when it finishes.
func TestQueueSupersedeReleasesChain(t *testing.T) {
	t.Parallel()
	body := cutAtFrames(t, "actually-you-can-track1.mp3", 16)
	gate := make(chan struct{})
	var (
		mu     sync.Mutex
		bodies = make(map[string]*recordedCloser)
	)
	src := &fakeSource{openFn: func(_ context.Context, tr contract.Track) (contract.AudioStream, error) {
		<-gate // hold the preparation until the test supersedes it
		rc := &recordedCloser{Reader: bytes.NewReader(body)}
		mu.Lock()
		bodies[tr.ID] = rc // keyed by track: the two unblocked Opens race
		mu.Unlock()
		return contract.AudioStream{Body: rc, ContentLength: int64(len(body))}, nil
	}}
	e, _ := newTestEngine(t, src)
	setCurrent(e, &chain{track: testTrack("a"), tail: newScriptStreamer(64, 1, nil)})

	e.Queue(testTrack("t1"))
	e.Queue(testTrack("t2")) // supersedes the first preparation
	close(gate)
	e.wg.Wait()

	mu.Lock()
	if len(bodies) != 2 {
		t.Fatalf("Open called %d times, want 2", len(bodies))
	}
	first, second := bodies["t1"], bodies["t2"]
	mu.Unlock()
	if !first.Closed() {
		t.Error("the superseded preparation's body was not closed")
	}
	if second.Closed() {
		t.Error("the surviving next chain was closed early")
	}
	e.mu.Lock()
	pd := e.next
	e.mu.Unlock()
	if pd == nil || pd.chain == nil {
		t.Error("the surviving next was not installed")
	}
}

// TestPlayPause: the transport toggles the device and the state flag.
func TestPlayPause(t *testing.T) {
	t.Parallel()
	body := cutAtFrames(t, "actually-you-can-track1.mp3", 16)
	src := sourceFromBytes(body)
	e, dev := newTestEngine(t, src)

	if err := e.Load(context.Background(), testTrack("t1")); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := e.Pause(context.Background()); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if e.State().Playing {
		t.Error("State.Playing = true after Pause")
	}
	if err := e.Play(context.Background()); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if !e.State().Playing {
		t.Error("State.Playing = false after Play")
	}
	if got := dev.Calls(); !sameStrings(got, []string{"play", "flush", "pause", "play"}) {
		t.Errorf("device calls = %v, want [play flush pause play]", got)
	}
}

// TestStop: terminal — it pauses the device, closes the current chain,
// closes the event channel, and refuses further operations.
func TestStop(t *testing.T) {
	t.Parallel()
	body := cutAtFrames(t, "actually-you-can-track1.mp3", 16)
	rc := &recordedCloser{Reader: bytes.NewReader(body)}
	src := &fakeSource{openFn: func(_ context.Context, _ contract.Track) (contract.AudioStream, error) {
		return contract.AudioStream{Body: rc, ContentLength: int64(len(body))}, nil
	}}
	e, dev := newTestEngine(t, src)
	if err := e.Load(context.Background(), testTrack("t1")); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := e.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if e.State().Playing {
		t.Error("State.Playing = true after Stop")
	}
	if !rc.Closed() {
		t.Error("the current chain's body was not closed by Stop")
	}
	if got := dev.Calls(); !sameStrings(got, []string{"play", "flush", "pause"}) {
		t.Errorf("device calls = %v, want [play flush pause]", got)
	}
	drainEvents(e) // must terminate: the channel is closed

	if err := e.Stop(context.Background()); err != nil {
		t.Errorf("second Stop = %v, want nil (idempotent)", err)
	}
	if err := e.Load(context.Background(), testTrack("x")); !errors.Is(err, errStopped) {
		t.Errorf("Load after Stop = %v, want errStopped", err)
	}
	if err := e.Play(context.Background()); !errors.Is(err, errStopped) {
		t.Errorf("Play after Stop = %v, want errStopped", err)
	}
	if err := e.Pause(context.Background()); !errors.Is(err, errStopped) {
		t.Errorf("Pause after Stop = %v, want errStopped", err)
	}
	e.Queue(testTrack("y"))
	if opens := src.Opened(); len(opens) != 1 {
		t.Errorf("Queue after Stop caused %d opens, want none more", len(opens)-1)
	}
}

// TestSetVolumeClamps: the gain is clamped to [0,1] and reported in State.
func TestSetVolumeClamps(t *testing.T) {
	t.Parallel()
	e, _ := newTestEngine(t, &fakeSource{})
	tests := []struct {
		in, want float64
	}{
		{2, 1}, {-1, 0}, {0.25, 0.25}, {0, 0}, {1, 1},
	}
	for _, tc := range tests {
		if err := e.SetVolume(context.Background(), tc.in); err != nil {
			t.Fatalf("SetVolume(%v): %v", tc.in, err)
		}
		if got := e.State().Volume; got != tc.want {
			t.Errorf("State.Volume after SetVolume(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestBands: the engine surfaces analyser frames in the contract's shape.
func TestBands(t *testing.T) {
	t.Parallel()
	e, _ := newTestEngine(t, &fakeSource{})
	bands := e.Bands()
	if len(bands) != contract.BandCount {
		t.Fatalf("Bands = %d values, want %d", len(bands), contract.BandCount)
	}
	for k, b := range bands {
		if b < 0 || b > 1 {
			t.Errorf("band %d = %v, want within [0,1]", k, b)
		}
	}
}

// TestScope: the engine surfaces the analyser's most recent audible samples;
// before any audio is produced the slice is empty.
func TestScope(t *testing.T) {
	t.Parallel()
	e, _ := newTestEngine(t, &fakeSource{})
	if s := e.Scope(); s == nil || len(s) != 0 {
		t.Errorf("Scope = %#v before any audio, want an empty non-nil slice", s)
	}
}

// TestLoadResetsAnalyser pins the track-replacement contract end to end:
// once audio has flowed, a Load leaves the analyser blank, so the new
// track's bands and scope build from its own samples.
func TestLoadResetsAnalyser(t *testing.T) {
	t.Parallel()
	body := readFixture(t, "actually-you-can-track1.mp3")
	e, _ := newTestEngine(t, sourceFromBytes(body))
	// The fake device never pulls, so drive the pump: enough of the fixture
	// for the scope to hold a full window.
	if err := e.Load(context.Background(), testTrack("first")); err != nil {
		t.Fatalf("Load: %v", err)
	}
	buf := pumpBuf(22050)
	for i := 0; i < 8; i++ {
		if _, err := e.pump.Read(buf); err != nil {
			t.Fatalf("Read %d: %v", i, err)
		}
	}
	if s := e.Scope(); len(s) != analyserWindow {
		t.Fatalf("Scope = %d samples after driving the pump, want a full window", len(s))
	}
	// Replace the track: the flush clears the device, and the analyser
	// resets with it.
	if err := e.Load(context.Background(), testTrack("second")); err != nil {
		t.Fatalf("Load (replacement): %v", err)
	}
	if s := e.Scope(); len(s) != 0 {
		t.Errorf("Scope = %d samples after the replacement, want empty (the analyser resets)", len(s))
	}
	for k, b := range e.Bands() {
		if b != 0 {
			t.Errorf("band %d = %v after the replacement, want 0", k, b)
		}
	}
}

// TestClose: the clean-shutdown path stops the engine and releases the
// device exactly once.
func TestClose(t *testing.T) {
	t.Parallel()
	e, dev := newTestEngine(t, &fakeSource{})
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got := dev.Calls()
	if len(got) == 0 || got[len(got)-1] != "close" {
		t.Errorf("device calls = %v, want to end in [close]", got)
	}
	drainEvents(e) // must terminate: the channel is closed
}

// closedReady is a pending slot whose preparation is finished.
func closedReady() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

// TestFadeNext: a ready slot becomes current at once (TrackStarted), the
// current track becomes the fade's ghost over the full crossfade window,
// the position zeros, and the device is untouched — the mix is continuous,
// so nothing needs a flush.
func TestFadeNext(t *testing.T) {
	t.Parallel()
	e, dev := newTestEngine(t, &fakeSource{})
	WithCrossfade(50 * time.Millisecond)(e)
	ghost := &chain{track: testTrack("old"), tail: newScriptStreamer(2500, 0.8, nil)}
	setCurrent(e, ghost)
	in := &chain{track: testTrack("next"), tail: newScriptStreamer(2500, 0.4, nil)}
	e.mu.Lock()
	e.playing = true
	e.next = &pending{chain: in, ready: closedReady()}
	e.mu.Unlock()

	if !e.FadeNext(testTrack("next")) {
		t.Fatal("FadeNext = false; the slot is ready and named correctly")
	}
	if st := e.State(); st.Track.ID != "next" {
		t.Fatalf("State track = %q, want next", st.Track.ID)
	}
	if st := e.State(); st.Position != 0 {
		t.Fatalf("State.Position = %v after the swap, want 0", st.Position)
	}
	if e.pump.fade == nil || e.pump.fade.out != ghost || e.pump.fade.in != in {
		t.Fatal("the pump's fade must hold the ghost and the promoted chain")
	}
	if e.pump.fade.total != 50*deviceRate/1000 || e.pump.fade.left != 50*deviceRate/1000 {
		t.Fatalf("fade window = %d/%d; want the full crossfade duration (50 ms = %d frames)",
			e.pump.fade.total, e.pump.fade.left, 50*deviceRate/1000)
	}
	if got := dev.Calls(); !sameStrings(got, []string{"play"}) {
		t.Errorf("device calls = %v, want [play] (a skip-fade must not flush)", got)
	}
	if got := drainEvents(e); len(got) != 1 || got[0].Kind != contract.EventTrackStarted || got[0].Track.ID != "next" {
		t.Fatalf("events = %v, want one TrackStarted(next)", got)
	}

	// Drive the mix: the first frame is the ghost at full gain (x = 1:
	// gainIn 0, gainOut 1), and the ghost is not drained by the window.
	buf := make([]byte, 20*bytesPerFrame)
	if _, err := e.pump.Read(buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
	frames := decodeFrames(t, buf)
	// The 1e-6 tolerance is the float32 round trip through encodeFloat32,
	// not a mix error: the ghost at full gain (x = 1) must be exact to
	// the encoding.
	if math.Abs(frames[0][0]-0.8) > 1e-6 {
		t.Fatalf("first mixed frame = %v; want the ghost at full gain (0.8)", frames[0])
	}
	// The position counts the incoming track: one Read of 20 frames.
	readFrames := 20
	wantPos := time.Duration(float64(readFrames) / float64(deviceRate) * float64(time.Second))
	if st := e.State(); st.Position != wantPos {
		t.Fatalf("Position after the first mixed Read = %v; want %v (20 frames at the device rate)", st.Position, wantPos)
	}
	// Run the window to its end: the ghost departs (cut, not drained) and
	// the incoming keeps playing.
	for e.pump.fade != nil && e.pump.fade.left > 0 {
		if _, err := e.pump.Read(buf); err != nil {
			t.Fatalf("Read: %v", err)
		}
	}
	if e.pump.fade != nil {
		t.Fatal("the fade must complete at the window's end")
	}
	if ghost.tail.(*scriptStreamer).left == 0 {
		t.Fatal("the ghost must be cut at the window's end, not drained")
	}
	evs := drainEvents(e)
	if len(evs) != 1 || evs[0].Kind != contract.EventTrackEnded || evs[0].Track.ID != "old" {
		t.Fatalf("events after the window = %v; want the cut ghost's TrackEnded", evs)
	}
}

// TestFadeNextRefusals: every shape the fade cannot take reports false and
// leaves the engine exactly as it was.
func TestFadeNextRefusals(t *testing.T) {
	t.Parallel()
	build := func() (*Engine, *fakeDevice, *chain) {
		e, dev := newTestEngine(t, &fakeSource{})
		WithCrossfade(time.Second)(e)
		ghost := &chain{track: testTrack("old"), tail: newScriptStreamer(16, 1, nil)}
		setCurrent(e, ghost)
		e.mu.Lock()
		e.playing = true
		e.mu.Unlock()
		return e, dev, ghost
	}
	ready := func() *pending {
		return &pending{chain: &chain{track: testTrack("next"), tail: newScriptStreamer(16, 1, nil)}, ready: closedReady()}
	}

	t.Run("crossfade unset", func(t *testing.T) {
		e, _, _ := build()
		e.mu.Lock()
		e.crossfade = 0
		e.next = ready()
		e.mu.Unlock()
		if e.FadeNext(testTrack("next")) {
			t.Fatal("a zero crossfade must refuse the fade")
		}
	})
	t.Run("nothing next", func(t *testing.T) {
		e, _, _ := build()
		if e.FadeNext(testTrack("next")) {
			t.Fatal("an empty slot must refuse the fade")
		}
	})
	t.Run("next not ready", func(t *testing.T) {
		e, _, _ := build()
		pd := ready()
		pd.ready = make(chan struct{}) // preparation still running
		e.mu.Lock()
		e.next = pd
		e.mu.Unlock()
		if e.FadeNext(testTrack("next")) {
			t.Fatal("an unready slot must refuse the fade")
		}
	})
	t.Run("failed preparation", func(t *testing.T) {
		e, _, _ := build()
		e.mu.Lock()
		e.next = &pending{chain: nil, ready: closedReady()}
		e.mu.Unlock()
		if e.FadeNext(testTrack("next")) {
			t.Fatal("a nil chain must refuse the fade")
		}
	})
	t.Run("different track", func(t *testing.T) {
		e, _, _ := build()
		e.mu.Lock()
		e.next = ready()
		e.mu.Unlock()
		if e.FadeNext(testTrack("someone else")) {
			t.Fatal("a slot holding another track must refuse the fade")
		}
	})
	t.Run("paused", func(t *testing.T) {
		e, _, _ := build()
		e.mu.Lock()
		e.playing = false
		e.next = ready()
		e.mu.Unlock()
		if e.FadeNext(testTrack("next")) {
			t.Fatal("a paused engine must refuse the fade")
		}
	})
	t.Run("fade in flight", func(t *testing.T) {
		e, _, _ := build()
		// The chains carry tails: Stop (in the cleanup) aborts the fade
		// and departs the ghost, which reads the tail's Err.
		e.mu.Lock()
		e.next = ready()
		e.pump.fade = &fade{out: &chain{tail: newScriptStreamer(1, 1, nil)}, in: &chain{tail: newScriptStreamer(1, 1, nil)}, total: 1, left: 1}
		e.mu.Unlock()
		if e.FadeNext(testTrack("next")) {
			t.Fatal("an in-flight fade must refuse a second one")
		}
	})
	// None of them may have moved the track or touched the device.
}

// TestSeek: a seekable chain moves by the requested amount, clamped to the
// track's ends; the device buffer is cleared (flush playing, reset
// paused); the position reads the move.
func TestSeek(t *testing.T) {
	t.Parallel()
	body := readFixture(t, "actually-you-can-track1.mp3")
	e, dev := newTestEngine(t, sourceFromSeekableBytes(body))
	if err := e.Load(context.Background(), testTrack("t1")); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !e.currentChain().seekable {
		t.Fatal("the fixture chain must be seekable (an indexed in-memory body)")
	}
	ctx := context.Background()

	e.Seek(ctx, time.Second)
	if st := e.State(); st.Position != time.Second {
		t.Fatalf("Position after seek +1 s = %v, want 1 s", st.Position)
	}
	if got := dev.Calls(); !sameStrings(got, []string{"play", "flush", "flush"}) {
		t.Fatalf("device calls = %v, want [play flush (load) flush (seek)]", got)
	}

	// Forward past the end clamps to the track's end.
	e.Seek(ctx, 100*time.Second)
	if st := e.State(); st.Position < 4*time.Second || st.Position > 8*time.Second {
		t.Fatalf("Position after a 100 s forward seek = %v; want the clamped end of the ~5 s fixture", st.Position)
	}
	// Backward past the start clamps to zero.
	e.Seek(ctx, -100*time.Second)
	if st := e.State(); st.Position != 0 {
		t.Fatalf("Position after a 100 s backward seek = %v, want 0", st.Position)
	}

	// Paused: the buffer clears with reset, not flush — the pause survives.
	if err := e.Pause(ctx); err != nil {
		t.Fatalf("Pause: %v", err)
	}
	e.Seek(ctx, time.Second)
	if got := dev.Calls(); got[len(got)-1] != "reset" {
		t.Fatalf("last device call = %q, want reset (a paused seek must not resume)", got[len(got)-1])
	}
	if st := e.State(); st.Playing {
		t.Fatal("a seek on a paused track must stay paused")
	}
	if st := e.State(); st.Position != time.Second {
		t.Fatalf("Position after a paused seek = %v, want 1 s", st.Position)
	}
}

// TestSeekNoOps: an idle engine, a zero delta, and a chain the seek cannot
// move all leave the engine — and any in-flight fade — untouched.
func TestSeekNoOps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("idle", func(t *testing.T) {
		e, dev := newTestEngine(t, &fakeSource{})
		e.Seek(ctx, time.Second)
		e.Seek(ctx, 0)
		if got := dev.Calls(); !sameStrings(got, []string{"play"}) {
			t.Fatalf("device calls = %v, want [play] (no seek landed)", got)
		}
	})
	t.Run("unseekable chain keeps the fade", func(t *testing.T) {
		e, dev := newTestEngine(t, &fakeSource{})
		ghost := &chain{track: testTrack("old"), tail: newScriptStreamer(16, 1, nil)}
		in := &chain{track: testTrack("next"), tail: newScriptStreamer(16, 1, nil)}
		setCurrent(e, in)
		e.mu.Lock()
		e.playing = true
		e.pump.fade = &fade{out: ghost, in: in, total: 16, left: 16}
		e.mu.Unlock()
		e.Seek(ctx, time.Second)
		if st := e.State(); st.Position != 0 {
			t.Fatalf("Position = %v; a seek that cannot land must not move it", st.Position)
		}
		if e.pump.fade == nil || e.pump.fade.left != 16 {
			t.Fatal("a seek that cannot land must not abort the in-flight fade")
		}
		if got := dev.Calls(); !sameStrings(got, []string{"play"}) {
			t.Fatalf("device calls = %v, want [play] (no buffer clear for a no-op seek)", got)
		}
	})
}

// TestSeekAbortsFade: a seek that lands while a crossfade is in flight
// ends the mix — the ghost departs (cut) — and the position moves.
func TestSeekAbortsFade(t *testing.T) {
	t.Parallel()
	body := readFixture(t, "actually-you-can-track1.mp3")
	e, dev := newTestEngine(t, sourceFromSeekableBytes(body))
	if err := e.Load(context.Background(), testTrack("t1")); err != nil {
		t.Fatalf("Load: %v", err)
	}
	in := e.currentChain()
	ghost := &chain{track: testTrack("ghost"), tail: newScriptStreamer(16, 1, nil)}
	e.mu.Lock()
	e.pump.fade = &fade{out: ghost, in: in, total: 16, left: 16}
	e.mu.Unlock()

	e.Seek(context.Background(), time.Second)
	if e.pump.fade != nil {
		t.Fatal("a landed seek must abort the in-flight fade")
	}
	if st := e.State(); st.Position != time.Second {
		t.Fatalf("Position = %v after the seek, want 1 s", st.Position)
	}
	if got := drainEvents(e); len(got) == 0 || got[0].Kind != contract.EventTrackEnded || got[0].Track.ID != "ghost" {
		t.Fatalf("events = %v, want the cut ghost's TrackEnded", got)
	}
	if got := dev.Calls(); got[len(got)-1] != "flush" {
		t.Fatalf("last device call = %q, want flush", got[len(got)-1])
	}
}
