package tui

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ali5ter/bc-radio/internal/contract"
)

// track returns a named test track; the ID is what the policy keys on, the
// display fields just make failure messages readable.
func track(id string) contract.Track {
	return contract.Track{
		ID:         id,
		Title:      "title-" + id,
		BandName:   "band-" + id,
		AlbumTitle: "album-" + id,
		Duration:   180 * time.Second,
	}
}

// playThrough simulates tracks playing and finishing in order.
func playThrough(p *playlist, ids ...string) {
	for _, id := range ids {
		t := track(id)
		p.SetCurrent(t)
		p.OnEnded()
	}
}

func TestNextQueueHeadFirst(t *testing.T) {
	p := newPlaylist()
	p.Add(track("A"))
	p.Add(track("B"))
	n, ok := p.Next()
	if !ok || n.ID != "A" {
		t.Fatalf("Next = %q, %v; want A", n.ID, ok)
	}
	if got := ids(p.Queue()); !reflect.DeepEqual(got, []string{"B"}) {
		t.Fatalf("queue after Next = %v; want [B]", got)
	}
}

func TestNextQueueBeatsRepeatOne(t *testing.T) {
	p := newPlaylist()
	p.repeat = true
	playThrough(p, "A") // repeat on: no history, last is A
	p.Add(track("B"))
	n, ok := p.Next()
	if !ok || n.ID != "B" {
		t.Fatalf("Next = %q, %v; want B (the queue beats repeat-one)", n.ID, ok)
	}
}

func TestNextRepeatOne(t *testing.T) {
	p := newPlaylist()
	p.repeat = true
	playThrough(p, "A")
	n, ok := p.Next()
	if !ok || n.ID != "A" {
		t.Fatalf("Next = %q, %v; want A (repeat-one)", n.ID, ok)
	}
}

// The pool itself is chronological and deduplicated (history in playback
// order, then the current track if it is not already in the history):
// the machinery SkipNext's chronological wrap draws on.
func TestPoolChronologicalOrder(t *testing.T) {
	p := newPlaylist()
	playThrough(p, "A", "B", "C") // history [A B C], nothing current
	pool := p.pool()
	if got := ids(pool); strings.Join(got, " ") != "A B C" {
		t.Fatalf("pool = %v; want A B C in playback order", got)
	}
	// The current track joins the pool unless it is already in the history.
	p.SetCurrent(track("C"))
	if got := ids(p.pool()); strings.Join(got, " ") != "A B C" {
		t.Fatalf("pool with current C = %v; want C deduplicated", got)
	}
	p.SetCurrent(track("D"))
	if got := ids(p.pool()); strings.Join(got, " ") != "A B C D" {
		t.Fatalf("pool with current D = %v; want D appended", got)
	}
}

// Radio on but the index empty (a failed walk): nothing opted in, so the
// next after the current track is silence — the pool is an opt-in via
// shuffle, never a default, even with the radio on.
func TestNextRadioOnEmptyIndexGoesQuiet(t *testing.T) {
	p := newPlaylist()
	p.radio = true
	playThrough(p, "A") // history [A]; the index is empty
	if n, ok := p.Next(); ok || n.ID != "" {
		t.Fatalf("Next = %q, %v; want nothing (no index, no shuffle, no repeat)", n.ID, ok)
	}
}

func TestNextEmpty(t *testing.T) {
	p := newPlaylist()
	if _, ok := p.Next(); ok {
		t.Fatal("Next on an empty playlist should report nothing")
	}
}

func TestSkipNextNeverRepeatOne(t *testing.T) {
	p := newPlaylist()
	p.repeat = true
	playThrough(p, "A") // last A, no history, no queue
	if n, ok := p.Next(); !ok || n.ID != "A" {
		t.Fatalf("Next = %q, %v; want A (repeat-one)", n.ID, ok)
	}
	// Skipping is the opposite of repeating: with nothing queued and no
	// pool there is nothing to skip to.
	if _, ok := p.SkipNext(); ok {
		t.Fatal("SkipNext with repeat on and an empty pool should report nothing")
	}
}

func TestSkipNextQueueThenPool(t *testing.T) {
	p := newPlaylist()
	p.repeat = true
	playThrough(p, "A")
	p.Add(track("B"))
	if n, ok := p.SkipNext(); !ok || n.ID != "B" {
		t.Fatalf("SkipNext = %q, %v; want B", n.ID, ok)
	}
}

