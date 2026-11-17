package tui

import (
	"context"
	"fmt"
	"math"
	"time"

	"charm.land/bubbletea/v2"
	"github.com/ali5ter/bc-radio/internal/contract"
	"github.com/charmbracelet/colorprofile"
)

// tickPeriod is the render cadence: 16 ms ≈ 60 fps, the PRD's visual
// target. The program also re-renders on every event, so the tick is the
// floor for animation, not a ceiling.
const tickPeriod = 16 * time.Millisecond

// specStepPeriod is the cadence the spectrum's band state steps: 50 ms,
// about 20 Hz, the quantum cliamp animates its spectrum at (its TickFast).
// The program still renders at 60 fps — the peak caps and the scope move on
// every frame — but the bands ease once per
// quantum, so a rendered step closes 1−e^(−rate·50ms) of the gap (82%
// rising, 39% falling) instead of the 60 fps share (43%/16%). The travel
// per second is the same either way; what the eye reads is the size of each
// rendered step, and the bigger steps are what make the bars read as fast.
const specStepPeriod = 50 * time.Millisecond

// statusTTL is how long a status message lingers before fading.
const statusTTL = 8 * time.Second

// briefTTL is the linger for switch confirmations (radio, visualizer,
// volume, play/pause): 3 s is long enough to read the acknowledgement, and
// short enough that the standing line is back before the row reads as
// stale. The 8 s default would keep the radio line out of view for most of
// the confirmation's life (issue #4).
const briefTTL = 3 * time.Second

// bandRiseRate and bandFallRate are the visualizer's easing rates in levels
// per second: each spectrum step closes the gap to the target by
// 1−e^(−rate·dt), the same exponential ease cliamp uses, with cliamp's own
// rates. They are deliberately asymmetric — sound energy arrives and leaves
// at different speeds, and the eye reads a fast rise with a slower fall as
// "responsive" where an equal rate reads as "laggy". At the 50 ms step a
// 34/s rise closes 82% of a new level per step (98% in about 115 ms of wall
// clock), and a 10/s fall decays a vanished peak 39% per step (to 2% in
// about 390 ms), so transients track the music and the fall keeps a visible
// tail instead of a hard cut.
const (
	bandRiseRate      = 34.0
	bandFallRate      = 10.0
	maxSmoothDtFrames = 10 // the cap clampFrameDT applies to the elapsed gap, in frames
)

// screen selects the visible screen.
type screen int

const (
	screenPlayer screen = iota
	screenLibrary
)

// indexState is the radio's track-index lifecycle: built once, as soon as
// the follow list lands, and not rebuilt mid-session — the page cache keeps
// the signed stream URLs alive, and new follows do not happen while a
// session is running.
type indexState int

const (
	indexIdle     indexState = iota // not started
	indexBuilding                   // a fetchIndex is in flight
	indexReady                      // the playlist holds the index
	indexFailed                     // the walk failed with nothing: the radio has no pool
)

// Messages internal to the app (the library's fetch messages live in
// library.go).
type (
	// tickMsg fires every tickPeriod while the program runs.
	tickMsg struct{}
	// eventMsg carries one player event off the engine's channel.
	eventMsg struct{ ev contract.Event }
	// loadedMsg reports the result of a background Load.
	loadedMsg struct {
		track contract.Track
		err   error
	}
)

