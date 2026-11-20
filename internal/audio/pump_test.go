package audio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"testing"
	"time"

	"github.com/ali5ter/bc-radio/internal/contract"
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

// scriptedChain builds a chain whose tail serves frames amp-valued and whose
// metadata total is total frames — the two can differ (a duration estimate
// that does not match the real track), which is exactly what the crossfade
// arithmetic must tolerate.
func scriptedChain(id string, total, frames int, amp float64) *chain {
	return &chain{track: testTrack(id), tail: newScriptStreamer(frames, amp, nil), total: total}
}

// wantCrossfadeFrames computes the expected device output for two constant
// tracks joined by a crossfade. The pump checks the fade start at the top of
// each pull, so the window opens at the first pull boundary at or after the
// metadata edge — a little late, never early — and its length is the
// outgoing track's remaining length at that moment. Layout in output frames:
// pure a up to the window, the equal-power mix across it (the outgoing at
// sin(π/2·x) falling 1→0, the incoming at cos(π/2·x) rising 0→1, x the
// window's remaining fraction, the sum clamped to [-1,1]), then pure b at
// full gain, then silence. Either side can run short of the window (a
// duration estimate that does not match the real track): the ghost's side
// goes quiet where its real audio ends, and where the incoming ends the mix
// aborts with it — silence, no curve. pull is the test's read size; aReal
// must be at or past the metadata edge (or the transition would be the
// plain gapless one, which this helper does not model).
func wantCrossfadeFrames(n, aMeta, aReal, bLen, fadeLen, pull int, aAmp, bAmp float64) [][2]float64 {
	fadeStart := (aMeta - fadeLen + pull - 1) / pull * pull
	if fadeStart < 0 {
		fadeStart = 0
	}
	fadeLenEff := aMeta - fadeStart
	// Each side's real content once the window opens; both can be short.
	ghostCut := aReal - fadeStart
	if ghostCut < 0 {
		ghostCut = 0
	}
	if ghostCut > fadeLenEff {
		ghostCut = fadeLenEff
	}
	inCut := bLen
	if inCut > fadeLenEff {
		inCut = fadeLenEff
	}
	if inCut < 0 {
		inCut = 0
	}
	// The incoming survives the window only if it outlasts it; then it
	// keeps playing at full gain for the rest of its real length.
	pureBEnd := 0
	if inCut >= fadeLenEff && bLen > fadeLenEff {
		pureBEnd = fadeStart + bLen
	}
	want := make([][2]float64, n)
	for g := range want {
		var v float64
		switch {
		case g < fadeStart:
			v = aAmp
		case g < fadeStart+fadeLenEff:
			k := g - fadeStart
			x := float64(fadeLenEff-k) / float64(fadeLenEff)
			if k < inCut {
				v = bAmp * math.Cos(x*math.Pi/2)
				if k < ghostCut {
					v += aAmp * math.Sin(x*math.Pi/2)
				}
				v = clampMix(v)
			}
			// k ≥ inCut: the incoming ended mid-fade; the mix is aborted
			// and the ghost departs with it — silence, no curve.
		case g < pureBEnd:
			v = bAmp // the window is spent: full incoming gain
		}
		want[g][0], want[g][1] = v, v
	}
	return want
}

