package tui

import (
	"math/rand"
	"time"

	"github.com/ali5ter/bc-radio/internal/contract"
)

// prevRestartThreshold: pressing prev within this much of a track's start
// goes to the previous track; later than that it restarts the current one
// (the classic player behaviour, so a slip of the finger does not lose a
// song).
const prevRestartThreshold = 5 * time.Second

// historyCap bounds the played-tracks ring: enough for repeat-all ordering
// and prev across a full evening, small enough that nothing accumulates
// without bound.
const historyCap = 64

// playlist is the queue policy: which tracks the user has gathered and how
// they play out. It owns shuffle, repeat and the radio, and the App mirrors
// them in the UI. It is pure state — the player is never touched here, so
// the policy is trivially testable.
//
// Tracks flow one way: the user adds tracks, they become current, and
// current becomes history. The pool for looping is the union of history
// and the queue, deduplicated, so repeat can revisit what was already
// played without duplicating anything that is still up ahead.
type playlist struct {
	rng     *rand.Rand
	history []contract.Track
	current contract.Track // zero value when idle
	last    contract.Track // most recently finished, for repeat
	queue   []contract.Track

	// index is every track of every followed band (from Library.Tracks):
	// the pool the radio picks from. indexBands counts its distinct bands
	// for the status line, derived once at SetIndex instead of per frame.
	index      []contract.Track
	indexBands int

	shuffle bool
	repeat  bool
	radio   bool // on by default: random radio is what a radio does
}

// newPlaylist builds an empty playlist with the radio on. The rng is
// time-seeded, not fixed: a radio with a deterministic draw would open on
// the same track every launch, which is not what a radio does. The tests
// never depend on the seed — their picks are forced by the avoidance rules.
func newPlaylist() *playlist {
	return &playlist{rng: rand.New(rand.NewSource(time.Now().UnixNano())), radio: true}
}

// Add appends t to the end of the queue.
func (p *playlist) Add(t contract.Track) {
	p.queue = append(p.queue, t)
}

// RemoveAt drops the queue entry at i, clamping i to the queue.
func (p *playlist) RemoveAt(i int) {
	if i < 0 {
		i = 0
	}
	if i >= len(p.queue) {
		i = len(p.queue) - 1
	}
	if i < 0 || i >= len(p.queue) {
		return
	}
	p.queue = append(p.queue[:i], p.queue[i+1:]...)
}

// Queue returns the upcoming tracks, head first.
func (p *playlist) Queue() []contract.Track { return p.queue }

// History returns the played tracks, oldest first.
func (p *playlist) History() []contract.Track { return p.history }

// Current returns the playing track (zero value when idle).
func (p *playlist) Current() contract.Track { return p.current }

// SetCurrent records a started track: it becomes current and is removed
// from the queue wherever it appears, so it is never both playing and
// queued.
func (p *playlist) SetCurrent(t contract.Track) {
	p.current = t
	out := p.queue[:0]
	for _, q := range p.queue {
		if q.ID != t.ID {
			out = append(out, q)
		}
	}
	p.queue = out
}

// OnEnded finishes the current track: recorded via recordPlayed.
func (p *playlist) OnEnded() {
	p.recordPlayed(p.current)
}

// recordPlayed is OnEnded for a named track: it records t as last played
// and moves it to the history (unless repeating). The App uses it for a
// track it abandons on the user's instruction (a skip or prev), where the
// engine does not report the abandonment as an end.
func (p *playlist) recordPlayed(t contract.Track) {
	if t.ID == "" {
		return
	}
	if !p.repeat {
		p.history = append(p.history, t)
		if len(p.history) > historyCap {
			p.history = p.history[len(p.history)-historyCap:]
		}
	}
	p.last = t
	if p.current.ID == t.ID {
		p.current = contract.Track{}
	}
}

// SetIndex replaces the flat track index the radio picks from. The band
// count is derived here so the status line never walks the index.
func (p *playlist) SetIndex(index []contract.Track) {
	p.index = index
	bands := make(map[int64]bool, len(index))
	for _, t := range index {
		bands[t.BandID] = true
	}
	p.indexBands = len(bands)
}

// Index returns the radio's flat track index.
func (p *playlist) Index() []contract.Track { return p.index }

// IndexBands is the number of distinct bands in the index.
func (p *playlist) IndexBands() int { return p.indexBands }