// Radio off, a played session, nothing queued: an explicit next continues
// the session chronologically — and must never return the abandoned track
// (the classic no-op skip: the chronological slot behind the last finished
// track *is* the current one).
func TestSkipNextPoolContinuesSession(t *testing.T) {
	p := newPlaylist()
	p.radio = false
	playThrough(p, "A", "B") // last B, history [A B]
	p.SetCurrent(track("C")) // C playing; a skip abandons it
	if n, ok := p.SkipNext(); !ok || n.ID != "A" {
		t.Fatalf("SkipNext = %q, %v; want A (wrap to the oldest, not C)", n.ID, ok)
	}
	// Shuffled or not, the abandoned track never comes back.
	p.radio = false
	for i := 0; i < 32; i++ {
		p.SetCurrent(track("C"))
		p.shuffle = i%2 == 0
		n, ok := p.SkipNext()
		if ok && n.ID == "C" {
			t.Fatalf("SkipNext = C; a skip must never return the abandoned track")
		}
	}
}

func TestPrev(t *testing.T) {
	p := newPlaylist()
	playThrough(p, "A", "B")
	if n, ok := p.Prev(); !ok || n.ID != "B" {
		t.Fatalf("Prev = %q, %v; want B", n.ID, ok)
	}
	if n, ok := p.Prev(); !ok || n.ID != "A" {
		t.Fatalf("Prev = %q, %v; want A", n.ID, ok)
	}
	if _, ok := p.Prev(); ok {
		t.Fatal("Prev with no history should report nothing")
	}
}

func TestHistoryCap(t *testing.T) {
	p := newPlaylist()
	for i := 0; i < historyCap+6; i++ {
		tk := track(fmt.Sprintf("t%02d", i))
		p.SetCurrent(tk)
		p.OnEnded()
	}
	h := p.History()
	if len(h) != historyCap {
		t.Fatalf("history length = %d; want %d", len(h), historyCap)
	}
	if want := fmt.Sprintf("t%02d", historyCap+6-historyCap); h[0].ID != want {
		t.Fatalf("history[0] = %s; want %s (oldest dropped)", h[0].ID, want)
	}
}

func TestRepeatSuppressesHistory(t *testing.T) {
	p := newPlaylist()
	p.repeat = true
	p.SetCurrent(track("A"))
	p.OnEnded()
	if got := len(p.History()); got != 0 {
		t.Fatalf("history length = %d; repeat must keep the history empty", got)
	}
}

// Given the same rng source, the shuffle sequence is a pure function of the
// playlist state: pin the source (the constructor time-seeds its own) so the
// test asserts there is no other source of non-determinism — no map
// iteration order, no hidden clock.
func TestShuffleDeterministic(t *testing.T) {
	seq := func() []string {
		p := newPlaylist()
		p.rng = rand.New(rand.NewSource(42))
		playThrough(p, "A", "B", "C", "D")
		p.shuffle = true
		out := make([]string, 0, 50)
		for i := 0; i < 50; i++ {
			n, ok := p.Next()
			if !ok {
				t.Fatalf("Next %d on a four-track pool: nothing", i)
			}
			out = append(out, n.ID)
		}
		return out
	}
	a, b := seq(), seq()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("shuffle sequences differ:\n%v\n%v", a, b)
	}
}

func TestSetCurrentRemovesFromQueue(t *testing.T) {
	p := newPlaylist()
	p.Add(track("A"))
	p.Add(track("B"))
	p.SetCurrent(track("A"))
	if c := p.Current(); c.ID != "A" {
		t.Fatalf("Current = %q; want A", c.ID)
	}
	if got := ids(p.Queue()); !reflect.DeepEqual(got, []string{"B"}) {
		t.Fatalf("queue = %v; want [B] (a playing track is never also queued)", got)
	}
}

func TestRemoveAt(t *testing.T) {
	p := newPlaylist()
	p.Add(track("A"))
	p.Add(track("B"))
	p.Add(track("C"))
	p.RemoveAt(1)
	if got := ids(p.Queue()); !reflect.DeepEqual(got, []string{"A", "C"}) {
		t.Fatalf("queue = %v; want [A C]", got)
	}
	p.RemoveAt(99) // clamps to the last entry
	if got := ids(p.Queue()); !reflect.DeepEqual(got, []string{"A"}) {
		t.Fatalf("queue = %v; want [A]", got)
	}
	p.RemoveAt(-1) // clamps to the first
	if len(p.Queue()) != 0 {
		t.Fatalf("queue = %v; want empty", p.Queue())
	}
	p.RemoveAt(0) // no panic on an empty queue
}

