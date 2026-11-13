package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"bc-radio/internal/contract"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// fakePlayer is a scriptable contract.Player: it records calls and exposes
// its state directly, so tests can arrange the engine in whatever shape an
// event handler must cope with. The Events channel is never driven by the
// fake — tests deliver eventMsg to Update directly, which keeps them
// deterministic.
type fakePlayer struct {
	mu      sync.Mutex
	state   contract.State
	bands   []float64
	loads   []contract.Track
	queues  []contract.Track
	plays   int
	pauses  int
	stops   int
	volumes []float64
	loadErr error
	events  chan contract.Event
}

func newFakePlayer() *fakePlayer {
	return &fakePlayer{
		bands:  make([]float64, contract.BandCount),
		events: make(chan contract.Event, 8),
	}
}

func (f *fakePlayer) Load(ctx context.Context, t contract.Track) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loads = append(f.loads, t)
	return f.loadErr
}

func (f *fakePlayer) Queue(t contract.Track) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queues = append(f.queues, t)
}

func (f *fakePlayer) Play(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plays++
	f.state.Playing = true
	return nil
}

func (f *fakePlayer) Pause(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pauses++
	f.state.Playing = false
	return nil
}

func (f *fakePlayer) Stop(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	f.state = contract.State{}
	return nil
}

func (f *fakePlayer) SetVolume(ctx context.Context, v float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.volumes = append(f.volumes, v)
	f.state.Volume = v
	return nil
}

func (f *fakePlayer) State() contract.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *fakePlayer) Bands() []float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bands
}

func (f *fakePlayer) Events() <-chan contract.Event { return f.events }

// setState mutates the engine state directly (tests only).
func (f *fakePlayer) setState(s contract.State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = s
}

func (f *fakePlayer) setBands(vals ...float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bands = frameBands(vals...)
}

// fakeLib serves preset library data and logs which calls arrived.
type fakeLib struct {
	mu       sync.Mutex
	artists  []contract.Artist
	albums   map[int64][]contract.Album
	full     map[string]contract.Album
	err      error
	artistCt int
	albumCt  int
	fullCt   int
}

func (f *fakeLib) Artists(ctx context.Context) ([]contract.Artist, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.artistCt++
	if f.err != nil {
		return nil, f.err
	}
	return f.artists, nil
}

func (f *fakeLib) Albums(ctx context.Context, a contract.Artist) ([]contract.Album, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.albumCt++
	if f.err != nil {
		return nil, f.err
	}
	return f.albums[a.BandID], nil
}

func (f *fakeLib) Album(ctx context.Context, a contract.Artist, slug string) (contract.Album, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fullCt++
	if f.err != nil {
		return contract.Album{}, f.err
	}
	al := f.full[slug]
	al.BandID = a.BandID
	return al, nil
}

// newTestApp builds an app over the fakes, sized 80 × 24.
func newTestApp() (*App, *fakePlayer, *fakeLib) {
	lp, fl := newFakePlayer(), &fakeLib{
		albums: map[int64][]contract.Album{},
		full:   map[string]contract.Album{},
	}
	app := New(fl, lp)
	_, _ = app.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return app, lp, fl
}

// press delivers one key press and returns the command it produced.
func press(t *testing.T, a *App, msg tea.KeyPressMsg) tea.Cmd {
	t.Helper()
	_, cmd := a.Update(msg)
	return cmd
}

func TestNew(t *testing.T) {
	app := New(&fakeLib{}, newFakePlayer())
	if got := len(app.bands); got != contract.BandCount {
		t.Fatalf("bands = %d; want %d", got, contract.BandCount)
	}
	if got := len(app.visualizers); got != 3 {
		t.Fatalf("visualizers = %d; want 3", got)
	}
}

func TestInit(t *testing.T) {
	app, _, _ := newTestApp()
	if app.Init() == nil {
		t.Fatal("Init must return the tick and event commands")
	}
}