// App is the bubbletea model for bc-radio: it consumes contract.Library
// and contract.Player, owns the queue policy, and renders the two screens
// (player and library). All engine mutation goes through the contract;
// Load runs in a goroutine so a stream fetch never blocks the renderer.
type App struct {
	library  contract.Library
	player   contract.Player
	playlist *playlist
	browser  *browser

	bands       []float64
	scope       []float64 // the analyser's recent samples, for the scope visualizer
	state       contract.State
	pos         time.Duration
	visualizers []visualizer
	visIdx      int

	theme   themeKind            // the user's -theme choice
	profile colorprofile.Profile // the terminal's colour profile (the program reports it)
	bgDark  bool                 // the terminal's background, from the colour query

	width      int
	height     int
	fullscreen bool
	help       bool
	screen     screen
	status     string
	statusKind statusKind
	statusAt   time.Time
	statusFor  time.Duration // how long this transient lingers before fading
	statusOn   screen        // the screen the transient was raised on

	now        func() time.Time // the clock sample() reads; tests stamp it
	lastSample time.Time        // the clock reading of the previous sample()
	specDT     time.Duration    // wall clock banked since the last spectrum step

	engineNextID string         // the track in the engine's single pending slot
	pendingLoad  bool           // a Load is in flight; transport actions wait
	skipFrom     contract.Track // abandoned by a skip/prev, recorded on success
	indexState   indexState     // the radio's track-index lifecycle
	indexDone    int            // the walk's finished artists, from contract.Indexer
	indexTotal   int            // the walk's total artists, from contract.Indexer
	spin         int            // the spinner frame, advanced while the index builds
}

// New builds an App over a library and a player. themeName is the -theme
// choice (auto, dark, light or mono); main validates it, and a kind New does
// not recognise falls back to auto rather than an empty palette. The player
// is not started here; Run drives the program and stops the player on exit.
func New(library contract.Library, player contract.Player, themeName string) *App {
	kind := themeKind(themeName)
	if kind != themeAuto && kind != themeDark && kind != themeLight && kind != themeMono {
		kind = themeAuto
	}
	a := &App{
		library:     library,
		player:      player,
		playlist:    newPlaylist(),
		browser:     newBrowser(),
		bands:       make([]float64, contract.BandCount),
		now:         time.Now, // the render clock; sample() eases against it
		state:       player.State(),
		visualizers: makeVisualizers(),
		theme:       kind,
		// The program reports the real profile and background colour as
		// soon as it starts; these are the guesses the first frame uses.
		// TrueColor is the conservative colour bet (the renderer re-encodes
		// per profile, so the only cost is the auto theme's one-frame guess)
		// and dark is the common background.
		profile: colorprofile.TrueColor,
		bgDark:  true,
	}
	a.applyThemeNow()
	return a
}

// applyThemeNow resolves the theme from the current profile and background
// observations and installs the palette.
func (a *App) applyThemeNow() {
	applyTheme(resolveScheme(a.theme, a.profile, a.bgDark))
}

// Init implements tea.Model: start the render tick, the event pump, and
// the follow-list fetch. The follow list is fetched at startup (not on the
// first l, as before) so the radio's track index can start building while
// the user is still reading the player screen. The background-colour query
// answers the auto theme: the reply is a terminal event, not a key, so it
// only matters when the theme can still move.
func (a *App) Init() tea.Cmd {
	if a.theme != themeAuto {
		return tea.Batch(a.tick(), a.watch(), fetchArtists(a.library))
	}
	requestBg := func() tea.Msg { return tea.RequestBackgroundColor() }
	return tea.Batch(a.tick(), a.watch(), fetchArtists(a.library), requestBg)
}