func TestRemoveQueued(t *testing.T) {
	p := newPlaylist()
	p.Add(track("A"))
	p.Add(track("B"))
	if !p.removeQueued(track("A")) {
		t.Fatal("removeQueued of a present track should report true")
	}
	if got := ids(p.Queue()); !reflect.DeepEqual(got, []string{"B"}) {
		t.Fatalf("queue = %v; want [B]", got)
	}
	if p.removeQueued(track("Z")) {
		t.Fatal("removeQueued of an absent track should report false")
	}
}

// --- radio ---

func TestRadioDefaultOn(t *testing.T) {
	if !newPlaylist().radio {
		t.Fatal("the radio must be on by default: random radio is what a radio does")
	}
}

func TestSetIndexDerivesBandCount(t *testing.T) {
	p := newPlaylist()
	p.SetIndex([]contract.Track{
		{ID: "a1", BandID: 1},
		{ID: "a2", BandID: 1}, // same band as a1
		{ID: "b1", BandID: 2},
	})
	if got := p.IndexBands(); got != 2 {
		t.Fatalf("IndexBands = %d; want 2 (distinct bands)", got)
	}
	if got := len(p.Index()); got != 3 {
		t.Fatalf("len(Index) = %d; want 3", got)
	}
}

// Next prefers the queue, then repeat-one, then the radio. With the radio on
// and a non-empty index it returns an index track, avoiding the last played.
func TestNextRadioAvoidsLast(t *testing.T) {
	p := newPlaylist()
	p.SetIndex([]contract.Track{track("A"), track("B")})
	playThrough(p, "A") // last is A, and A is in the index
	if n, ok := p.Next(); !ok || n.ID != "B" {
		t.Fatalf("Next = %q, %v; want B (the only non-last index track)", n.ID, ok)
	}
}

// A one-track index: avoiding the last leaves nothing, so the radio falls
// back to the whole index and keeps playing the one track.
func TestNextRadioSingleTrackReplays(t *testing.T) {
	p := newPlaylist()
	p.SetIndex([]contract.Track{track("A")})
	playThrough(p, "A")
	if n, ok := p.Next(); !ok || n.ID != "A" {
		t.Fatalf("Next = %q, %v; want A (a one-track radio replays it)", n.ID, ok)
	}
}

func TestNextQueueBeatsRadio(t *testing.T) {
	p := newPlaylist()
	p.SetIndex([]contract.Track{track("A"), track("B")})
	p.Add(track("Q")) // the explicit queue always beats the radio
	if n, ok := p.Next(); !ok || n.ID != "Q" {
		t.Fatalf("Next = %q, %v; want Q (the explicit queue beats the radio)", n.ID, ok)
	}
}

func TestNextRepeatOneBeatsRadio(t *testing.T) {
	p := newPlaylist()
	p.repeat = true
	p.SetIndex([]contract.Track{track("A"), track("B")})
	playThrough(p, "A") // repeat on: last A, empty history
	if n, ok := p.Next(); !ok || n.ID != "A" {
		t.Fatalf("Next = %q, %v; want A (repeat-one beats the radio)", n.ID, ok)
	}
}

// Radio off and nothing opted in: the app stops picking. The next after
// the current track is silence — the pool (session history) is a last
// resort behind an explicit shuffle or repeat, never a default, so "off"
// means the same thing at a cold start and mid-session (2026-11-19:
// Alister — radio off is not the app keeping the program going).
func TestNextRadioOffGoesQuiet(t *testing.T) {
	p := newPlaylist()
	p.radio = false
	p.SetIndex([]contract.Track{track("A"), track("B")})
	playThrough(p, "C", "D") // history [C D], last D
	if n, ok := p.Next(); ok || n.ID != "" {
		t.Fatalf("Next = %q, %v; want nothing (radio off, no repeat, no shuffle)", n.ID, ok)
	}
	// The explicit queue still beats the quiet: curated plays first.
	p.Add(track("Q"))
	if n, ok := p.Next(); !ok || n.ID != "Q" {
		t.Fatalf("Next = %q, %v; want Q (the queue beats the quiet)", n.ID, ok)
	}
}

