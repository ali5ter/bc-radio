package audio

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"

	"bc-radio/internal/contract"
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