// Update implements tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		a.browser.viewH = a.height - 3 // title, columns, help
		return a, nil

	case tea.BackgroundColorMsg:
		// The terminal answered Init's query; only the auto theme moves.
		a.bgDark = m.IsDark()
		if a.theme == themeAuto {
			a.applyThemeNow()
		}
		return a, nil

	case tea.ColorProfileMsg:
		// The program reports the terminal's colour profile at start and
		// again when it is upgraded. Only the auto theme moves: NO_COLOR
		// collapses the profile to ASCII, which resolves to mono.
		a.profile = m.Profile
		if a.theme == themeAuto {
			a.applyThemeNow()
		}
		return a, nil

	case tickMsg:
		a.sample()
		if a.indexState == indexBuilding {
			// Keep the building line alive while the walk runs: pull the
			// counter and roll the spinner, so the screen moves instead
			// of reading as hung.
			a.pollIndexProgress()
			a.spin = (a.spin + 1) % len(spinFrames)
		}
		return a, a.tick()

	case eventMsg:
		a.onEvent(m.ev)
		return a, a.watch()

	case loadedMsg:
		a.pendingLoad = false
		skipFrom := a.skipFrom
		a.skipFrom = contract.Track{}
		if m.err != nil {
			a.statusfBad("playback failed: %v", m.err)
			return a, nil
		}
		a.playlist.recordPlayed(skipFrom)
		a.playlist.SetCurrent(m.track)
		a.engineNextID = ""
		a.pos = 0
		a.statusf("now: %s — %s", m.track.BandName, m.track.Title)
		a.syncNext()
		return a, nil

	case artistsMsg:
		// A partial list (the >20 batch-cap note) still lands the artists
		// it read: only an empty failure leaves the library empty.
		a.browser.artists = m.artists
		a.browser.selArtist = 0
		a.browser.scroll[0] = 0
		// The follow list failing is a near-success when it still read some
		// (the >20 batch-cap note) and a hard failure only when it read
		// none: a partial list is still fuel for both the browser and the index.
		if m.err != nil {
			if len(m.artists) > 0 {
				a.statusfWarn("library: %v", m.err)
			} else {
				a.statusfBad("library: %v", m.err)
				// A read that came back empty also fails the index:
				// without this the standing line would keep reading
				// "building track index…" forever after the red
				// transient fades (the walk never started, because it
				// needs artists to walk). A working index is never
				// demoted by a stray read.
				if a.indexState != indexReady && a.indexState != indexBuilding {
					a.indexState = indexFailed
				}
			}
		} else {
			a.statusf("%d artists", len(m.artists))
		}
		// The index is the radio's fuel: build it as soon as there are
		// follows. The batch cap's partial note is not a stop — the 20
		// readable artists still walk. A failed state restarts the walk
		// on the next read: a transient network failure at startup must
		// not permanently disable the radio when the l refetch recovers.
		cmd := a.browserCmd()
		if (a.indexState == indexIdle || a.indexState == indexFailed) && len(m.artists) > 0 {
			a.indexState = indexBuilding
			cmd = tea.Batch(cmd, fetchIndex(a.library))
		}
		return a, cmd

	case albumsMsg:
		if m.err != nil {
			a.statusfBad("albums: %v", m.err)
			return a, nil
		}
		// Drop stale responses: the selection has moved on.
		if a.browser.selArtist < len(a.browser.artists) &&
			a.browser.artists[a.browser.selArtist].BandID == m.artist.BandID {
			a.browser.albums = m.albums
			a.browser.albumArtist = m.artist
			a.browser.albumsFor = m.artist.BandID
			a.browser.selAlbum = 0
			a.browser.scroll[1] = 0
			a.browser.tracks = nil
			a.browser.tracksFor = ""
		}
		return a, a.browserCmd()

	case tracksMsg:
		if m.err != nil {
			if a.browser.queueAllFor == m.slug {
				a.browser.queueAllFor = ""
				a.statusfBad("queueing %s: %v", m.slug, m.err)
			} else {
				a.statusfBad("tracks: %v", m.err)
			}
			return a, nil
		}
		// A pending queue-all (enter on a stale albums column) is
		// consumed when its refetch lands, even if the selection has
		// moved on: the user asked for that album explicitly.
		if a.browser.queueAllFor == m.slug {
			a.browser.queueAllFor = ""
			a.queueAlbumTracks(m.album.Tracks)
		}
		if a.browser.selAlbum < len(a.browser.albums) &&
			a.browser.albums[a.browser.selAlbum].Slug == m.album.Slug {
			a.browser.tracks = m.album.Tracks
			a.browser.tracksFor = m.album.Slug
			a.browser.selTrack = 0
			a.browser.scroll[2] = 0
		}
		return a, nil

	case indexMsg:
		a.indexDone, a.indexTotal = 0, 0
		a.playlist.SetIndex(m.tracks)
		switch {
		case m.err != nil && len(m.tracks) == 0:
			a.indexState = indexFailed
			a.statusfBad("track index unavailable: %v", m.err)
		case m.err != nil:
			a.indexState = indexReady
			// A partial walk is a near-success: the index is the union of what
			// could be read, and the error names the gaps — a glance, not an
			// alarm. Red is reserved for the walk that read nothing.
			a.statusfWarn("track index: %d tracks — %v", len(m.tracks), m.err)
		default:
			a.indexState = indexReady
			a.statusf("track index: %d tracks from %d bands", len(m.tracks), a.playlist.IndexBands())
		}
		return a, nil

	case tea.KeyPressMsg:
		act := actionFor(m)
		if a.help {
			// Any key dismisses the overlay — except quit: q and ctrl+c
			// leave the app at once, so the emergency exit works from
			// every screen.
			a.help = false
			if act == actQuit {
				return a, quit()
			}
			return a, nil
		}
		return a, a.handleKey(act)
	}
	return a, nil
}