func TestResize(t *testing.T) {
	app, _, _ := newTestApp()
	_, _ = app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if app.width != 100 || app.height != 30 {
		t.Fatalf("size = %dx%d; want 100x30", app.width, app.height)
	}
	if app.browser.viewH != 27 {
		t.Fatalf("browser viewport = %d; want 27", app.browser.viewH)
	}
}

// The View must be exactly the terminal size on every screen: the layout
// builds every line at full width and joins them, so any drift here tears
// the frame.
func TestViewSize(t *testing.T) {
	app, _, fl := newTestApp()
	fl.artists = []contract.Artist{{BandID: 1, Name: "Alpha"}, {BandID: 2, Name: "Beta"}}

	check := func(name string) {
		t.Helper()
		w, h := lipgloss.Size(app.View().Content)
		if w != 80 || h != 24 {
			t.Errorf("%s screen: view is %dx%d; want 80x24", name, w, h)
		}
	}
	check("player")
	app.fullscreen = true
	check("fullscreen")
	app.fullscreen = false
	app.help = true
	check("help")
	app.help = false
	app.screen = screenLibrary
	check("library")
}

func TestViewTooSmall(t *testing.T) {
	app, _, _ := newTestApp()
	_, _ = app.Update(tea.WindowSizeMsg{Width: 20, Height: 4})
	v := app.View().Content
	w, h := lipgloss.Size(v)
	if w != 20 || h != 4 {
		t.Fatalf("tiny view is %dx%d; want 20x4", w, h)
	}
	if !strings.Contains(stripANSI(v), "too small") {
		t.Fatalf("tiny view should say the terminal is too small: %q", stripANSI(v))
	}
}

// sample eases the band frame 30% toward the target each tick, freezes the
// position while paused, and decays to rest when the engine is idle — the
// engine's analyser is only fed while audio is produced, so a stale
// non-zero Bands() must not keep the visual alive.
func TestSample(t *testing.T) {
	app, lp, _ := newTestApp()
	lp.setState(contract.State{Track: track("A"), Playing: true, Position: 30 * time.Second})
	lp.setBands(1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1)

	app.sample()
	if app.bands[0] != 0.3 {
		t.Fatalf("band after first sample = %v; want 0.3 (30%% ease from 0)", app.bands[0])
	}
	if app.pos != 30*time.Second {
		t.Fatalf("pos = %v; want 30s while playing", app.pos)
	}

	// The music stops: the frame decays and the position freezes.
	lp.setState(contract.State{Track: track("A"), Position: 31 * time.Second})
	lp.setBands() // zero frame
	app.sample()
	if got := app.bands[0]; got != 0.21 {
		t.Fatalf("band after silence = %v; want 0.21 (decay toward 0)", got)
	}
	if app.pos != 30*time.Second {
		t.Fatalf("pos = %v; must stay frozen while paused", app.pos)
	}

	// Engine idle with a stale analyser frame: the target is zero, not the
	// stale frame.
	lp.setState(contract.State{})
	lp.setBands(1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1)
	app.sample()
	if got := app.bands[0]; got != 0.147 {
		t.Fatalf("idle band = %v; want 0.147 (decay; the stale analyser is ignored)", got)
	}
}

func TestTrackStartedConsumesQueueSlot(t *testing.T) {
	app, lp, _ := newTestApp()
	app.playlist.Add(track("A"))
	app.playlist.Add(track("B"))
	app.playlist.SetCurrent(track("C"))
	app.engineNextID = "A" // the engine holds A in its pending slot
	lp.setState(contract.State{Track: track("A"), Playing: true})

	_, _ = app.Update(eventMsg{ev: contract.Event{Kind: contract.EventTrackStarted, Track: track("A")}})

	if got := app.playlist.Current().ID; got != "A" {
		t.Fatalf("current = %q; want A", got)
	}
	if got := ids(app.playlist.Queue()); len(got) != 1 || got[0] != "B" {
		t.Fatalf("queue = %v; want [B] (A started, so it left the queue)", got)
	}
	// The slot was consumed and refilled with B for a gapless advance.
	if app.engineNextID != "B" {
		t.Fatalf("engineNextID = %q; want B", app.engineNextID)
	}
	if got := last(ids(lp.queues)); got != "B" {
		t.Fatalf("player Queue calls = %v; want a final B", ids(lp.queues))
	}
}