// Shuffle is the opt-in for the session pool: with the radio off, a
// shuffled next is drawn from the played tracks, never the index.
func TestNextRadioOffShuffleUsesPool(t *testing.T) {
	p := newPlaylist()
	p.radio = false
	p.shuffle = true
	p.SetIndex([]contract.Track{track("A"), track("B")})
	playThrough(p, "C", "D") // pool [C D]
	n, ok := p.Next()
	if !ok {
		t.Fatal("Next with the radio off and shuffle on must play from the pool")
	}
	if want := map[string]bool{"C": true, "D": true}; !want[n.ID] {
		t.Fatalf("Next = %q; want a played track (C or D), not the index", n.ID)
	}
}

// Repeat-one is the other explicit session loop: radio off, repeat on,
// the last track plays again (before the pool is ever consulted).
func TestNextRadioOffRepeatOne(t *testing.T) {
	p := newPlaylist()
	p.radio = false
	p.repeat = true
	p.SetIndex([]contract.Track{track("A"), track("B")})
	playThrough(p, "C", "D") // last D
	if n, ok := p.Next(); !ok || n.ID != "D" {
		t.Fatalf("Next = %q, %v; want D (repeat-one beats the pool)", n.ID, ok)
	}
}

func TestStartRadio(t *testing.T) {
	p := newPlaylist()
	p.SetIndex([]contract.Track{track("A"), track("B")})
	n, ok := p.Start()
	if !ok {
		t.Fatal("Start with the radio on and a non-empty index must return a track")
	}
	if want := map[string]bool{"A": true, "B": true}; !want[n.ID] {
		t.Fatalf("Start = %q; want an index track", n.ID)
	}
}

func TestStartRadioOffEmptyQueue(t *testing.T) {
	p := newPlaylist()
	p.radio = false
	// Cold start, radio off, empty queue: nothing to start (no pool
	// fallback at a cold start — the pool loops what already played).
	if _, ok := p.Start(); ok {
		t.Fatal("Start with the radio off and an empty queue must report nothing")
	}
}

func TestStartRadioOnEmptyIndex(t *testing.T) {
	p := newPlaylist()
	// Radio on but the index is empty (still building): nothing to start.
	if _, ok := p.Start(); ok {
		t.Fatal("Start with the radio on and an empty index must report nothing")
	}
}

func TestStartRepeatOneBeatsRadio(t *testing.T) {
	p := newPlaylist()
	p.repeat = true
	p.SetIndex([]contract.Track{track("B")})
	playThrough(p, "A") // repeat on: last A
	if n, ok := p.Start(); !ok || n.ID != "A" {
		t.Fatalf("Start = %q, %v; want A (repeat-one beats the radio at a cold start)", n.ID, ok)
	}
}

// Start peeks the queue head, not pops it: a failed load keeps it for retry;
// a successful one removes it via SetCurrent.
func TestStartPeeksQueueHead(t *testing.T) {
	p := newPlaylist()
	p.Add(track("A"))
	p.Add(track("B"))
	n, ok := p.Start()
	if !ok || n.ID != "A" {
		t.Fatalf("Start = %q, %v; want A", n.ID, ok)
	}
	if got := ids(p.Queue()); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Fatalf("queue after Start = %v; want [A B] (the head is peeked, not popped)", got)
	}
	p.SetCurrent(n)
	if got := ids(p.Queue()); !reflect.DeepEqual(got, []string{"B"}) {
		t.Fatalf("queue after SetCurrent = %v; want [B] (a started track leaves the queue)", got)
	}
}

// SkipNext (an explicit next) picks a radio track avoiding both the
// abandoned current and the last finished one.
func TestSkipNextRadioAvoidsLastAndCurrent(t *testing.T) {
	p := newPlaylist()
	p.SetIndex([]contract.Track{track("A"), track("B"), track("C")})
	playThrough(p, "B")      // last B, current cleared
	p.SetCurrent(track("A")) // current A, about to be abandoned
	if n, ok := p.SkipNext(); !ok || n.ID != "C" {
		t.Fatalf("SkipNext = %q, %v; want C (avoid last B and current A)", n.ID, ok)
	}
}

