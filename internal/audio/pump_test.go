package audio

import (
	"errors"
	"testing"
	"time"

	"bc-radio/internal/contract"
)

// pumpBuf returns a device-sized buffer of nframes frames.
func pumpBuf(nframes int) []byte {
	return make([]byte, nframes*bytesPerFrame)
}

// expectEvents asserts the engine's event channel holds exactly the given
// kinds, in order.
func expectEvents(t *testing.T, e *Engine, want ...contract.EventKind) []contract.Event {
	t.Helper()
	got := drainEvents(e)
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i].Kind != w {
			t.Errorf("event %d kind = %v, want %v (all: %v)", i, got[i].Kind, w, got)
		}
	}
	return got
}

// TestPumpIdleSilence: with no current track the pump fills the buffer with
// silence, every time.
func TestPumpIdleSilence(t *testing.T) {
	t.Parallel()
	e, _ := newTestEngine(t, &fakeSource{})
	buf := pumpBuf(1024)
	for i := 0; i < 2; i++ {
		n, err := e.pump.Read(buf)
		if err != nil {
			t.Fatalf("Read: %v (the pump must never return an error)", err)
		}
		if n != len(buf) {
			t.Fatalf("Read = %d, want %d (always a full buffer)", n, len(buf))
		}
		for j, b := range buf {
			if b != 0 {
				t.Fatalf("byte %d = %d, want 0 (idle must be silence)", j, b)
			}
		}
	}
}

// TestPumpGaplessTransition: track A drains into the prepared track B within
// the same pull — no silence between them, the position resets on the new
// track, and the events come out in playback order.
func TestPumpGaplessTransition(t *testing.T) {
	t.Parallel()
	e, _ := newTestEngine(t, &fakeSource{})
	a := &chain{track: testTrack("a"), tail: newScriptStreamer(2048, 1, nil)}
	b := &chain{track: testTrack("b"), tail: newScriptStreamer(2048, 0.5, nil)}
	ready := make(chan struct{})
	e.mu.Lock()
	e.cur = a
	e.next = &pending{chain: b, ready: ready}
	e.mu.Unlock()
	close(ready)

	buf := pumpBuf(1024)
	want := append(constantFrames(2048, 1), constantFrames(2048, 0.5)...)
	gotAll := make([][2]float64, 0, len(want))
	for i := 0; i < 4; i++ {
		if _, err := e.pump.Read(buf); err != nil {
			t.Fatalf("Read %d: %v", i, err)
		}
		gotAll = append(gotAll, decodeFrames(t, buf)...)
	}
	checkFrames(t, gotAll, want, 1e-6)
	// The next Read is idle silence: the transition left no ghost audio.
	if _, err := e.pump.Read(buf); err != nil {
		t.Fatalf("Read 5: %v", err)
	}
	checkFrames(t, decodeFrames(t, buf), constantFrames(1024, 0), 1e-12)

	expectEvents(t, e,
		contract.EventTrackStarted, // a
		contract.EventTrackEnded,   // a
		contract.EventTrackStarted, // b
		contract.EventTrackEnded)   // b
	if got, wantPos := e.State().Position, time.Duration(2048)*time.Second/deviceRate; mathAbsDur(got-wantPos) > time.Millisecond {
		t.Errorf("Position = %v, want %v (2048 frames of b)", got, wantPos)
	}
}

// mathAbsDur returns the absolute value of a duration.
func mathAbsDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// TestPumpStaleTailSilenced: the frame scratch is reused between Reads, so
// after a track drains the pump must silence whatever the previous Read left
// in it — the earlier bug replayed the last track's audio while idle.
func TestPumpStaleTailSilenced(t *testing.T) {
	t.Parallel()
	e, _ := newTestEngine(t, &fakeSource{})
	setCurrent(e, &chain{track: testTrack("a"), tail: newScriptStreamer(1024, 1, nil)})
	buf := pumpBuf(1024)
	if _, err := e.pump.Read(buf); err != nil {
		t.Fatalf("Read 1: %v", err)
	}
	checkFrames(t, decodeFrames(t, buf), constantFrames(1024, 1), 1e-6)
	if _, err := e.pump.Read(buf); err != nil {
		t.Fatalf("Read 2: %v", err)
	}
	checkFrames(t, decodeFrames(t, buf), constantFrames(1024, 0), 1e-12)
	expectEvents(t, e, contract.EventTrackStarted, contract.EventTrackEnded)
}