func TestTrackStartedForeignSlotUntouched(t *testing.T) {
	app, lp, _ := newTestApp()
	app.playlist.SetCurrent(track("C"))
	app.engineNextID = "X" // the pending slot holds something the app does not know
	lp.setState(contract.State{Track: track("A"), Playing: true})

	_, _ = app.Update(eventMsg{ev: contract.Event{Kind: contract.EventTrackStarted, Track: track("A")}})

	if app.engineNextID != "X" {
		t.Fatalf("engineNextID = %q; a started track that is not the pending one must not clear it", app.engineNextID)
	}
	if len(lp.queues) != 0 {
		t.Fatalf("Queue calls = %v; slot full, so none expected", ids(lp.queues))
	}
}

// An ended track with the engine idle: the app must pick and start the
// next one itself (Queue on an idle engine starts it at once).
func TestTrackEndedIdleAutoAdvance(t *testing.T) {
	app, lp, _ := newTestApp()
	app.playlist.Add(track("B"))
	app.playlist.SetCurrent(track("C"))
	lp.setState(contract.State{}) // idle: the engine's own advance exhausted

	_, _ = app.Update(eventMsg{ev: contract.Event{Kind: contract.EventTrackEnded, Track: track("C")}})

	if got := last(ids(lp.queues)); got != "B" {
		t.Fatalf("Queue calls = %v; want the app to start B", ids(lp.queues))
	}
	if got := app.playlist.Current().ID; got != "" {
		t.Fatalf("current = %q; C ended, so the playlist must be idle", got)
	}
	if !containsID(app.playlist.History(), "C") {
		t.Fatal("C must be in the history")
	}
}

// An ended track while the engine already advanced to its prepared next:
// the TrackStarted that follows owns the bookkeeping, so no double advance.
func TestTrackEndedEngineAdvanced(t *testing.T) {
	app, lp, _ := newTestApp()
	app.playlist.SetCurrent(track("C"))
	app.engineNextID = "B"
	lp.setState(contract.State{Track: track("B"), Playing: true}) // the engine moved

	_, _ = app.Update(eventMsg{ev: contract.Event{Kind: contract.EventTrackEnded, Track: track("C")}})

	if got := ids(lp.queues); len(got) != 0 {
		t.Fatalf("Queue calls = %v; want none (the engine advanced on its own)", got)
	}
	if got := app.playlist.Current().ID; got != "C" {
		t.Fatalf("current = %q; must stay C until B's TrackStarted arrives", got)
	}
}

func TestPlaybackFailedIdleNoRetry(t *testing.T) {
	app, lp, _ := newTestApp()
	app.playlist.SetCurrent(track("B")) // nothing else gathered: the pool is [B]
	lp.setState(contract.State{})

	_, _ = app.Update(eventMsg{ev: contract.Event{
		Kind: contract.EventPlaybackFailed, Track: track("B"), Err: errors.New("stream 404"),
	}})

	if got := ids(lp.queues); len(got) != 0 {
		t.Fatalf("Queue calls = %v; a failed track with nothing else must not be retried", got)
	}
	if !strings.Contains(app.status, "playback failed") {
		t.Fatalf("status = %q; want the failure surfaced", app.status)
	}
}

func TestPlaybackFailedIdleWithQueueAdvances(t *testing.T) {
	app, lp, _ := newTestApp()
	app.playlist.Add(track("C"))
	app.playlist.SetCurrent(track("B"))
	lp.setState(contract.State{})

	_, _ = app.Update(eventMsg{ev: contract.Event{
		Kind: contract.EventPlaybackFailed, Track: track("B"), Err: errors.New("boom"),
	}})

	if got := last(ids(lp.queues)); got != "C" {
		t.Fatalf("Queue calls = %v; want C", ids(lp.queues))
	}
}

