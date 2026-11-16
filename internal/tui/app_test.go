package tui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"bc-radio/internal/contract"
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
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
	scope   []float64
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
		scope:  []float64{},
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

func (f *fakePlayer) Scope() []float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.scope
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

func (f *fakePlayer) setScope(vals ...float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scope = vals
}

// fakeLib serves preset library data and logs which calls arrived.
type fakeLib struct {
	mu       sync.Mutex
	artists  []contract.Artist
	albums   map[int64][]contract.Album
	full     map[string]contract.Album
	tracks   []contract.Track
	err      error
	artistCt int
	albumCt  int
	fullCt   int
	trackCt  int
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

func (f *fakeLib) Tracks(ctx context.Context) ([]contract.Track, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trackCt++
	if f.err != nil {
		return nil, f.err
	}
	return f.tracks, nil
}

// newTestApp builds an app over the fakes, sized 80 × 24.
func newTestApp() (*App, *fakePlayer, *fakeLib) {
	lp, fl := newFakePlayer(), &fakeLib{
		albums: map[int64][]contract.Album{},
		full:   map[string]contract.Album{},
	}
	app := New(fl, lp, "dark")
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
	app := New(&fakeLib{}, newFakePlayer(), "dark")
	if got := len(app.bands); got != contract.BandCount {
		t.Fatalf("bands = %d; want %d", got, contract.BandCount)
	}
	if got := len(app.visualizers); got != 4 {
		t.Fatalf("visualizers = %d; want 4", got)
	}
	if app.theme != themeDark {
		t.Fatalf("theme = %q; want dark", app.theme)
	}
}

func TestNewThemeFallback(t *testing.T) {
	// An unvalidated name must land on the conservative theme, never an
	// empty palette: main is the gate, but New must not be able to break.
	app := New(&fakeLib{}, newFakePlayer(), "nope")
	if app.theme != themeAuto {
		t.Fatalf("theme = %q; want auto for an unknown name", app.theme)
	}
}

func TestThemeResolution(t *testing.T) {
	dark := colorprofile.TrueColor
	ascii := colorprofile.ASCII
	// auto follows the profile: NO_COLOR (ASCII) is mono.
	auto := New(&fakeLib{}, newFakePlayer(), "auto")
	auto.profile = ascii
	auto.applyThemeNow()
	if !current.mono {
		t.Fatal("auto with an ASCII profile must resolve to mono")
	}
	// auto follows the background: a light one is the light palette.
	auto.profile = dark
	auto.bgDark = false
	auto.applyThemeNow()
	if current.mono {
		t.Fatal("auto with a light background must not be mono")
	}
	// An explicit theme never moves: the same inputs keep the dark palette.
	auto.theme = themeDark
	auto.profile = ascii
	auto.bgDark = false
	auto.applyThemeNow()
	if current.mono {
		t.Fatal("an explicit dark theme must ignore the ASCII profile")
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
	// The notice is deliberately short ("bc-radio needs 30 × 8") so it
	// displays in full even below the minimum width: at 20 columns the
	// ellipsis eats only the trailing "× 8".
	if got := stripANSI(v); !strings.Contains(got, "bc-radio") || !strings.Contains(got, "needs 30") {
		t.Fatalf("tiny view should name the app and its minimum size: %q", got)
	}
}

// sample refreshes the position (frozen while paused), the scope samples,
// and the status expiry on every tick; the band frame steps on its own 50 ms
// quantum (specStepPeriod) — fast on the way up (bandRiseRate) and slow on
// the way down (bandFallRate) — and decays to rest when the engine is idle:
// the player reports no current track, so the target is nil and a stale
// non-zero Bands() must not keep the visual alive.
// almost fails the test unless got is within eps of want, for pins derived
// from the easing formula whose float values are not exactly representable.
func almost(t *testing.T, got, want, eps float64) {
	t.Helper()
	if math.Abs(got-want) > eps {
		t.Fatalf("got %v; want %v (±%v)", got, want, eps)
	}
}

func TestSample(t *testing.T) {
	app, lp, _ := newTestApp()
	// Stamp the render clock: each sample() advances it by exactly one
	// frame, so the steps land where the formula says and the pins below
	// hold no matter how slowly the test itself runs.
	clock := time.Unix(0, 0)
	app.now = func() time.Time {
		clock = clock.Add(tickPeriod)
		return clock
	}
	lp.setState(contract.State{Track: track("A"), Playing: true, Position: 30 * time.Second})
	lp.setBands(1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1)
	lp.setScope(0.5, -0.5)

	// The band frame steps on the 50 ms quantum, not on every 16 ms tick:
	// three ticks bank below it and must not move the frame, while the
	// position and scope refresh on every tick.
	app.sample()
	app.sample()
	app.sample()
	if app.bands[0] != 0 {
		t.Fatalf("bands[0] = %v after three ticks; want 0 (below the 50 ms quantum)", app.bands[0])
	}
	if app.pos != 30*time.Second {
		t.Fatalf("pos = %v; want 30s while playing", app.pos)
	}
	// The scope passes the raw post-gain samples through, unrescaled.
	if len(app.scope) != 2 || app.scope[0] != 0.5 || app.scope[1] != -0.5 {
		t.Fatalf("scope = %v; want [0.5 -0.5] (raw post-gain)", app.scope)
	}
	// The fourth tick banks 64 ms: the analyser publishes on a log scale and
	// the app uses it absolute, so the step closes 1−e^(−34·64ms) ≈ 0.8865
	// of the gap from 0.
	app.sample()
	almost(t, app.bands[0], 0.886505397698, 1e-9)

	// The music stops: the frame decays with the fall rate on its own
	// quantum — three ticks bank below it, the fourth steps 64 ms:
	// 0.8865·e^(−10·64ms) ≈ 0.4674. The position freezes.
	lp.setState(contract.State{Track: track("A"), Position: 31 * time.Second})
	lp.setBands() // zero frame
	app.sample()
	app.sample()
	app.sample()
	almost(t, app.bands[0], 0.886505397698, 1e-9)
	if app.pos != 30*time.Second {
		t.Fatalf("pos = %v; must stay frozen while paused", app.pos)
	}
	app.sample()
	almost(t, app.bands[0], 0.467447580079, 1e-9)

	// Engine idle with a stale analyser frame: the target is zero, not the
	// stale frame, and the scope empties with the track. The decay continues
	// on the same quantum: 0.4674·e^(−10·64ms) ≈ 0.2465.
	lp.setState(contract.State{})
	lp.setBands(1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1)
	app.sample()
	app.sample()
	app.sample()
	almost(t, app.bands[0], 0.467447580079, 1e-9)
	if app.scope != nil {
		t.Fatalf("scope = %v; want nil when the engine is idle", app.scope)
	}
	app.sample()
	almost(t, app.bands[0], 0.246481567613, 1e-9)
}

// clampFrameDT bounds the easing step: a missing clock, a backwards gap, or
// a gap of more than ten frames yields exactly one frame, so a pause or a
// stalled terminal moves the visual one frame instead of the whole gap;
// inside the cap the real elapsed time passes through.
func TestClampFrameDT(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	cases := []struct {
		name string
		now  time.Time
		last time.Time
		want time.Duration
	}{
		{"zero now", time.Time{}, t0, tickPeriod},
		{"zero last", t0, time.Time{}, tickPeriod},
		{"zero gap", t0, t0, tickPeriod},
		{"backwards gap", t0, t0.Add(tickPeriod), tickPeriod},
		{"eleven frames", t0.Add(11 * tickPeriod), t0, tickPeriod},
		{"five frames", t0.Add(5 * tickPeriod), t0, 5 * tickPeriod},
		{"ten frames, cap inclusive", t0.Add(10 * tickPeriod), t0, 10 * tickPeriod},
	}
	for _, c := range cases {
		if got := clampFrameDT(c.now, c.last); got != c.want {
			t.Fatalf("%s: clampFrameDT = %v; want %v", c.name, got, c.want)
		}
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

// Space on an idle engine with a queue starts the head. The key is the
// legacy decode shape (Text " ") — the form the space bar actually arrives
// in, and the one that used to fall through to actNone.
func TestSpaceStartsQueueHead(t *testing.T) {
	app, lp, _ := newTestApp()
	app.playlist.Add(track("A"))
	lp.setState(contract.State{})

	cmd := press(t, app, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
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

// runMsgs runs cmd and returns the messages it produced. A batch (several
// commands, as when the index build is batched with a browser fetch) runs
// every sub-command and yields them all; a plain command yields one.
func runMsgs(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	m := mustRun(t, cmd)
	if batch, ok := m.(tea.BatchMsg); ok {
		out := make([]tea.Msg, len(batch))
		for i, c := range batch {
			out[i] = c()
		}
		return out
	}
	return []tea.Msg{m}
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
	// The radio is on by default but the index is empty, so space finds
	// nothing to start.
	press(t, app, tea.KeyPressMsg{Code: tea.KeySpace})
	if !strings.Contains(app.status, "nothing to play") {
		t.Fatalf("status = %q; want the nothing-to-play hint", app.status)
	}
	// With the radio off the hint also names how to turn it back on.
	app.playlist.radio = false
	press(t, app, tea.KeyPressMsg{Code: tea.KeySpace})
	if !strings.Contains(app.status, "radio off") {
		t.Fatalf("status = %q; want the radio-off hint", app.status)
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
	if app.visIdx != 1 || app.status != "visualizer: particles" {
		t.Fatalf("visIdx = %d, status = %q; want 1 / particles", app.visIdx, app.status)
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

	// The browser preloads the selected artist's releases; the radio's index
	// build starts in the same batch.
	if app.indexState != indexBuilding {
		t.Fatalf("indexState = %v; the index build must start when the follows land", app.indexState)
	}
	var ma albumsMsg
	for _, x := range runMsgs(t, cmd) {
		if am, ok := x.(albumsMsg); ok {
			ma = am
		}
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
	var ma albumsMsg
	for _, x := range runMsgs(t, cmd) {
		if am, ok := x.(albumsMsg); ok {
			ma = am
		}
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

// --- radio ---

// d toggles the radio on and off; the status line confirms.
func TestRadioToggleKey(t *testing.T) {
	app, _, _ := newTestApp()
	if !app.playlist.radio {
		t.Fatal("the radio must be on by default")
	}
	press(t, app, tea.KeyPressMsg{Text: "d"})
	if app.playlist.radio || app.status != "radio off" {
		t.Fatalf("radio = %v, status = %q; want off / \"radio off\"", app.playlist.radio, app.status)
	}
	press(t, app, tea.KeyPressMsg{Text: "d"})
	if !app.playlist.radio || app.status != "radio on" {
		t.Fatalf("radio = %v, status = %q; want on / \"radio on\"", app.playlist.radio, app.status)
	}
}

// indexMsg lands the radio's flat track index in all three outcomes: a
// clean build, a walk that failed with nothing (the radio has no pool), and
// a walk that failed after reading some (a partial index is still fuel).
func TestIndexMsg(t *testing.T) {
	app, _, _ := newTestApp()
	app.indexState = indexBuilding

	_, _ = app.Update(indexMsg{tracks: []contract.Track{
		{ID: "a1", BandID: 1}, {ID: "a2", BandID: 1}, {ID: "b1", BandID: 2},
	}})
	if app.indexState != indexReady {
		t.Fatalf("indexState = %v; want ready", app.indexState)
	}
	if got := app.status; got != "track index: 3 tracks from 2 bands" {
		t.Fatalf("status = %q", got)
	}
	if len(app.playlist.Index()) != 3 || app.playlist.IndexBands() != 2 {
		t.Fatalf("index = %d tracks / %d bands; want 3 / 2", len(app.playlist.Index()), app.playlist.IndexBands())
	}

	_, _ = app.Update(indexMsg{err: errors.New("network down")})
	if app.indexState != indexFailed {
		t.Fatalf("indexState = %v; a failed empty walk is failed", app.indexState)
	}
	if got := app.status; got != "track index unavailable: network down" {
		t.Fatalf("status = %q", got)
	}
	if len(app.playlist.Index()) != 0 {
		t.Fatalf("index = %d; a failed empty walk clears the index", len(app.playlist.Index()))
	}

	_, _ = app.Update(indexMsg{
		tracks: []contract.Track{{ID: "a1", BandID: 1}},
		err:    errors.New("one band 404"),
	})
	if app.indexState != indexReady {
		t.Fatalf("indexState = %v; a partial index is ready", app.indexState)
	}
	if got := app.status; got != "track index: 1 tracks — one band 404" {
		t.Fatalf("status = %q", got)
	}
}

// A partial follow list (the >20 batch cap) still lands the artists it read,
// and the index build still walks them; the note is surfaced, not fatal.
func TestArtistsPartialAcceptsBatchCap(t *testing.T) {
	app, _, _ := newTestApp()
	artists := make([]contract.Artist, 20)
	for i := range artists {
		artists[i] = contract.Artist{BandID: int64(i + 1), Name: fmt.Sprintf("Band %d", i+1)}
	}
	_, cmd := app.Update(artistsMsg{
		artists: artists,
		err:     errors.New("19 of 34 follows readable (batch cap)"),
	})
	if len(app.browser.artists) != 20 {
		t.Fatalf("artists = %d; a partial list still lands", len(app.browser.artists))
	}
	if !strings.Contains(app.status, "library:") {
		t.Fatalf("status = %q; the partial note must be surfaced", app.status)
	}
	if app.indexState != indexBuilding {
		t.Fatalf("indexState = %v; the readable artists still walk", app.indexState)
	}
	var sawAlbums, sawIndex bool
	for _, x := range runMsgs(t, cmd) {
		if _, ok := x.(albumsMsg); ok {
			sawAlbums = true
		}
		if _, ok := x.(indexMsg); ok {
			sawIndex = true
		}
	}
	if !sawAlbums || !sawIndex {
		t.Fatalf("the batch must hold the browser preload and the index build (albums = %v, index = %v)",
			sawAlbums, sawIndex)
	}
}

// Space on an idle engine with the radio on and a built index starts a
// random index track (a cold start draws from the index, not the pool).
func TestSpaceIdleRadioStarts(t *testing.T) {
	app, lp, _ := newTestApp()
	app.playlist.SetIndex([]contract.Track{track("A"), track("B")})
	lp.setState(contract.State{}) // idle
	app.state = lp.State()

	m, ok := mustRun(t, press(t, app, tea.KeyPressMsg{Code: tea.KeySpace})).(loadedMsg)
	if !ok {
		t.Fatalf("space should load a radio track; got %v", m)
	}
	if m.err != nil {
		t.Fatalf("load err = %v", m.err)
	}
	want := map[string]bool{"A": true, "B": true}
	if !want[m.track.ID] {
		t.Fatalf("radio start = %q; want an index track", m.track.ID)
	}
	if !app.pendingLoad {
		t.Fatal("pendingLoad must be set while the load is in flight")
	}
	if !strings.Contains(app.status, "play:") {
		t.Fatalf("status = %q", app.status)
	}

	_, _ = app.Update(m)
	if app.pendingLoad {
		t.Fatal("pendingLoad must clear when the load lands")
	}
	if got := app.playlist.Current().ID; got != m.track.ID {
		t.Fatalf("current = %q; want %q", got, m.track.ID)
	}
}

// A track ends while the engine sits idle: the radio draws the next from
// the index, avoiding the track that just ended.
func TestTrackEndedIdleRadioAdvance(t *testing.T) {
	app, lp, _ := newTestApp()
	app.playlist.SetIndex([]contract.Track{track("A"), track("B"), track("C")})
	app.playlist.SetCurrent(track("C"))
	lp.setState(contract.State{}) // idle: the engine's own advance exhausted

	_, _ = app.Update(eventMsg{ev: contract.Event{Kind: contract.EventTrackEnded, Track: track("C")}})

	want := map[string]bool{"A": true, "B": true}
	if got := last(ids(lp.queues)); !want[got] {
		t.Fatalf("Queue calls = %v; the radio must avoid C and pick from the index", ids(lp.queues))
	}
	if got := app.playlist.Current().ID; got != "" {
		t.Fatalf("current = %q; C ended, so the playlist must be idle", got)
	}
	if !strings.Contains(app.status, "now:") {
		t.Fatalf("status = %q", app.status)
	}
}

// queueAlbumBrowser arranges the library screen on an album with the given
// tracks column, for the queue-all tests.
func queueAlbumBrowser(app *App, tracksFor string, tracks ...contract.Track) {
	app.screen = screenLibrary
	app.browser.albumArtist = contract.Artist{BandID: 1, Name: "Alpha"}
	app.browser.albums = []contract.Album{{Slug: "s1", Title: "One", BandID: 1}}
	app.browser.tracksFor = tracksFor
	app.browser.tracks = tracks
	app.browser.focus = 1
}

func albumTracks() []contract.Track {
	return []contract.Track{
		{ID: "t1", Title: "T1", BandName: "Alpha", AlbumTitle: "One"},
		{ID: "t2", Title: "T2", BandName: "Alpha", AlbumTitle: "One"},
	}
}

// Enter on an album with a fresh tracks column queues every track at once;
// an idle engine starts the first.
func TestQueueAlbumFreshColumn(t *testing.T) {
	app, lp, _ := newTestApp()
	queueAlbumBrowser(app, "s1", albumTracks()...)
	lp.setState(contract.State{}) // idle
	app.state = lp.State()

	press(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := ids(app.playlist.Queue()); len(got) != 2 || got[0] != "t1" || got[1] != "t2" {
		t.Fatalf("queue = %v; want [t1 t2]", got)
	}
	if app.status != "queued 2 tracks from One" {
		t.Fatalf("status = %q", app.status)
	}
	if got := last(ids(lp.queues)); got != "t1" || app.engineNextID != "t1" {
		t.Fatalf("Queue calls = %v, slot = %q; the idle engine must start t1", ids(lp.queues), app.engineNextID)
	}
}

// Enter on an album dedups against the queue and the current track; a
// second enter finds nothing new.
func TestQueueAlbumDedup(t *testing.T) {
	app, lp, _ := newTestApp()
	queueAlbumBrowser(app, "s1", albumTracks()...)
	t1 := albumTracks()[0]
	app.playlist.SetCurrent(t1)
	lp.setState(contract.State{Track: t1, Playing: true}) // t1 is playing
	app.state = lp.State()

	press(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := ids(app.playlist.Queue()); len(got) != 1 || got[0] != "t2" {
		t.Fatalf("queue = %v; t1 is playing, so only t2 queues", got)
	}
	if app.status != "queued 1 tracks from One" {
		t.Fatalf("status = %q", app.status)
	}
	if got := last(ids(lp.queues)); got != "t2" || app.engineNextID != "t2" {
		t.Fatalf("Queue calls = %v, slot = %q; t2 must be the prepared next", ids(lp.queues), app.engineNextID)
	}

	press(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.status != "every track of One is already queued or playing" {
		t.Fatalf("status = %q", app.status)
	}
	if got := last(ids(lp.queues)); got != "t2" {
		t.Fatalf("Queue calls = %v; nothing new must be prepared", ids(lp.queues))
	}
}

// Enter on an album with a stale tracks column records the intent and
// queues when the refetch the browser issues lands.
func TestQueueAlbumStaleRefetch(t *testing.T) {
	app, lp, fl := newTestApp()
	fl.full["s1"] = contract.Album{Slug: "s1", Title: "One", BandID: 1, Tracks: albumTracks()}
	queueAlbumBrowser(app, "stale",
		contract.Track{ID: "zz", Title: "Zz", BandName: "Other", AlbumTitle: "Else"})
	lp.setState(contract.State{}) // idle
	app.state = lp.State()

	cmd := press(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.browser.queueAllFor != "s1" {
		t.Fatalf("queueAllFor = %q; want s1 (the tracks column is stale)", app.browser.queueAllFor)
	}
	m, ok := mustRun(t, cmd).(tracksMsg)
	if !ok || m.err != nil {
		t.Fatalf("the refetch must run and succeed; got %v (%v)", m, m.err)
	}
	_, _ = app.Update(m)

	if got := ids(app.playlist.Queue()); len(got) != 2 || got[0] != "t1" || got[1] != "t2" {
		t.Fatalf("queue = %v; the pending queue-all must land with the refetch", got)
	}
	if app.browser.queueAllFor != "" {
		t.Fatal("queueAllFor must be consumed")
	}
	if app.status != "queued 2 tracks from One" {
		t.Fatalf("status = %q", app.status)
	}
	if app.browser.tracksFor != "s1" || len(app.browser.tracks) != 2 {
		t.Fatalf("tracksFor = %q, tracks = %d; the column must refresh to s1", app.browser.tracksFor, len(app.browser.tracks))
	}
	if got := last(ids(lp.queues)); got != "t1" {
		t.Fatalf("Queue calls = %v; the idle engine must start t1", ids(lp.queues))
	}
}

// And a failed refetch reports the queue-all failure and clears the intent.
func TestQueueAlbumStaleRefetchFails(t *testing.T) {
	app, lp, fl := newTestApp()
	fl.err = errors.New("bandcamp down")
	queueAlbumBrowser(app, "stale",
		contract.Track{ID: "zz", Title: "Zz", BandName: "Other", AlbumTitle: "Else"})
	lp.setState(contract.State{}) // idle
	app.state = lp.State()

	cmd := press(t, app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.browser.queueAllFor != "s1" {
		t.Fatalf("queueAllFor = %q; want s1", app.browser.queueAllFor)
	}
	m, ok := mustRun(t, cmd).(tracksMsg)
	if !ok || m.err == nil {
		t.Fatalf("want a failed tracksMsg; got %v", m)
	}
	_, _ = app.Update(m)

	if app.status != "queueing s1: bandcamp down" {
		t.Fatalf("status = %q; the queue-all failure must name the album", app.status)
	}
	if app.browser.queueAllFor != "" {
		t.Fatal("queueAllFor must be consumed on failure too")
	}
	if got := ids(lp.queues); len(got) != 0 {
		t.Fatalf("Queue calls = %v; nothing must be prepared", got)
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