// TestPumpCrossfadeMix: a crossfade set, the current track reaches its
// final window, and the prepared next is ready — the pump mixes the two on
// an equal-power curve, continuous at both ends, and the events and the
// position track the incoming track from the fade's start.
func TestPumpCrossfadeMix(t *testing.T) {
	t.Parallel()
	e, _ := newTestEngine(t, &fakeSource{})
	e.crossfade = time.Second // 44100 frames at the device rate

	const aLen, fadeLen, bLen = 55900, 44100, 50000
	const pull = 1024
	a := scriptedChain("a", aLen, aLen, 1)
	b := scriptedChain("b", bLen, bLen, 0.5)
	ready := make(chan struct{})
	e.mu.Lock()
	e.cur = a
	e.next = &pending{chain: b, ready: ready}
	e.playing = true
	e.mu.Unlock()
	close(ready)

	// Both tracks are exact, so the audio runs from the start to the
	// incoming's full length after the window; one extra pull lets the
	// final drain report.
	audioEnd := (aLen-fadeLen+pull-1)/pull*pull + bLen
	buf := pumpBuf(pull)
	gotAll := make([][2]float64, 0, (audioEnd+pull-1)/pull*pull)
	for i := 0; i < (audioEnd+pull-1)/pull; i++ {
		if _, err := e.pump.Read(buf); err != nil {
			t.Fatalf("Read %d: %v", i, err)
		}
		gotAll = append(gotAll, decodeFrames(t, buf)...)
	}
	wantAll := wantCrossfadeFrames(len(gotAll), aLen, aLen, bLen, fadeLen, pull, 1, 0.5)
	checkFrames(t, gotAll, wantAll, 1e-5)

	// Continuity at both ends of the window — the click detector. Any
	// gain step would move a frame far beyond the curve's own slope.
	fadeStart := (aLen - fadeLen + pull - 1) / pull * pull
	for _, g := range []int{fadeStart - 1, aLen - 1} {
		if d := math.Abs(gotAll[g][0] - gotAll[g+1][0]); d > 2e-3 {
			t.Errorf("frame %d→%d jumps %v; the transition must be click-free", g, g+1, d)
		}
	}
	// Mid-window the unclamped sum exceeds full scale
	// (1·sin(π/4) + 0.5·cos(π/4) ≈ 1.06): the clamp holds the output at 1.
	mid := fadeStart + (aLen-fadeStart)/2
	if math.Abs(gotAll[mid][0]) > 1+1e-5 || math.Abs(gotAll[mid][0]-1) > 1e-5 {
		t.Errorf("mid-fade frame = %v, want 1 (the clamped equal-power peak)", gotAll[mid][0])
	}

	expectEvents(t, e,
		contract.EventTrackStarted, // a
		contract.EventTrackStarted, // b, at the fade's start
		contract.EventTrackEnded,   // a, when the window spends
		contract.EventTrackEnded)   // b
	if st := e.State(); st.Track.ID != "" || st.Playing {
		t.Errorf("final state = %+v; want idle (both tracks drained, nothing queued)", st)
	}
}

// TestPumpCrossfadeGhostDrainsEarly: the metadata duration over-estimates
// the real track, so the ghost runs dry inside the window — its side of the
// mix goes quiet and the incoming keeps rising to full on the curve. The
// one drop at the ghost's real end is the content ending (a track that
// stops), not a gain step: its size is exactly the ghost's curve value
// there, and the curve itself stays smooth.
func TestPumpCrossfadeGhostDrainsEarly(t *testing.T) {
	t.Parallel()
	e, _ := newTestEngine(t, &fakeSource{})
	e.crossfade = time.Second // 44100 frames

	const aMeta, aReal, fadeLen, bLen = 55900, 40000, 44100, 50000
	const pull = 1024
	a := scriptedChain("a", aMeta, aReal, 1)
	b := scriptedChain("b", bLen, bLen, 0.5)
	ready := make(chan struct{})
	e.mu.Lock()
	e.cur = a
	e.next = &pending{chain: b, ready: ready}
	e.playing = true
	e.mu.Unlock()
	close(ready)

	audioEnd := (aMeta-fadeLen+pull-1)/pull*pull + bLen
	buf := pumpBuf(pull)
	gotAll := make([][2]float64, 0, (audioEnd+pull-1)/pull*pull)
	for i := 0; i < (audioEnd+pull-1)/pull; i++ {
		if _, err := e.pump.Read(buf); err != nil {
			t.Fatalf("Read %d: %v", i, err)
		}
		gotAll = append(gotAll, decodeFrames(t, buf)...)
	}
	// The window is keyed to the metadata; the ghost's real audio ends
	// inside it, the incoming is exact.
	wantAll := wantCrossfadeFrames(len(gotAll), aMeta, aReal, bLen, fadeLen, pull, 1, 0.5)
	checkFrames(t, gotAll, wantAll, 1e-5)

	// The drop where the ghost's content ends: exactly the ghost's curve
	// value (its share of the mix), no more — a gain step would add to it.
	fadeStart := (aMeta - fadeLen + pull - 1) / pull * pull
	fadeLenEff := aMeta - fadeStart
	ghostCut := aReal - fadeStart
	lastGhost := fadeStart + ghostCut - 1 // the ghost's final frame
	x := float64(fadeLenEff-(ghostCut-1)) / float64(fadeLenEff)
	wantDrop := math.Sin(x * math.Pi / 2) // the ghost's share of the last frame
	if d := gotAll[lastGhost][0] - gotAll[lastGhost+1][0]; d < wantDrop-1e-3 || d > wantDrop+1e-3 {
		t.Errorf("drop at the ghost's real end = %v, want ≈ %v (its curve value, no gain step)", d, wantDrop)
	}
	// The window's tail is the incoming alone, at its curve value.
	tail := fadeStart + fadeLenEff - 1
	xT := float64(1) / float64(fadeLenEff)
	if v := gotAll[tail][0]; math.Abs(v-0.5*math.Cos(xT*math.Pi/2)) > 1e-3 {
		t.Errorf("window tail = %v, want %v (the incoming's curve value at the tail)", v, 0.5*math.Cos(xT*math.Pi/2))
	}
	expectEvents(t, e,
		contract.EventTrackStarted,
		contract.EventTrackStarted,
		contract.EventTrackEnded,
		contract.EventTrackEnded)
}