// View implements tea.Model.
func (a *App) View() tea.View {
	var s string
	switch {
	case a.help:
		s = a.viewHelp()
	case a.screen == screenLibrary:
		s = a.viewLibrary()
	default:
		s = a.viewPlayer()
	}
	v := tea.NewView(s)
	v.AltScreen = true
	return v
}

// Run starts the program on the terminal. When the user quits, the player
// is stopped and Run returns the program's error (nil on a clean exit).
func (a *App) Run() error {
	p := tea.NewProgram(a, tea.WithFPS(60))
	_, err := p.Run()
	a.player.Stop(context.Background())
	return err
}

// tick re-arms the render cadence.
func (a *App) tick() tea.Cmd {
	return tea.Tick(tickPeriod, func(time.Time) tea.Msg { return tickMsg{} })
}

// watch pumps one player event at a time; it re-arms after each so events
// apply in order on the model thread.
func (a *App) watch() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-a.player.Events()
		if !ok {
			return nil // the engine stopped; the program exits on its own
		}
		return eventMsg{ev: ev}
	}
}

// clampFrameDT returns the elapsed time between two sample() clock readings,
// capped for easing. A missing clock, a non-positive gap, or a gap of more
// than maxSmoothDtFrames frames yields exactly one frame: a pause, a sleep
// or a stalled terminal moves the visual one frame's worth instead of the
// whole gap, so the bars resume where they left off rather than jumping.
func clampFrameDT(now, last time.Time) time.Duration {
	dt := tickPeriod
	if !now.IsZero() && !last.IsZero() {
		dt = now.Sub(last)
	}
	if dt <= 0 || dt > maxSmoothDtFrames*tickPeriod {
		return tickPeriod
	}
	return dt
}

