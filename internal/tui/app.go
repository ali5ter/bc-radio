package tui

import (
	"context"
	"fmt"
	"time"

	"bc-radio/internal/contract"
	"charm.land/bubbletea/v2"
)

// tickPeriod is the render cadence: 16 ms ≈ 60 fps, the PRD's visual
// target. The program also re-renders on every event, so the tick is the
// floor for animation, not a ceiling.
const tickPeriod = 16 * time.Millisecond

// statusTTL is how long a status message lingers before fading.
const statusTTL = 8 * time.Second

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
	state       contract.State
	pos         time.Duration
	visualizers []visualizer
	visIdx      int

	width      int
	height     int
	fullscreen bool
	help       bool
	screen     screen
	status     string
	statusAt   time.Time

	engineNextID string         // the track in the engine's single pending slot
	pendingLoad  bool           // a Load is in flight; transport actions wait
	skipFrom     contract.Track // abandoned by a skip/prev, recorded on success
	indexState   indexState     // the radio's track-index lifecycle
}

// New builds an App over a library and a player. The player is not started
// here; Run drives the program and stops the player on exit.
func New(library contract.Library, player contract.Player) *App {
	return &App{
		library:     library,
		player:      player,
		playlist:    newPlaylist(),
		browser:     newBrowser(),
		bands:       make([]float64, contract.BandCount),
		state:       player.State(),
		visualizers: makeVisualizers(),
	}
}

// Init implements tea.Model: start the render tick, the event pump, and
// the follow-list fetch. The follow list is fetched at startup (not on the
// first l, as before) so the radio's track index can start building while
// the user is still reading the player screen.
func (a *App) Init() tea.Cmd {
	return tea.Batch(a.tick(), a.watch(), fetchArtists(a.library))
}

// Update implements tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		a.browser.viewH = a.height - 3 // title, columns, help
		return a, nil

	case tickMsg:
		a.sample()
		return a, a.tick()

	case eventMsg:
		a.onEvent(m.ev)
		return a, a.watch()

	case loadedMsg:
		a.pendingLoad = false
		skipFrom := a.skipFrom
		a.skipFrom = contract.Track{}
		if m.err != nil {
			a.statusf("playback failed: %v", m.err)
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
		if m.err != nil {
			a.statusf("library: %v", m.err)
		} else {
			a.statusf("%d artists", len(m.artists))
		}
		// The index is the radio's fuel: build it as soon as there are
		// follows. The batch cap's partial note is not a stop — the 20
		// readable artists still walk.
		cmd := a.browserCmd()
		if a.indexState == indexIdle && len(m.artists) > 0 {
			a.indexState = indexBuilding
			cmd = tea.Batch(cmd, fetchIndex(a.library))
		}
		return a, cmd

	case albumsMsg:
		if m.err != nil {
			a.statusf("albums: %v", m.err)
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
				a.statusf("queueing %s: %v", m.slug, m.err)
			} else {
				a.statusf("tracks: %v", m.err)
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
		a.playlist.SetIndex(m.tracks)
		switch {
		case m.err != nil && len(m.tracks) == 0:
			a.indexState = indexFailed
			a.statusf("track index unavailable: %v", m.err)
		case m.err != nil:
			a.indexState = indexReady
			a.statusf("track index: %d tracks — %v", len(m.tracks), m.err)
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

// sample refreshes the display state once per tick: the position (frozen
// while paused), the eased band frame, and the status expiry. The engine's
// analyser is fed only while audio is being produced, so when the engine
// is idle the eased target is zero — the visual decays to rest.
func (a *App) sample() {
	st := a.player.State()
	a.state = st
	if st.Playing {
		a.pos = st.Position
	}
	target := a.player.Bands()
	if st.Track.ID == "" {
		target = nil
	}
	for k := range a.bands {
		t := 0.0
		if target != nil && k < len(target) {
			t = target[k]
		}
		a.bands[k] += (t - a.bands[k]) * 0.3
	}
	if a.status != "" && time.Since(a.statusAt) > statusTTL {
		a.status = ""
	}
}

// statusf sets the transient status line.
func (a *App) statusf(format string, args ...any) {
	a.status = fmt.Sprintf(format, args...)
	a.statusAt = time.Now()
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
		a.statusf("playback failed: %v", ev.Err)
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
		a.statusf("playback failed: %v", failErr)
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
		a.statusf("shuffle %s", onOff(a.playlist.shuffle))
	case actRepeat:
		a.playlist.repeat = !a.playlist.repeat
		a.statusf("repeat %s", onOff(a.playlist.repeat))
	case actRadio:
		a.playlist.radio = !a.playlist.radio
		a.statusf("radio %s", onOff(a.playlist.radio))
	case actVisual:
		a.visIdx = (a.visIdx + 1) % len(a.visualizers)
		a.statusf("visualizer: %s", a.visualizers[a.visIdx].Name())
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
		a.statusf("volume: %v", err)
		return
	}
	a.state.Volume = v
	a.statusf("volume %d%%", int(v*100+0.5))
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
			a.statusf("pause failed: %v", err)
		} else {
			a.statusf("paused")
		}
	default:
		if err := a.player.Play(context.Background()); err != nil {
			a.statusf("play failed: %v", err)
		} else {
			a.statusf("playing")
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