// Space on an idle engine with a queue starts the head.
func TestSpaceStartsQueueHead(t *testing.T) {
	app, lp, _ := newTestApp()
	app.playlist.Add(track("A"))
	lp.setState(contract.State{})

	cmd := press(t, app, tea.KeyPressMsg{Code: tea.KeySpace})
	m, ok := mustRun(t, cmd).(loadedMsg)
	if !ok || m.track.ID != "A" || m.err != nil {
		t.Fatalf("space should load A; got %v (%v)", m, m.err)
	}
	_, _ = app.Update(m)

	if got := app.playlist.Current().ID; got != "A" {
		t.Fatalf("current = %q; want A", got)
	}
	if app.pendingLoad {
		t.Fatal("pendingLoad must clear when the load lands")
	}
	if !containsTrack(lp.loads, "A") {
		t.Fatalf("Load calls = %v; want A", ids(lp.loads))
	}
}

func mustRun(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command, got nil")
	}
	return cmd()
}

func TestSpacePauseResume(t *testing.T) {
	app, lp, _ := newTestApp()
	lp.setState(contract.State{Track: track("A"), Playing: true, Volume: 1})
	app.state = lp.State()

	press(t, app, tea.KeyPressMsg{Code: tea.KeySpace})
	if lp.pauses != 1 || !strings.Contains(app.status, "paused") {
		t.Fatalf("pauses = %d, status = %q; want one pause", lp.pauses, app.status)
	}

	lp.setState(contract.State{Track: track("A"), Volume: 1}) // paused
	app.state = lp.State()
	press(t, app, tea.KeyPressMsg{Code: tea.KeySpace})
	if lp.plays != 1 || !strings.Contains(app.status, "playing") {
		t.Fatalf("plays = %d, status = %q; want one play", lp.plays, app.status)
	}
}

func TestSpaceIdleEmptyQueue(t *testing.T) {
	app, _, _ := newTestApp()
	press(t, app, tea.KeyPressMsg{Code: tea.KeySpace})
	if !strings.Contains(app.status, "queue is empty") {
		t.Fatalf("status = %q; want the empty-queue hint", app.status)
	}
}

// n abandons the current track (it lands in the history when the
// replacement is confirmed) and loads the policy's next.
func TestSkipFlow(t *testing.T) {
	app, lp, _ := newTestApp()
	lp.setState(contract.State{Track: track("A"), Playing: true})
	app.state = lp.State()
	app.playlist.SetCurrent(track("A")) // the engine and the playlist agree
	app.playlist.Add(track("B"))

	m, ok := mustRun(t, press(t, app, tea.KeyPressMsg{Text: "n"})).(loadedMsg)
	if !ok || m.track.ID != "B" {
		t.Fatalf("skip should load B; got %v", m)
	}
	_, _ = app.Update(m)

	if got := app.playlist.Current().ID; got != "B" {
		t.Fatalf("current = %q; want B", got)
	}
	if !containsID(app.playlist.History(), "A") {
		t.Fatalf("history = %v; the abandoned A must be recorded", ids(app.playlist.History()))
	}
	if app.pendingLoad {
		t.Fatal("pendingLoad must clear")
	}
}

// A skip with a single gathered track replays it (the pool holds it); an
// idle engine with nothing at all ends the queue.
func TestSkipEmpty(t *testing.T) {
	app, lp, _ := newTestApp()
	lp.setState(contract.State{Track: track("A"), Playing: true})
	app.state = lp.State()
	app.playlist.SetCurrent(track("A")) // the engine and the playlist agree

	m, ok := mustRun(t, press(t, app, tea.KeyPressMsg{Text: "n"})).(loadedMsg)
	if !ok || m.track.ID != "A" {
		t.Fatalf("skip with only A gathered should replay A; got %v", m)
	}

	app, lp, _ = newTestApp()
	app.state = contract.State{} // idle, nothing gathered
	cmd := press(t, app, tea.KeyPressMsg{Text: "n"})
	if cmd != nil {
		t.Fatalf("skip with nothing to play should produce no command; got %v", cmd)
	}
	if !strings.Contains(app.status, "end of queue") {
		t.Fatalf("status = %q; want end of queue", app.status)
	}
	_ = lp
}