// sample refreshes the display state once per tick: the position (frozen
// while paused), the scope samples, and the status expiry every tick, and the
// spectrum's band frame on its own 50 ms quantum (specStepPeriod).
//
// The engine's analyser publishes levels on a log (dB) scale in [0,1], so
// the display uses them absolute: a loud passage reads tall and a quiet one
// short, dynamics and all. The band frame eases toward them with the
// asymmetric rise/fall rates, scaled by the elapsed time banked since the
// last step, and the scope passes the raw post-gain samples through — the
// region is fixed-scale, so the music's own loudness is what the eye sees.
//
// Paused, the device's buffer fills and pins the analyser's anchor, so it
// stops publishing and the display holds its last frame — the familiar
// pause cue. Idle (no track) the target is nil and the scope empties at
// once, and the easing carries the bands to rest.
func (a *App) sample() {
	now := a.now()
	dt := clampFrameDT(now, a.lastSample)
	a.lastSample = now
	st := a.player.State()
	a.state = st
	if st.Playing {
		a.pos = st.Position
	}
	target := a.player.Bands()
	var scope []float64
	if st.Track.ID == "" {
		// Engine idle: no target, no samples — the visual decays to rest.
		target = nil
	} else {
		scope = a.player.Scope()
	}
	a.scope = scope
	// The spectrum steps on its own quantum: the wall clock banks on every
	// tick, and once the quantum is full the bands ease once, by the real
	// elapsed time — the motion's speed is the wall clock's, and each
	// rendered step carries a whole quantum of travel, which is what the eye
	// reads as "fast".
	a.specDT += dt
	if a.specDT >= specStepPeriod {
		sec := float64(a.specDT) / float64(time.Second)
		a.specDT = 0
		// Asymmetric easing: rises close fast, falls with a tail.
		alphaRise := 1 - math.Exp(-bandRiseRate*sec)
		alphaFall := 1 - math.Exp(-bandFallRate*sec)
		for k := range a.bands {
			t := 0.0
			if target != nil && k < len(target) {
				t = target[k]
			}
			alpha := alphaFall
			if t > a.bands[k] {
				alpha = alphaRise
			}
			a.bands[k] += (t - a.bands[k]) * alpha
		}
	}
	// The IsZero guard keeps tests that assign the status fields directly
	// (without a stamp from setStatus) out of the expiry: an unstamped
	// status lingers until something stamps it.
	if a.status != "" && !a.statusAt.IsZero() && now.Sub(a.statusAt) > a.statusFor {
		a.status = ""
		a.statusKind = statusInfo
	}
}

// statusKind is the severity of a status message. It is set structurally, at
// the point the message is raised, from what actually happened — never inferred
// from the message's words. Keying a colour off keywords was brittle in both
// directions: a near-success phrased with a failure word (a partial index that
// names an "unavailable" band) read as a hard failure, the only saturated colour
// on the idle screen, drawing the eye to the one non-issue; and a real failure
// phrased without the keywords read as quiet.
type statusKind int

// The three status severities, quiet to loud.
const (
	statusInfo statusKind = iota // normal operation: the quiet dim line
	statusWarn                   // a near-success: a gap to glance at, amber
	statusBad                    // a hard failure: the alarm, red
)

// statusf sets the transient status line at the default (info) severity and
// the default linger (statusTTL).
func (a *App) statusf(format string, args ...any) {
	a.setStatus(statusInfo, fmt.Sprintf(format, args...), statusTTL)
}

// statusfWarn sets the transient status line at the warning severity: a
// near-success that needs a glance, not an alarm (a partial index, a partial
// follow list).
func (a *App) statusfWarn(format string, args ...any) {
	a.setStatus(statusWarn, fmt.Sprintf(format, args...), statusTTL)
}

// statusfBad sets the transient status line at the failure severity: a hard
// failure the user must notice.
func (a *App) statusfBad(format string, args ...any) {
	a.setStatus(statusBad, fmt.Sprintf(format, args...), statusTTL)
}

// statusfBrief sets a switch confirmation (radio, visualizer, volume,
// play/pause) at the info severity with the brief linger: it acknowledges an
// action the user just took and gives the standing line its row back after
// briefTTL instead of holding it for the full 8 s.
func (a *App) statusfBrief(format string, args ...any) {
	a.setStatus(statusInfo, fmt.Sprintf(format, args...), briefTTL)
}

// setStatus records a transient status line: its severity, its linger (ttl),
// its expiry clock, and the screen it was raised on — the line is only
// shown on that screen, so a player confirmation never surfaces in the
// library title row and vice versa.
func (a *App) setStatus(kind statusKind, text string, ttl time.Duration) {
	a.status = text
	a.statusKind = kind
	a.statusFor = ttl
	a.statusAt = a.now()
	a.statusOn = a.screen
}

// pollIndexProgress copies the walk's counter from a library that can
// report it (contract.Indexer), so the building line can show
// "9/17 bands". A library that cannot report leaves the counter at zero
// and the line stays plain.
func (a *App) pollIndexProgress() {
	progress, ok := a.library.(contract.Indexer)
	if !ok {
		return
	}
	a.indexDone, a.indexTotal = progress.IndexProgress()
}