// TestPumpCrossfadeIncomingEndsMidFade: the incoming track is shorter than
// the fade window — it ends before the fade completes, so the ghost departs
// with it (a cut, reported as an end), the mix stops, and the engine idles.
func TestPumpCrossfadeIncomingEndsMidFade(t *testing.T) {
	t.Parallel()
	e, _ := newTestEngine(t, &fakeSource{})
	e.crossfade = time.Second // 44100 frames

	const aLen, fadeLen, bLen = 55900, 44100, 10000
	const pull = 1024
	a := scriptedChain("a", aLen, aLen, 1)
	b := scriptedChain("b", bLen, bLen, 0.5)
	ready := make(chan struct{})
	e.mu.Lock()
	e.cur = a
	e.next = &pending{chain: b, ready: ready}
	e.playing = true
	e.mu.Unlock()
	close(ready)

	// The incoming ends inside the window: the audio runs until it does.
	fadeStart := (aLen - fadeLen + pull - 1) / pull * pull
	audioEnd := fadeStart + bLen
	buf := pumpBuf(pull)
	gotAll := make([][2]float64, 0, (audioEnd+pull-1)/pull*pull)
	for i := 0; i < (audioEnd+pull-1)/pull; i++ {
		if _, err := e.pump.Read(buf); err != nil {
			t.Fatalf("Read %d: %v", i, err)
		}
		gotAll = append(gotAll, decodeFrames(t, buf)...)
	}
	// The incoming ends inside the window (inCut < the window), so the
	// helper models the aborted mix — silence from its end, no pure-b tail.
	wantAll := wantCrossfadeFrames(len(gotAll), aLen, aLen, bLen, fadeLen, pull, 1, 0.5)
	checkFrames(t, gotAll, wantAll, 1e-5)

	expectEvents(t, e,
		contract.EventTrackStarted, // a
		contract.EventTrackStarted, // b, at the fade's start
		contract.EventTrackEnded,   // a, cut when b ends mid-fade
		contract.EventTrackEnded)   // b
	if st := e.State(); st.Track.ID != "" || st.Playing {
		t.Errorf("final state = %+v; want idle", st)
	}
	// The ghost is gone: further Reads are silence.
	if _, err := e.pump.Read(buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
	checkFrames(t, decodeFrames(t, buf), constantFrames(pull, 0), 1e-12)
}

// TestPumpCrossfadeAbortOnLoad: a Load landing mid-fade kills the mix — the
// ghost and the incoming both depart (clean ends), the loaded track plays
// alone, and nothing of the mix leaks into it.
func TestPumpCrossfadeAbortOnLoad(t *testing.T) {
	t.Parallel()
	e, _ := newTestEngine(t, &fakeSource{})
	e.crossfade = time.Second // 44100 frames

	const aLen, fadeLen = 55900, 44100
	a := scriptedChain("a", aLen, aLen, 1)
	b := scriptedChain("b", 50000, 50000, 0.5)
	c := scriptedChain("c", 20000, 20000, 0.25)
	ready := make(chan struct{})
	e.mu.Lock()
	e.cur = a
	e.next = &pending{chain: b, ready: ready}
	e.playing = true
	e.mu.Unlock()
	close(ready)

	buf := pumpBuf(aLen - fadeLen) // 11800 frames: lands exactly on the window
	if _, err := e.pump.Read(buf); err != nil {
		t.Fatalf("Read 1: %v", err)
	}
	checkFrames(t, decodeFrames(t, buf), constantFrames(aLen-fadeLen, 1), 1e-6)
	if _, err := e.pump.Read(buf); err != nil {
		t.Fatalf("Read 2: %v", err)
	}
	// The fade is in flight now (the window opened at Read 2's first
	// frame). A Load replaces the incoming chain: the next Read must play
	// only c.
	e.mu.Lock()
	e.cur = c
	e.next = nil
	e.pos = 0
	e.playing = true
	e.mu.Unlock()
	if _, err := e.pump.Read(buf); err != nil {
		t.Fatalf("Read 3: %v", err)
	}
	checkFrames(t, decodeFrames(t, buf), constantFrames(aLen-fadeLen, 0.25), 1e-6)

	expectEvents(t, e,
		contract.EventTrackStarted, // a
		contract.EventTrackStarted, // b, the fade's start
		contract.EventTrackEnded,   // a, the ghost cut by the abort
		contract.EventTrackEnded,   // b, cut by the Load
		contract.EventTrackStarted) // c
	if st := e.State(); st.Track.ID != "c" || !st.Playing {
		t.Errorf("state = %+v; want c playing", st)
	}
}

// TestPumpCrossfadeRealFixtures drives the whole real path — two captured
// MP3s decoded, volume-applied and mixed — through the pump, and asserts
// what the scripted tests take on faith: the mix never exceeds full scale
// (the clamp), no frame-to-frame click at the transition, and the fade's
// event order (the incoming starts before the outgoing ends).
func TestPumpCrossfadeRealFixtures(t *testing.T) {
	t.Parallel()
	aBody := cutAtFrames(t, "actually-you-can-track1.mp3", 120) // ~3.1 s
	bBody := cutAtFrames(t, "bad-street.mp3", 120)
	src := &fakeSource{openFn: func(_ context.Context, tr contract.Track) (contract.AudioStream, error) {
		body := aBody
		if tr.ID == "b" {
			body = bBody
		}
		return contract.AudioStream{Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}, nil
	}}
	e, _ := newTestEngine(t, src)
	e.crossfade = time.Second

	at := testTrack("a")
	at.Duration = 3 * time.Second // the metadata; the cut runs a little long
	bt := testTrack("b")
	bt.Duration = 4 * time.Second
	if err := e.Load(context.Background(), at); err != nil {
		t.Fatalf("Load a: %v", err)
	}
	e.Queue(bt)
	e.mu.Lock()
	pd := e.next
	e.mu.Unlock()
	<-pd.ready
	if pd.chain == nil {
		t.Fatal("the queued track prepared no chain")
	}

	buf := pumpBuf(4410) // 0.1 s
	var prev [2]float64
	var maxAbs, maxJump float64
	for i := 0; i < 80; i++ { // 8 s: both tracks and then idle
		if _, err := e.pump.Read(buf); err != nil {
			t.Fatalf("Read %d: %v", i, err)
		}
		for _, f := range decodeFrames(t, buf) {
			if av := math.Max(math.Abs(f[0]), math.Abs(f[1])); av > maxAbs {
				maxAbs = av
			}
			if d := math.Max(math.Abs(f[0]-prev[0]), math.Abs(f[1]-prev[1])); d > maxJump {
				maxJump = d
			}
			prev = f
		}
	}
	if maxAbs > 1+1e-6 {
		t.Errorf("max |sample| = %v; the mix must stay within full scale", maxAbs)
	}
	if maxJump > 0.5 {
		t.Errorf("max frame-to-frame jump = %v; the transition must not click", maxJump)
	}
	got := drainEvents(e)
	if len(got) != 4 {
		t.Fatalf("events = %v; want four", got)
	}
	// The fade's order, by track: a starts, b starts (the fade), a ends
	// (the window), b ends — gapless would be a-start, a-end, b-start.
	wantIDs := []string{"a", "b", "a", "b"}
	for i, id := range wantIDs {
		if got[i].Track.ID != id {
			t.Errorf("event %d track = %q, want %q (kinds %v)", i, got[i].Track.ID, id, got)
		}
	}
}

// TestPumpNoCrossfadeWhenTotalUnknown: a track whose duration the metadata
// does not know (total -1) cannot time a fade — it transitions the old
// gapless way even with a crossfade set.
func TestPumpNoCrossfadeWhenTotalUnknown(t *testing.T) {
	t.Parallel()
	e, _ := newTestEngine(t, &fakeSource{})
	e.crossfade = time.Second
	a := scriptedChain("a", -1, 2048, 1)
	b := scriptedChain("b", 2048, 2048, 0.5)
	ready := make(chan struct{})
	e.mu.Lock()
	e.cur = a
	e.next = &pending{chain: b, ready: ready}
	e.mu.Unlock()
	close(ready)

	buf := pumpBuf(1024)
	var gotAll [][2]float64
	// A drain is reported on the pull *after* the last frame (the streamer
	// returns its final frames with ok=true), so one extra read is needed
	// for the final TrackEnded.
	for i := 0; i < 5; i++ {
		if _, err := e.pump.Read(buf); err != nil {
			t.Fatalf("Read %d: %v", i, err)
		}
		gotAll = append(gotAll, decodeFrames(t, buf)...)
	}
	// The reads continue past the audio: a and b are exact 2048 each, and
	// the silence that follows fills the buffers — all of it asserted.
	want := append(constantFrames(2048, 1), constantFrames(2048, 0.5)...)
	want = append(want, constantFrames(len(gotAll)-len(want), 0)...)
	checkFrames(t, gotAll, want, 1e-6)
	// No fade: the TrackStarted for b follows the TrackEnded for a — the
	// gapless order, not the fade's (which starts b first).
	expectEvents(t, e,
		contract.EventTrackStarted, // a
		contract.EventTrackEnded,   // a
		contract.EventTrackStarted, // b
		contract.EventTrackEnded)   // b
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