// p within 5 s of the start restarts the current track; later it goes to
// the most recent history entry.
func TestPrevRestart(t *testing.T) {
	app, lp, _ := newTestApp()
	lp.setState(contract.State{Track: track("A"), Playing: true, Position: 2 * time.Second})
	app.state = lp.State()
	app.pos = 2 * time.Second

	m, ok := mustRun(t, press(t, app, tea.KeyPressMsg{Text: "p"})).(loadedMsg)
	if !ok || m.track.ID != "A" {
		t.Fatalf("early prev should restart A; got %v", m)
	}
	_, _ = app.Update(m)
	if app.pos != 0 {
		t.Fatalf("pos = %v; a restart must zero it", app.pos)
	}
}

func TestPrevPopsHistory(t *testing.T) {
	app, lp, _ := newTestApp()
	lp.setState(contract.State{Track: track("B"), Playing: true, Position: 60 * time.Second})
	app.state = lp.State()
	app.pos = 60 * time.Second
	app.playlist.history = []contract.Track{track("A")} // B is current, A finished

	m, ok := mustRun(t, press(t, app, tea.KeyPressMsg{Text: "p"})).(loadedMsg)
	if !ok || m.track.ID != "A" {
		t.Fatalf("late prev should load A from the history; got %v", m)
	}
	_, _ = app.Update(m)
	if got := app.playlist.Current().ID; got != "A" {
		t.Fatalf("current = %q; want A", got)
	}
	// The abandoned B is recorded: it was played, just not finished.
	if !containsID(app.playlist.History(), "B") {
		t.Fatalf("history = %v; the abandoned B must be recorded", ids(app.playlist.History()))
	}
}

// containsID reports whether any of ts has the given ID.
func containsID(ts []contract.Track, id string) bool {
	for _, t := range ts {
		if t.ID == id {
			return true
		}
	}
	return false
}

func TestQuit(t *testing.T) {
	app, _, _ := newTestApp()
	m := mustRun(t, press(t, app, tea.KeyPressMsg{Text: "q"}))
	if m == nil {
		t.Fatal("q must produce a quit message (v2: Quit returns a Msg)")
	}
}

func TestVolumeClamp(t *testing.T) {
	app, lp, _ := newTestApp()
	lp.setState(contract.State{Track: track("A"), Volume: 0.97})
	app.state = lp.State()
	press(t, app, tea.KeyPressMsg{Text: "+"})
	if v := lastVolume(t, lp); v != 1.0 {
		t.Fatalf("volume = %v; must clamp to 1", v)
	}

	lp.setState(contract.State{Track: track("A"), Volume: 0.02})
	app.state = lp.State()
	press(t, app, tea.KeyPressMsg{Text: "-"})
	if v := lastVolume(t, lp); v != 0.0 {
		t.Fatalf("volume = %v; must clamp to 0", v)
	}
}

func lastVolume(t *testing.T, lp *fakePlayer) float64 {
	t.Helper()
	if len(lp.volumes) == 0 {
		t.Fatal("no SetVolume calls")
	}
	return lp.volumes[len(lp.volumes)-1]
}

func TestToggles(t *testing.T) {
	app, _, _ := newTestApp()
	press(t, app, tea.KeyPressMsg{Text: "s"})
	if !app.playlist.shuffle || app.status != "shuffle on" {
		t.Fatalf("shuffle = %v, status = %q", app.playlist.shuffle, app.status)
	}
	press(t, app, tea.KeyPressMsg{Text: "s"})
	if app.playlist.shuffle || app.status != "shuffle off" {
		t.Fatalf("shuffle = %v, status = %q", app.playlist.shuffle, app.status)
	}
	press(t, app, tea.KeyPressMsg{Text: "r"})
	if !app.playlist.repeat {
		t.Fatal("repeat must turn on")
	}
	press(t, app, tea.KeyPressMsg{Text: "v"})
	if app.visIdx != 1 || app.status != "visualizer: waveform" {
		t.Fatalf("visIdx = %d, status = %q; want 1 / waveform", app.visIdx, app.status)
	}
	press(t, app, tea.KeyPressMsg{Text: "f"})
	if !app.fullscreen {
		t.Fatal("f must enter fullscreen")
	}
}