// syncNext fills the engine's single pending slot with the queue head, so
// the engine can advance gaplessly when the current track ends. The engine
// holds exactly one prepared track — a second Queue would supersede it — so
// this acts only when the slot is empty. When the engine is idle, the same
// call starts the head at once (contract: Queue on an idle player starts).
func (a *App) syncNext() {
	if a.engineNextID != "" {
		return
	}
	if q := a.playlist.Queue(); len(q) > 0 {
		a.player.Queue(q[0])
		a.engineNextID = q[0].ID
	}
}

// loadCmd starts t in the background: the stream fetch must not block the
// renderer. Bookkeeping happens in loadedMsg.
func (a *App) loadCmd(t contract.Track) tea.Cmd {
	return func() tea.Msg {
		err := a.player.Load(context.Background(), t)
		return loadedMsg{track: t, err: err}
	}
}

// onEvent applies a player event to the playlist and the engine's pending
// slot. The engine advances by itself when it has a prepared next, so the
// app must tell the two cases apart via State: a non-zero track after the
// event means the engine moved, a zero track means the app must pick.
func (a *App) onEvent(ev contract.Event) {
	st := a.player.State()
	switch ev.Kind {
	case contract.EventTrackStarted:
		a.pos = 0
		a.playlist.SetCurrent(ev.Track)
		if ev.Track.ID == a.engineNextID {
			a.engineNextID = "" // the engine consumed its pending slot
		}
		a.statusf("now: %s — %s", ev.Track.BandName, ev.Track.Title)
		a.syncNext()
	case contract.EventTrackEnded:
		if st.Track.ID != "" {
			// The engine advanced to its prepared track; the TrackStarted
			// that follows syncs the playlist.
			a.syncNext()
			return
		}
		a.autoAdvance(ev.Track, nil)
	case contract.EventPlaybackFailed:
		if st.Track.ID != "" {
			a.syncNext()
			return
		}
		a.statusfBad("playback failed: %v", ev.Err)
		a.autoAdvance(ev.Track, ev.Err)
	}
}

// autoAdvance picks the next track for an idle engine and starts it. A
// failed track is never retried: with a one-track pool the policy would
// hand it straight back, so that case ends the queue instead.
func (a *App) autoAdvance(ended contract.Track, failErr error) {
	a.playlist.OnEnded()
	t, ok := a.playlist.Next()
	if !ok {
		a.statusf("end of queue")
		return
	}
	if failErr != nil && t.ID == ended.ID {
		a.statusfBad("playback failed: %v", failErr)
		return
	}
	a.statusf("now: %s — %s", t.BandName, t.Title)
	a.player.Queue(t) // idle engine: starts at once
	a.engineNextID = ""
}

// handleKey dispatches a key action to the right screen.
func (a *App) handleKey(act action) tea.Cmd {
	switch act {
	case actHelp:
		a.help = !a.help
		return nil
	case actQuit:
		return quit()
	}
	if a.screen == screenLibrary {
		return a.handleLibraryKey(act)
	}
	return a.handlePlayerKey(act)
}

// handlePlayerKey dispatches transport and view actions on the player
// screen.
func (a *App) handlePlayerKey(act action) tea.Cmd {
	switch act {
	case actPlayPause:
		return a.togglePlayPause()
	case actNext:
		return a.skip()
	case actPrev:
		return a.prev()
	case actShuffle:
		a.playlist.shuffle = !a.playlist.shuffle
		a.statusfBrief("shuffle %s", onOff(a.playlist.shuffle))
	case actRepeat:
		a.playlist.repeat = !a.playlist.repeat
		a.statusfBrief("repeat %s", onOff(a.playlist.repeat))
	case actRadio:
		a.playlist.radio = !a.playlist.radio
		a.statusfBrief("radio %s", onOff(a.playlist.radio))
	case actVisual:
		a.visIdx = (a.visIdx + 1) % len(a.visualizers)
		a.statusfBrief("visualizer: %s", a.visualizers[a.visIdx].Name())
	case actFullscreen:
		a.fullscreen = !a.fullscreen
	case actVolumeUp:
		a.setVolume(a.state.Volume + 0.05)
	case actVolumeDown:
		a.setVolume(a.state.Volume - 0.05)
	case actLibrary:
		a.screen = screenLibrary
		if len(a.browser.artists) == 0 {
			a.statusf("loading artists…")
			return fetchArtists(a.library)
		}
		return a.browserCmd()
	}
	return nil
}