// radioPick picks a random index track, avoiding the named ones (the last
// played, the current, ...). It is the heart of the radio: the pick is
// uniform over the index minus the avoided tracks, and when everything is
// avoided — a one-track index — it falls back to the whole index rather
// than stalling, so a radio with a single track keeps playing it.
func (p *playlist) radioPick(avoid ...contract.Track) (contract.Track, bool) {
	if !p.radio || len(p.index) == 0 {
		return contract.Track{}, false
	}
	avoided := make(map[string]bool, len(avoid))
	for _, t := range avoid {
		if t.ID != "" {
			avoided[t.ID] = true
		}
	}
	var candidates []contract.Track
	for _, t := range p.index {
		if !avoided[t.ID] {
			candidates = append(candidates, t)
		}
	}
	if len(candidates) == 0 {
		candidates = p.index
	}
	return candidates[p.rng.Intn(len(candidates))], true
}

// pool is the tracks repeat can cycle through — the history in playback
// order (oldest first) plus the current track, deduplicated by ID — so a
// loop wraps in the order the tracks were played.
func (p *playlist) pool() []contract.Track {
	seen := make(map[string]bool)
	out := make([]contract.Track, 0, len(p.history)+1)
	add := func(t contract.Track) {
		if t.ID == "" || seen[t.ID] {
			return
		}
		seen[t.ID] = true
		out = append(out, t)
	}
	for _, t := range p.history {
		add(t)
	}
	if p.current.ID != "" {
		add(p.current)
	}
	return out
}

// Next picks the track that should play after the current one ends,
// removing it from the queue where it came from. The order of preference:
// the queued head, then repeat-one, then the radio (random index track,
// avoiding the one that just ended), then the pool (shuffled or in
// order), then nothing. With the radio on and a non-empty index it never
// returns false — the pool below is the radio-off (or empty-index) path.
func (p *playlist) Next() (contract.Track, bool) {
	if n := len(p.queue); n > 0 {
		t := p.queue[0]
		p.queue = p.queue[1:]
		return t, true
	}
	if p.repeat && p.last.ID != "" {
		return p.last, true
	}
	if t, ok := p.radioPick(p.last); ok {
		return t, true
	}
	pool := p.pool()
	if len(pool) == 0 {
		return contract.Track{}, false
	}
	if p.shuffle {
		return pool[p.rng.Intn(len(pool))], true
	}
	// In order: the pool is chronological, so continue after the last
	// played track, wrapping to the oldest. A pool of one replays itself —
	// a single gathered track keeps playing.
	for i, t := range pool {
		if t.ID == p.last.ID {
			return pool[(i+1)%len(pool)], true
		}
	}
	return pool[0], true
}

// SkipNext picks the track for an explicit next (the user pressed it),
// removing it from the queue where it came from. Unlike Next it never
// returns the repeat-one target: skipping away from a track is the opposite
// of repeating it, so the preference is the queued head, then the radio —
// avoiding both the abandoned track (still current until the load lands)
// and the last finished one, which is behind, not ahead — then the pool
// (shuffled or chronological wrap), then nothing.
func (p *playlist) SkipNext() (contract.Track, bool) {
	if n := len(p.queue); n > 0 {
		t := p.queue[0]
		p.queue = p.queue[1:]
		return t, true
	}
	if t, ok := p.radioPick(p.last, p.current); ok {
		return t, true
	}
	pool := p.pool()
	if len(pool) == 0 {
		return contract.Track{}, false
	}
	if p.shuffle {
		return pool[p.rng.Intn(len(pool))], true
	}
	for i, t := range pool {
		if t.ID == p.last.ID {
			return pool[(i+1)%len(pool)], true
		}
	}
	return pool[0], true
}

// Start picks the first track for an idle engine (the user pressed play
// with nothing playing or pending): the queued head, then repeat-one, then
// the radio. The head is peeked, not popped: a failed load keeps it for
// retry, and a successful one removes it via SetCurrent. There is
// deliberately no pool fallback at a cold start — the pool loops what has
// already played, and nothing has, so an empty queue with the radio off
// starts silence rather than noise.
func (p *playlist) Start() (contract.Track, bool) {
	if n := len(p.queue); n > 0 {
		return p.queue[0], true
	}
	if p.repeat && p.last.ID != "" {
		return p.last, true
	}
	return p.radioPick(p.last)
}

// Prev picks the previous track: the most recently finished one, removed
// from the history. With no history there is nothing to go back to.
func (p *playlist) Prev() (contract.Track, bool) {
	if n := len(p.history); n > 0 {
		t := p.history[n-1]
		p.history = p.history[:n-1]
		return t, true
	}
	return contract.Track{}, false
}