// Help opens on ? and any key dismisses it — except quit, which works from
// the overlay too: the emergency exit must be reachable from every screen.
func TestHelpToggle(t *testing.T) {
	app, _, _ := newTestApp()
	press(t, app, tea.KeyPressMsg{Text: "?"})
	if !app.help {
		t.Fatal("? must open the help overlay")
	}
	press(t, app, tea.KeyPressMsg{Text: "g"})
	if app.help {
		t.Fatal("any key must dismiss the help overlay")
	}
	app.help = true
	m := mustRun(t, press(t, app, tea.KeyPressMsg{Text: "q"}))
	if m == nil || app.help {
		t.Fatal("q must quit while the help overlay is open")
	}
	app.help = true
	m = mustRun(t, press(t, app, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}))
	if m == nil || app.help {
		t.Fatal("ctrl+c must quit while the help overlay is open")
	}
}

// The full library round trip: open, browse down to a track, queue it,
// remove it, go back.
func TestLibraryFlow(t *testing.T) {
	app, lp, fl := newTestApp()
	fl.artists = []contract.Artist{{BandID: 1, Name: "Alpha"}, {BandID: 2, Name: "Beta"}}
	fl.albums[1] = []contract.Album{
		{Slug: "s1", Title: "One", BandID: 1},
		{Slug: "s2", Title: "Two", BandID: 1},
	}
	fl.full["s1"] = contract.Album{
		Slug: "s1", Title: "One", BandID: 1,
		Tracks: []contract.Track{
			{ID: "t1", Title: "T1", BandName: "Alpha", AlbumTitle: "One"},
			{ID: "t2", Title: "T2", BandName: "Alpha", AlbumTitle: "One"},
		},
	}
	lp.setState(contract.State{}) // idle: queued tracks start at once

	// l with an empty browser fetches the artists.
	m, ok := mustRun(t, press(t, app, tea.KeyPressMsg{Text: "l"})).(artistsMsg)
	if !ok {
		t.Fatalf("l should fetch artists; got %v", m)
	}
	_, cmd := app.Update(m)
	if app.screen != screenLibrary || len(app.browser.artists) != 2 {
		t.Fatalf("screen = %v, artists = %d; want library / 2", app.screen, len(app.browser.artists))
	}

	// The browser preloads the selected artist's releases.
	ma, ok := mustRun(t, cmd).(albumsMsg)
	if !ok {
		t.Fatalf("after artists load, the browser should fetch albums; got %v", ma)
	}
	_, cmd = app.Update(ma)
	if len(app.browser.albums) != 2 || app.browser.tracksFor != "" {
		t.Fatalf("albums = %d, tracksFor = %q; want 2 / cleared", len(app.browser.albums), app.browser.tracksFor)
	}

	// enter on the artist moves focus to the albums and preloads s1's tracks.
	mt, ok := mustRun(t, press(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})).(tracksMsg)
	if !ok {
		t.Fatalf("enter on an artist should fetch the album; got %v", mt)
	}
	_, _ = app.Update(mt)
	if app.browser.focus != 1 || len(app.browser.tracks) != 2 {
		t.Fatalf("focus = %d, tracks = %d; want 1 / 2", app.browser.focus, len(app.browser.tracks))
	}

	// right to the tracks column, then enter queues the selected track.
	press(t, app, tea.KeyPressMsg{Code: tea.KeyRight})
	if app.browser.focus != 2 {
		t.Fatalf("focus = %d; want the tracks column", app.browser.focus)
	}
	press(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := ids(app.playlist.Queue()); len(got) != 1 || got[0] != "t1" {
		t.Fatalf("queue = %v; want [t1]", got)
	}
	if got := last(ids(lp.queues)); got != "t1" {
		t.Fatalf("Queue calls = %v; an idle engine starts the first queued track", ids(lp.queues))
	}

	// x removes it from the queue again.
	press(t, app, tea.KeyPressMsg{Text: "x"})
	if got := app.playlist.Queue(); len(got) != 0 {
		t.Fatalf("queue = %v; x must remove the selected track", got)
	}

	// l goes back to the player.
	press(t, app, tea.KeyPressMsg{Text: "l"})
	if app.screen != screenPlayer {
		t.Fatalf("screen = %v; want the player", app.screen)
	}
}