// onOff renders a boolean for status lines.
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// setVolume nudges the digital gain and reports it.
func (a *App) setVolume(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	if err := a.player.SetVolume(context.Background(), v); err != nil {
		a.statusfBad("volume: %v", err)
		return
	}
	a.state.Volume = v
	a.statusfBrief("volume %d%%", int(v*100+0.5))
}

// togglePlayPause implements space: play/pause, or — when the engine is
// idle — start whatever the policy says comes first (queue head, then
// repeat-one, then the radio).
func (a *App) togglePlayPause() tea.Cmd {
	st := a.state
	switch {
	case st.Track.ID == "":
		if a.pendingLoad {
			return nil // a start is already in flight; the radio re-picks on failure
		}
		if t, ok := a.playlist.Start(); ok {
			a.pendingLoad = true
			a.skipFrom = contract.Track{}
			a.statusf("play: %s — %s", t.BandName, t.Title)
			return a.loadCmd(t)
		}
		if a.playlist.radio {
			a.statusf("nothing to play — add tracks with l") // the radio is on but the index is empty
		} else {
			a.statusf("queue is empty, radio off — l adds tracks, d starts the radio")
		}
	case st.Playing:
		if err := a.player.Pause(context.Background()); err != nil {
			a.statusfBad("pause failed: %v", err)
		} else {
			a.statusfBrief("paused")
		}
	default:
		if err := a.player.Play(context.Background()); err != nil {
			a.statusfBad("play failed: %v", err)
		} else {
			a.statusfBrief("playing")
		}
	}
	return nil
}

// skip implements n: abandon the current track (recording it once the
// replacement is confirmed) and start the policy's next.
func (a *App) skip() tea.Cmd {
	if a.pendingLoad {
		return nil
	}
	a.skipFrom = a.playlist.Current()
	t, ok := a.playlist.SkipNext()
	if !ok {
		a.statusf("end of queue")
		a.syncNext()
		return nil
	}
	a.pendingLoad = true
	a.statusf("next: %s — %s", t.BandName, t.Title)
	return a.loadCmd(t)
}

// prev implements p: within 5 s of the start, restart the current track;
// otherwise go back to the most recently finished one.
func (a *App) prev() tea.Cmd {
	if a.pendingLoad {
		return nil
	}
	if a.state.Track.ID == "" {
		a.statusf("nothing playing")
		return nil
	}
	if a.pos <= prevRestartThreshold {
		a.pendingLoad = true
		a.skipFrom = a.state.Track
		a.statusf("restart: %s", a.state.Track.Title)
		return a.loadCmd(a.state.Track)
	}
	t, ok := a.playlist.Prev()
	if !ok {
		a.statusf("no previous track")
		return nil
	}
	a.pendingLoad = true
	a.skipFrom = a.state.Track
	a.statusf("previous: %s — %s", t.BandName, t.Title)
	return a.loadCmd(t)
}

// quit is the v2 way of leaving from Update: Quit returns a Msg, not a
// Cmd.
func quit() tea.Cmd {
	return func() tea.Msg { return tea.Quit() }
}

