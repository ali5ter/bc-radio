package audio

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"bc-radio/internal/contract"
)

// errStopped is returned by operations on an engine that has been stopped.
var errStopped = errors.New("audio: engine stopped")

// pending is a track whose stream has been, or is being, prepared to play
// seamlessly after the current one. ready is closed once preparation
// finishes; chain is nil if it failed.
type pending struct {
	chain *chain
	ready chan struct{}
}

// Engine is the bc-radio playback engine. It implements contract.Player: it
// decodes Bandcamp MP3 streams (beep/mp3), plays them to the audio device
// through the pump, exposes the 16-band analyser, and emits player events.
//
// The engine runs for the life of the application. Stop unwinds it to idle
// and closes the event channel, after which it is done; Close additionally
// releases the output device.
type Engine struct {
	source     contract.AudioSource
	device     device
	sampleRate int

	analyser *analyser
	pump     *pump

	// decMu guards chain decoding against Stop: the device keeps pulling
	// (pump.Read) for up to half a second after a pause, and a Read that
	// is mid-decode when Stop closes the chain would tear the decoder out
	// from under it. Stop takes it around the chain closes, so a close
	// waits for any in-flight Read to finish.
	decMu sync.Mutex

	mu      sync.Mutex
	cur     *chain
	next    *pending
	vol     *volume
	playing bool
	stopped bool
	pos     int64 // frames of the current track consumed, guarded by mu

	ev chan contract.Event
	wg sync.WaitGroup // in-flight queue-prep goroutines

	ctx    context.Context
	cancel context.CancelFunc
}

// Option customises an Engine at construction.
type Option func(*Engine)

// WithDevice overrides the default oto output device. Tests use it to avoid
// opening a real audio device; the pump is then driven manually.
func WithDevice(d device) Option { return func(e *Engine) { e.device = d } }

// New builds an Engine that plays tracks from source. Unless WithDevice is
// given, it opens the system audio output device. The engine runs until Stop
// or Close.
func New(source contract.AudioSource, opts ...Option) (*Engine, error) {
	e := &Engine{
		source:     source,
		sampleRate: deviceRate,
		analyser:   newAnalyser(deviceRate),
		vol:        &volume{g: 1},
		ev:         make(chan contract.Event, 32),
	}
	e.ctx, e.cancel = context.WithCancel(context.Background())
	e.pump = newPump(e)
	for _, o := range opts {
		o(e)
	}
	if e.device == nil {
		d, err := newOtoDevice(e.pump)
		if err != nil {
			return nil, fmt.Errorf("audio: open output device: %w", err)
		}
		e.device = d
	}
	e.device.play()
	return e, nil
}

var _ contract.Player = (*Engine)(nil)

// Load starts playing t at once, replacing whatever is current and
// discarding any queued next track. It fetches and decodes t (bounded by
// ctx); on failure it emits EventPlaybackFailed and returns the error,
// leaving any current track untouched. The pump emits TrackStarted when it
// activates the new chain.
//
// On success Load flushes the device: the player keeps up to half a second
// of already-pulled audio in its buffer, and nothing but a reset clears it,
// so without the flush the tail of the previous track would play ahead of
// t.
func (e *Engine) Load(ctx context.Context, t contract.Track) error {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return errStopped
	}
	e.mu.Unlock()

	c, err := newChain(ctx, e.source, t, e.sampleRate, e.vol)
	if err != nil {
		e.emit(contract.Event{Kind: contract.EventPlaybackFailed, Track: t, Err: err})
		return err
	}
	e.mu.Lock()
	if e.next != nil {
		// An explicit Load supersedes the queued next. A preparation that
		// has already installed its chain is unreferenced once the slot is
		// dropped, so release it; an in-flight preparation closes its own
		// chain when it sees the slot gone (prepare).
		if e.next.chain != nil {
			e.next.chain.close()
		}
		e.next = nil
	}
	e.cur = c
	e.pos = 0
	e.playing = true
	e.mu.Unlock()
	e.device.flush()
	return nil
}

// Queue schedules t to start seamlessly (gapless) when the current track
// ends: its stream is fetched and decoded in the background so the pump can
// pick it up the moment the current one drains. If the engine is idle, Queue
// starts t at once. The engine keeps a single next slot — queuing again
// replaces the previous next; the TUI owns multi-track queue policy.
func (e *Engine) Queue(t contract.Track) {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return
	}
	idle := e.cur == nil && e.next == nil
	e.mu.Unlock()

	if idle {
		_ = e.Load(e.ctx, t)
		return
	}
	e.mu.Lock()
	if e.next != nil {
		e.next = nil // the superseded prep goroutine releases its chain
	}
	pd := &pending{ready: make(chan struct{})}
	e.next = pd
	e.wg.Add(1)
	e.mu.Unlock()
	go e.prepare(t, pd)
}

// Play resumes playback of the current track from where it paused. The
// device's buffer may hold up to half a second of already-decoded audio
// (the player keeps pulling until it is full, even paused), so resuming
// plays exactly what would have played next — the TUI should freeze its
// position display while not playing, because State().Position keeps
// advancing by the buffered amount during the pause.
func (e *Engine) Play(ctx context.Context) error {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return errStopped
	}
	e.playing = true
	e.mu.Unlock()
	e.device.play()
	return nil
}