// TestPumpDrainSilencesAndEnds: a track that drains mid-buffer ends the rest
// of that buffer in silence and reports TrackEnded.
func TestPumpDrainSilencesAndEnds(t *testing.T) {
	t.Parallel()
	e, _ := newTestEngine(t, &fakeSource{})
	setCurrent(e, &chain{track: testTrack("a"), tail: newScriptStreamer(512, 1, nil)})
	buf := pumpBuf(1024)
	if _, err := e.pump.Read(buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
	frames := decodeFrames(t, buf)
	checkFrames(t, frames[:512], constantFrames(512, 1), 1e-6)
	checkFrames(t, frames[512:], constantFrames(512, 0), 1e-12)
	expectEvents(t, e, contract.EventTrackStarted, contract.EventTrackEnded)
}

// TestPumpDrainErrorFails: a chain that dies mid-stream (a dead stream URL,
// a decode failure) reports EventPlaybackFailed, not TrackEnded.
func TestPumpDrainErrorFails(t *testing.T) {
	t.Parallel()
	e, _ := newTestEngine(t, &fakeSource{})
	boom := errors.New("stream died")
	setCurrent(e, &chain{track: testTrack("a"), tail: newScriptStreamer(512, 1, boom)})
	buf := pumpBuf(1024)
	if _, err := e.pump.Read(buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
	got := expectEvents(t, e, contract.EventTrackStarted, contract.EventPlaybackFailed)
	if !errors.Is(got[1].Err, boom) {
		t.Errorf("failed event err = %v, want %v", got[1].Err, boom)
	}
}

// TestPumpCutEmitsEnded: a track cut by an explicit Load (replaced mid-
// stream, no error) reports TrackEnded — it is no longer playing.
func TestPumpCutEmitsEnded(t *testing.T) {
	t.Parallel()
	e, _ := newTestEngine(t, &fakeSource{})
	a := &chain{track: testTrack("a"), tail: newScriptStreamer(4096, 1, nil)}
	b := &chain{track: testTrack("b"), tail: newScriptStreamer(4096, 0.5, nil)}
	setCurrent(e, a)
	buf := pumpBuf(1024)
	if _, err := e.pump.Read(buf); err != nil {
		t.Fatalf("Read 1: %v", err)
	}
	// Simulate Load installing b over a mid-stream.
	e.mu.Lock()
	e.cur = b
	e.next = nil
	e.pos = 0
	e.mu.Unlock()
	if _, err := e.pump.Read(buf); err != nil {
		t.Fatalf("Read 2: %v", err)
	}
	checkFrames(t, decodeFrames(t, buf), constantFrames(1024, 0.5), 1e-6)
	expectEvents(t, e,
		contract.EventTrackStarted, // a
		contract.EventTrackEnded,   // a (cut cleanly)
		contract.EventTrackStarted) // b
}

// TestPumpDrainWaitsForUnreadyNext: if the next track is not prepared yet
// when the current one drains, the pump idles (silence) until the
// preparation lands, then starts the next.
func TestPumpDrainWaitsForUnreadyNext(t *testing.T) {
	t.Parallel()
	e, _ := newTestEngine(t, &fakeSource{})
	a := &chain{track: testTrack("a"), tail: newScriptStreamer(512, 1, nil)}
	b := &chain{track: testTrack("b"), tail: newScriptStreamer(2048, 0.5, nil)}
	ready := make(chan struct{})
	e.mu.Lock()
	e.cur = a
	e.next = &pending{chain: b, ready: ready}
	e.mu.Unlock()

	buf := pumpBuf(1024)
	if _, err := e.pump.Read(buf); err != nil {
		t.Fatalf("Read 1: %v", err)
	}
	frames := decodeFrames(t, buf)
	checkFrames(t, frames[:512], constantFrames(512, 1), 1e-6)
	checkFrames(t, frames[512:], constantFrames(512, 0), 1e-12) // the wait is audible silence
	expectEvents(t, e, contract.EventTrackStarted, contract.EventTrackEnded)

	close(ready)
	if _, err := e.pump.Read(buf); err != nil {
		t.Fatalf("Read 2: %v", err)
	}
	checkFrames(t, decodeFrames(t, buf), constantFrames(1024, 0.5), 1e-6)
	// The TrackStarted/TrackEnded for a were drained by the expectEvents
	// call above; only the new TrackStarted for b is on the channel.
	expectEvents(t, e, contract.EventTrackStarted)
}