// handleLibraryKey dispatches the browser's keys.
func (a *App) handleLibraryKey(act action) tea.Cmd {
	switch act {
	case actScrollUp:
		a.browser.move(-1)
		return a.browserCmd()
	case actScrollDown:
		a.browser.move(1)
		return a.browserCmd()
	case actLeft:
		a.browser.focusOn(a.browser.focus - 1)
		return a.browserCmd()
	case actRight:
		a.browser.focusOn(a.browser.focus + 1)
		return a.browserCmd()
	case actSelect:
		return a.activateBrowser()
	case actRemove:
		a.removeBrowserTrack()
	case actLibrary, actBack:
		a.screen = screenPlayer
	}
	return nil
}

// activateBrowser acts on the focused column's selection: artist → load
// their releases (pure navigation), album → queue all its tracks, track →
// queue it.
func (a *App) activateBrowser() tea.Cmd {
	b := a.browser
	switch b.focus {
	case 0:
		if b.selArtist < len(b.artists) {
			b.focusOn(1)
		}
	case 1:
		if b.selAlbum < len(b.albums) {
			a.queueAlbum()
		}
	case 2:
		if b.selTrack < len(b.tracks) {
			a.queueTrack(b.tracks[b.selTrack])
		}
	}
	return a.browserCmd()
}

// queueAlbum queues every track of the selected album (enter on the albums
// column). When the tracks column is fresh for that album the tracks queue
// at once; otherwise the intent is recorded in queueAllFor and the
// refetch browserCmd issues does the queuing when its tracksMsg lands.
func (a *App) queueAlbum() {
	b := a.browser
	if b.selAlbum >= len(b.albums) {
		return
	}
	al := b.albums[b.selAlbum]
	if b.tracksFor == al.Slug {
		a.queueAlbumTracks(b.tracks)
		return
	}
	b.queueAllFor = al.Slug
}

// queueAlbumTracks adds the given tracks to the queue, skipping those
// already queued or playing, and fills the engine's pending slot.
func (a *App) queueAlbumTracks(ts []contract.Track) {
	if len(ts) == 0 {
		return
	}
	queued := make(map[string]bool, len(ts))
	for _, q := range a.playlist.Queue() {
		queued[q.ID] = true
	}
	if c := a.playlist.Current(); c.ID != "" {
		queued[c.ID] = true
	}
	n := 0
	for _, t := range ts {
		if queued[t.ID] {
			continue
		}
		queued[t.ID] = true
		a.playlist.Add(t)
		n++
	}
	if n == 0 {
		a.statusf("every track of %s is already queued or playing", clip(ts[0].AlbumTitle, 24))
		return
	}
	a.statusf("queued %d tracks from %s", n, clip(ts[0].AlbumTitle, 24))
	a.syncNext()
}

// queueTrack adds t to the queue and fills the engine's pending slot.
func (a *App) queueTrack(t contract.Track) {
	a.playlist.Add(t)
	a.statusf("queued %s — %s", clip(t.Title, 24), clip(t.BandName, 20))
	a.syncNext()
}

// removeBrowserTrack drops the selected track from the queue.
func (a *App) removeBrowserTrack() {
	b := a.browser
	if b.focus != 2 || b.selTrack >= len(b.tracks) {
		return
	}
	t := b.tracks[b.selTrack]
	if a.playlist.removeQueued(t) {
		a.statusf("removed from queue: %s", t.Title)
	}
}

// browserCmd returns the fetch the browser needs to fill a column whose
// selection no longer matches its contents: moving through artists
// preloads their releases, and the tracks column loads for the selected
// release.
func (a *App) browserCmd() tea.Cmd {
	b := a.browser
	switch b.focus {
	case 0:
		if b.selArtist < len(b.artists) {
			ar := b.artists[b.selArtist]
			if b.albumsFor != ar.BandID {
				return fetchAlbums(a.library, ar)
			}
		}
	case 1:
		if b.selAlbum < len(b.albums) {
			al := b.albums[b.selAlbum]
			if b.tracksFor != al.Slug {
				return fetchAlbumFull(a.library, b.albumArtist, al)
			}
		}
	}
	return nil
}