// The prepared draw avoids the last played *and* the one in the speakers:
// on a two-track index, "avoid the last" alone would force a preview of
// what is currently playing, and the hop after would replay it.
func TestPrepareRadioNextAvoidsCurrent(t *testing.T) {
	p := newPlaylist()
	p.SetIndex([]contract.Track{track("A"), track("B")})
	playThrough(p, "B")      // last B
	p.SetCurrent(track("A")) // A playing
	p.prepareRadioNext()
	if got := p.radioNext.ID; got != "B" {
		t.Fatalf("prepared draw = %q; want B (the only track that is neither last nor current)", got)
	}
}

// prepareRadioNext must not preview the current track on any index: the
// redraw after a track starts (the gapless radio's bookkeeping) runs while
// that track is still playing.
func TestPrepareRadioNextNeverPreviewsCurrent(t *testing.T) {
	p := newPlaylist()
	p.SetIndex([]contract.Track{track("A"), track("B"), track("C"), track("D")})
	p.last = track("C")
	p.SetCurrent(track("A"))
	for i := 0; i < 200; i++ {
		p.prepareRadioNext()
		if got := p.radioNext.ID; got == "A" {
			t.Fatalf("draw %d = %q; the radio must never preview the track in the speakers", i, got)
		}
	}
}

// upNext is the engine slot's fuel in the gapless flow: the queued head
// first, then repeat-one, then the prepared radio draw; it peeks, so it
// leaves both the queue and the draw untouched.
func TestUpNextPrecedence(t *testing.T) {
	p := newPlaylist()
	p.SetIndex([]contract.Track{track("A"), track("B")})
	playThrough(p, "C") // last C
	p.SetCurrent(track("X"))
	p.prepareRadioNext()
	draw := p.radioNext

	if n := p.upNext(); n.ID != draw.ID {
		t.Fatalf("upNext = %q; want the radio draw %q with an empty queue", n.ID, draw.ID)
	}

	p.repeat = true
	if n := p.upNext(); n.ID != "C" {
		t.Fatalf("upNext = %q; repeat-one beats the radio", n.ID)
	}

	p.Add(track("Q"))
	if n := p.upNext(); n.ID != "Q" {
		t.Fatalf("upNext = %q; the queued head beats repeat-one and the radio", n.ID)
	}
	// The peek mutated nothing.
	if got := len(p.Queue()); got != 1 {
		t.Fatalf("queue length = %d; upNext must peek, not pop", got)
	}
	if p.radioNext.ID != draw.ID {
		t.Fatalf("radio draw changed to %q; upNext must not consume it", p.radioNext.ID)
	}
}

// recordPlayed dedupes adjacent duplicates: the same end can be reported
// twice (an explicit load records the abandoned track, then the engine's
// TrackStarted for it arrives behind), and repeat-one ends the same track
// over and over — neither may stack the history, or prev would pop the
// same track twice in a row.
func TestRecordPlayedAdjacentDedup(t *testing.T) {
	p := newPlaylist()
	p.SetCurrent(track("A"))
	p.recordPlayed(track("A")) // the load path
	p.recordPlayed(track("A")) // the engine's end for the same track
	if got := len(p.History()); got != 1 {
		t.Fatalf("history = %v; the same end reported twice must stack once", ids(p.History()))
	}
	p.recordPlayed(track("B"))
	p.recordPlayed(track("A")) // non-adjacent: a different end in between
	if got := ids(p.History()); !reflect.DeepEqual(got, []string{"A", "B", "A"}) {
		t.Fatalf("history = %v; want [A B A] (dedup is adjacent only)", got)
	}
}

// consumeRadioNext will not hand back a draw that is now stale: the last
// played, or the track in the speakers.
func TestConsumeRadioNextStaleRedraws(t *testing.T) {
	p := newPlaylist()
	p.SetIndex([]contract.Track{track("A"), track("B"), track("C")})
	playThrough(p, "B")      // last B
	p.SetCurrent(track("A")) // A in the speakers
	p.radioNext = track("A") // a stale preview of the current
	if n, ok := p.consumeRadioNext(); !ok || n.ID != "C" {
		t.Fatalf("consume = %q, %v; a stale draw must be redrawn (C: neither last nor current)", n.ID, ok)
	}
}

// ids projects tracks to their IDs for readable assertions.
func ids(ts []contract.Track) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.ID
	}
	return out
}

// last returns the last element of s, or "" when empty.
func last(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[len(s)-1]
}
