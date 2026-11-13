package tui

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"bc-radio/internal/contract"
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

func TestNextPoolChronologicalWrap(t *testing.T) {
	p := newPlaylist()
	playThrough(p, "A", "B", "C")
	// last C, history [A B C]: the pool wraps to the oldest.
	n, ok := p.Next()
	if !ok || n.ID != "A" {
		t.Fatalf("Next = %q, %v; want A (chronological wrap)", n.ID, ok)
	}
}

func TestNextSingleTrackPoolReplays(t *testing.T) {
	p := newPlaylist()
	playThrough(p, "A")
	// A radio with one gathered track keeps playing it.
	n, ok := p.Next()
	if !ok || n.ID != "A" {
		t.Fatalf("Next = %q, %v; want A (single-track pool replays)", n.ID, ok)
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

// Radio off: Next uses the pool (history + current), never the index.
func TestNextRadioOffUsesPool(t *testing.T) {
	p := newPlaylist()
	p.radio = false
	p.SetIndex([]contract.Track{track("A"), track("B")})
	playThrough(p, "C", "D") // pool [C D], last D
	if n, ok := p.Next(); !ok || n.ID != "C" {
		t.Fatalf("Next = %q, %v; want C (radio off: the pool wraps, not the index)", n.ID, ok)
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
