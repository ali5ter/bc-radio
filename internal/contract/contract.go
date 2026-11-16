// Package contract defines the shared types and interfaces of bc-radio.
//
// It is the frozen boundary between the Phase 1b packages:
//
//   - internal/bandcamp implements Library and AudioSource.
//   - internal/audio implements Player and consumes AudioSource.
//   - internal/tui consumes Library and Player.
//
// The package contains types, interfaces and constants only — no
// behaviour. Packages in one Phase 1b work area may import it, never a
// sibling work area; propose contract changes at a checkpoint, not
// mid-phase.
package contract

import (
	"context"
	"io"
	"time"
)

// BandCount is the number of frequency bands in one analyser frame.
// The locked v1 decision: a 16-band analyser — v1 built
// it from Goertzel filters; Phase 2 analyses with a pure-Go FFT instead,
// but the count is all the contract pins.
const BandCount = 16

// Artist is a followed Bandcamp artist, as parsed from the following
// page's data-blob JSON (item_cache.following_bands). Subdomain and
// CustomDomain are mutually exclusive: exactly one is set, and the
// artist's host is Subdomain + ".bandcamp.com" or CustomDomain verbatim.
type Artist struct {
	BandID       int64
	Name         string
	Subdomain    string
	CustomDomain string
	ArtID        int64 // avatar art, for display
	DateFollowed time.Time
}

// Album is a Bandcamp release (tralbum). A record returned by
// Library.Albums carries only Slug, Title and ArtID; the full form,
// returned by Library.Album, also carries ID, ReleaseDate and Tracks
// (zero values mean "not populated yet").
type Album struct {
	ID          int64 // tralbum id; 0 until the album page is fetched
	Slug        string
	Title       string
	BandID      int64
	ArtID       int64
	ReleaseDate time.Time // zero value when unknown
	Tracks      []Track
}

// Track is one track, denormalized with both the display strings the TUI
// renders and the location fields the bandcamp client needs to find its
// stream URL. A Track must be self-sufficient: once queued it is playable
// without re-consulting the library.
//
// Upstream, data-tralbum trackinfo carries id/track_id (strings), title,
// duration (a string of fractional seconds) and track_num.
type Track struct {
	ID         string
	Title      string
	Number     int // position on the release; 0 when unknown
	Duration   time.Duration
	BandID     int64
	BandName   string
	AlbumID    int64
	AlbumSlug  string
	AlbumTitle string
	Subdomain  string // artist host for stream-URL fetches
}

// AudioStream is an open stream of one encoded track (MP3 in v1). The
// consumer must close Body.
type AudioStream struct {
	Body          io.ReadCloser
	ContentLength int64 // encoded bytes; -1 when unknown
}

// State is a snapshot of a Player.
type State struct {
	Track    Track // zero value when idle
	Position time.Duration
	Playing  bool
	Volume   float64 // digital gain, 0..1
}

// EventKind identifies a player event.
type EventKind int

// Event kinds emitted on Player.Events.
const (
	EventTrackStarted EventKind = iota
	EventTrackEnded
	EventPlaybackFailed
)

// Event is a notification from a Player. Err is set for
// EventPlaybackFailed only.
type Event struct {
	Kind  EventKind
	Track Track // the affected track
	Err   error
}

// Library is the local index of followed artists and their releases.
// bandcamp implements it; tui consumes it. Implementations serve
// repeated calls from the local cache and must be safe for concurrent
// use.
type Library interface {
	// Artists returns the followed artists — the first 20 embedded in the
	// following page. Implementations must surface a clear error or
	// annotation when item_count exceeds the embedded batch (accounts
	// following more than 20 artists).
	Artists(ctx context.Context) ([]Artist, error)

	// Albums returns the artist's releases from the /music page, in
	// minimal form (see Album).
	Albums(ctx context.Context, a Artist) ([]Album, error)

	// Album returns the full release record for one slug, tracks included.
	Album(ctx context.Context, a Artist, slug string) (Album, error)

	// Tracks returns every track of every release of every followed artist
	// — the flat index behind random radio. It is the composition of
	// Artists, Albums and Album, walked in that order, and is served from
	// the local cache. The walk is best-effort: a release whose page fails
	// is skipped, not fatal; err (when non-nil) describes what was skipped
	// and the returned slice is still the index to use.
	Tracks(ctx context.Context) ([]Track, error)
}

// Indexer is implemented by Libraries that can report how far their
// Tracks walk is through the followed artists, so a UI can show progress
// while the index builds. It is separate from Library because reporting is
// optional: a Library that cannot report it is still a Library, and the UI
// simply shows the plain building line.
type Indexer interface {
	// IndexProgress returns how many artists the current Tracks walk has
	// finished and how many it has to walk. The total is known once the
	// follow list is read. Both are zero when no walk is running.
	IndexProgress() (artistsDone, artistsTotal int)
}

// AudioSource opens the encoded audio of a track. bandcamp implements it;
// audio consumes it. Implementations must re-fetch the stream URL when the
// previous one has expired or a fetch fails (URLs are signed, ~24 h).
type AudioSource interface {
	Open(ctx context.Context, t Track) (AudioStream, error)
}

// Player is the playback engine. audio implements it; tui consumes it.
// Contexts bound the setup of an operation (fetch, device), not the whole
// playback, which runs until Stop or the engine's own context ends.
type Player interface {
	// Load starts playing t at once, replacing whatever is current.
	Load(ctx context.Context, t Track) error

	// Queue schedules t to start seamlessly (gapless) when the current
	// track ends. If the player is idle, Queue starts t at once. Queue
	// runs on the engine's own context, so it takes none.
	Queue(t Track)

	// Play resumes, Pause suspends and Stop unwinds the engine to idle.
	Play(ctx context.Context) error
	Pause(ctx context.Context) error
	Stop(ctx context.Context) error

	// SetVolume sets the digital gain, 0..1.
	SetVolume(ctx context.Context, volume float64) error

	// State returns the current snapshot.
	State() State

	// Bands returns the latest analyser frame: BandCount values in [0,1],
	// ordered low to high frequency. The frame reflects what is audible
	// right now — it refreshes as the audio device's buffer drains, and
	// decays to rest on silence. Display data only.
	Bands() []float64

	// Scope returns the post-gain mono samples ending at what is audible
	// right now, for a time-domain display: oldest to newest, in [-1,1],
	// at most one analyser window. The slice is empty before audio is
	// produced. Display data only — never gate playback logic on it.
	Scope() []float64

	// Events yields player events. The channel closes when the engine
	// stops.
	Events() <-chan Event
}