// A slow albums response for an artist the user has moved on from is
// dropped rather than rendered over the new selection.
func TestLibraryStaleAlbumsDropped(t *testing.T) {
	app, _, fl := newTestApp()
	fl.artists = []contract.Artist{{BandID: 1, Name: "Alpha"}, {BandID: 2, Name: "Beta"}}
	fl.albums[1] = []contract.Album{{Slug: "s1", Title: "One", BandID: 1}}

	m, ok := mustRun(t, press(t, app, tea.KeyPressMsg{Text: "l"})).(artistsMsg)
	if !ok {
		t.Fatalf("l should fetch artists; got %v", m)
	}
	_, cmd := app.Update(m)
	ma, ok := mustRun(t, cmd).(albumsMsg)
	if !ok {
		t.Fatalf("albums fetch expected; got %v", ma)
	}
	_, _ = app.Update(ma)
	if len(app.browser.albums) != 1 || app.browser.albums[0].Title != "One" {
		t.Fatalf("albums = %v; want Alpha's One", app.browser.albums)
	}

	// The user moves to Beta; then Alpha's stale response arrives.
	app.browser.selArtist = 1
	_, _ = app.Update(albumsMsg{
		artist: fl.artists[0],
		albums: []contract.Album{{Slug: "stale", Title: "Stale", BandID: 1}},
	})
	if got := app.browser.albums[0].Title; got != "One" {
		t.Fatalf("albums[0] = %q; the stale response must not overwrite the column", got)
	}
}

func TestLibraryFetchError(t *testing.T) {
	app, _, _ := newTestApp()
	_, _ = app.Update(artistsMsg{err: errors.New("network down")})
	if !strings.Contains(app.status, "library:") {
		t.Fatalf("status = %q; want the fetch error surfaced", app.status)
	}
	_, _ = app.Update(artistsMsg{artists: []contract.Artist{{Name: "A"}}})
	if len(app.browser.artists) != 1 {
		t.Fatalf("artists = %d; a later success must land", len(app.browser.artists))
	}
}

// A failed load surfaces the error, clears the load gate, and leaves the
// queue intact so the user can try again.
func TestLoadedError(t *testing.T) {
	app, lp, _ := newTestApp()
	lp.loadErr = errors.New("stream 404")
	app.playlist.Add(track("A"))
	lp.setState(contract.State{})

	m := mustRun(t, press(t, app, tea.KeyPressMsg{Code: tea.KeySpace}))
	_, _ = app.Update(m)
	if !strings.Contains(app.status, "playback failed") {
		t.Fatalf("status = %q; want the load failure", app.status)
	}
	if app.pendingLoad {
		t.Fatal("pendingLoad must clear on failure")
	}
	if len(app.playlist.Queue()) != 1 {
		t.Fatal("a failed load must not consume the queue entry")
	}
	if mustRun(t, press(t, app, tea.KeyPressMsg{Code: tea.KeySpace})) == nil {
		t.Fatal("space must retry")
	}
}

// containsTrack reports whether any of ts has the given ID.
func containsTrack(ts []contract.Track, id string) bool {
	for _, t := range ts {
		if t.ID == id {
			return true
		}
	}
	return false
}