// Pause suspends playback. The decoder position is preserved, so Play
// resumes where it left off.
func (e *Engine) Pause(ctx context.Context) error {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return errStopped
	}
	e.playing = false
	e.mu.Unlock()
	e.device.pause()
	return nil
}

// Stop unwinds the engine to idle and closes the event channel. It is
// terminal: the engine cannot be used afterwards (build a new one for
// further playback).
func (e *Engine) Stop(ctx context.Context) error {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return nil
	}
	e.stopped = true
	e.playing = false
	e.cancel()
	e.device.pause() // stop new pulls before releasing chains
	e.mu.Unlock()

	// The device's pull loop may be mid-Read (a paused player keeps pulling
	// until its buffer is full); the decode mutex waits for it to finish
	// before the chains are closed, so a close never races a decode.
	e.decMu.Lock()
	e.mu.Lock()
	if e.cur != nil {
		e.cur.close()
		e.cur = nil
	}
	if e.next != nil {
		e.next.chain.close()
		e.next = nil
	}
	e.pos = 0
	close(e.ev)
	e.mu.Unlock()
	e.decMu.Unlock()

	e.wg.Wait()
	return nil
}

// SetVolume sets the digital gain, clamped to [0,1].
func (e *Engine) SetVolume(ctx context.Context, v float64) error {
	e.vol.set(v)
	return nil
}

// State returns the current snapshot.
func (e *Engine) State() contract.State {
	e.mu.Lock()
	var track contract.Track
	if e.cur != nil {
		track = e.cur.track
	}
	pos := e.pos
	playing := e.playing
	stopped := e.stopped
	e.mu.Unlock()
	return contract.State{
		Track:    track,
		Position: time.Duration(float64(pos) / float64(e.sampleRate) * float64(time.Second)),
		Playing:  playing && !stopped,
		Volume:   e.vol.get(),
	}
}

// Bands returns the latest analyser frame: contract.BandCount values in
// [0,1], ordered low to high frequency.
func (e *Engine) Bands() []float64 {
	return e.analyser.Bands()
}

// Events returns the player-event channel, closed when the engine stops.
func (e *Engine) Events() <-chan contract.Event {
	return e.ev
}

// Close stops the engine and releases the output device. It is the
// application's clean-shutdown path (distinct from the contract's Stop,
// which idles the engine but keeps the device) and is not part of
// contract.Player.
func (e *Engine) Close() error {
	err := e.Stop(context.Background())
	e.mu.Lock()
	d := e.device
	e.device = nil
	e.mu.Unlock()
	if d != nil {
		d.close()
	}
	return err
}

// prepare fetches and decodes t in the background for a queued next slot.
// When done it installs the chain if pd is still the current next, else
// releases it. It always closes pd.ready so waiters never block.
func (e *Engine) prepare(t contract.Track, pd *pending) {
	defer e.wg.Done()
	c, _ := newChain(e.ctx, e.source, t, e.sampleRate, e.vol)
	e.mu.Lock()
	if e.stopped || e.next != pd {
		c.close() // superseded or engine stopped
	} else if c == nil {
		e.next = nil // preparation failed; nothing to play next
	} else {
		pd.chain = c
	}
	e.mu.Unlock()
	close(pd.ready)
}

// sync is called by the pump at the start of each Read: if the engine is
// idle and a prepared next is ready, it becomes current.
func (e *Engine) sync() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cur == nil && e.next != nil {
		select {
		case <-e.next.ready:
			e.cur = e.next.chain
			e.next = nil
		default:
		}
	}
}

// currentChain returns the active chain, or nil when idle.
func (e *Engine) currentChain() *chain {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cur
}

// chainDrained is called by the pump when cur's stream is exhausted. It
// clears the current slot and, if a prepared next is ready, installs it so
// the pump continues gaplessly. It does not emit events — the pump's advance
// does, on the resulting chain change. With nothing current and nothing
// queued the transport stops, so State().Playing goes false; the next Queue
// (idle) or Load starts it again.
func (e *Engine) chainDrained(cur *chain) {
	e.mu.Lock()
	if e.cur == cur {
		e.cur = nil
	}
	if e.next != nil {
		select {
		case <-e.next.ready:
			e.cur = e.next.chain
			e.next = nil
		default:
		}
	}
	if e.cur == nil && e.next == nil {
		e.playing = false
	}
	e.mu.Unlock()
}

// activate marks cur as the active track: it resets the position and emits
// TrackStarted. Called by the pump when a chain becomes current.
func (e *Engine) activate(cur *chain) {
	e.mu.Lock()
	e.pos = 0
	e.mu.Unlock()
	e.emit(contract.Event{Kind: contract.EventTrackStarted, Track: cur.track})
}

// addPos records frames consumed from the current track.
func (e *Engine) addPos(n int) {
	e.mu.Lock()
	e.pos += int64(n)
	e.mu.Unlock()
}

// emit sends an event without blocking the caller (which may be the device
// pump). It drops the event if the engine is stopped or the channel is full.
// The stopped check and the send share the engine lock so a concurrent Stop
// (which closes the channel under the same lock) can never leave a send on a
// closed channel.
func (e *Engine) emit(ev contract.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped {
		return
	}
	select {
	case e.ev <- ev:
	default:
	}
}
