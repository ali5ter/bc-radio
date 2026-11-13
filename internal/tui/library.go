package tui

import (
	"context"

	"bc-radio/internal/contract"
	"charm.land/bubbletea/v2"
)

// Messages carrying the results of library fetches. Each carries enough
// identity to be matched against what is currently selected, so a slow
// response for a band the user has already moved on from is dropped rather
// than rendered over the new selection.
type (
	artistsMsg struct {
		artists []contract.Artist
		err     error
	}
	albumsMsg struct {
		artist contract.Artist
		albums []contract.Album
		err    error
	}
	tracksMsg struct {
		album contract.Album
		slug  string // the requested slug, known even when the fetch failed
		err   error
	}
	indexMsg struct {
		tracks []contract.Track
		err    error
	}
)

// Fetch commands. Each runs the call in a goroutine under bubbletea; the
// library implementation serves repeated calls from its local cache, so
// refetching a page is cheap and the browser can simply ask again.
func fetchArtists(lib contract.Library) tea.Cmd {
	return func() tea.Msg {
		a, err := lib.Artists(context.Background())
		return artistsMsg{artists: a, err: err}
	}
}

func fetchAlbums(lib contract.Library, a contract.Artist) tea.Cmd {
	return func() tea.Msg {
		albums, err := lib.Albums(context.Background(), a)
		return albumsMsg{artist: a, albums: albums, err: err}
	}
}

func fetchAlbumFull(lib contract.Library, a contract.Artist, al contract.Album) tea.Cmd {
	return func() tea.Msg {
		album, err := lib.Album(context.Background(), a, al.Slug)
		return tracksMsg{album: album, slug: al.Slug, err: err}
	}
}

// fetchIndex builds the radio's flat track index (every track of every
// followed band). The walk is a few seconds of cached page reads, but it
// runs in a goroutine under bubbletea anyway: it must never block the UI,
// which shows "building track index…" in the meantime.
func fetchIndex(lib contract.Library) tea.Cmd {
	return func() tea.Msg {
		tracks, err := lib.Tracks(context.Background())
		return indexMsg{tracks: tracks, err: err}
	}
}

// browser is the library screen's state: three columns of selection. The
// albums and tracks columns are lazy — they are fetched for the selected
// entry above them, and the *For fields record which entry each column was
// fetched for, so a moved selection invalidates the stale column.
type browser struct {
	artists     []contract.Artist
	albums      []contract.Album
	tracks      []contract.Track
	albumArtist contract.Artist // the artist the albums column was fetched for

	selArtist int
	selAlbum  int
	selTrack  int
	focus     int // 0 artists, 1 albums, 2 tracks
	scroll    [3]int
	viewH     int    // viewport rows per column, set on resize
	albumsFor int64  // artist BandID the albums column holds; -1 = none
	tracksFor string // album slug the tracks column holds; "" = none

	// queueAllFor is the album slug whose tracks the user asked to queue
	// (enter on the albums column) but whose tracks column was not fresh:
	// set here, consumed when the refetched tracksMsg lands.
	queueAllFor string
}

// newBrowser builds a browser with no data loaded.
func newBrowser() *browser { return &browser{albumsFor: -1} }

// columnCount is the number of rows in the focused column.
func (b *browser) columnCount() int { return b.columnCountAt(b.focus) }

// columnSel is the selection index of the focused column.
func (b *browser) columnSel() int { return b.columnSelAt(b.focus) }

// columnCountAt is the number of rows in column col.
func (b *browser) columnCountAt(col int) int {
	switch col {
	case 0:
		return len(b.artists)
	case 1:
		return len(b.albums)
	default:
		return len(b.tracks)
	}
}

// columnSelAt is the selection index of column col.
func (b *browser) columnSelAt(col int) int {
	switch col {
	case 0:
		return b.selArtist
	case 1:
		return b.selAlbum
	default:
		return b.selTrack
	}
}

// columnSetSel moves the focused column's selection to i, clamped, keeping
// it inside the viewport.
func (b *browser) columnSetSel(i int) {
	n := b.columnCount()
	if n == 0 {
		return
	}
	if i < 0 {
		i = 0
	}
	if i >= n {
		i = n - 1
	}
	if b.focus == 0 {
		b.selArtist = i
	} else if b.focus == 1 {
		b.selAlbum = i
	} else {
		b.selTrack = i
	}
	b.scrollIntoView(b.focus, i)
}

// move shifts the focused column's selection by dy rows.
func (b *browser) move(dy int) {
	b.columnSetSel(b.columnSel() + dy)
}

// focusOn moves focus to column i.
func (b *browser) focusOn(i int) {
	if i < 0 {
		i = 0
	}
	if i > 2 {
		i = 2
	}
	b.focus = i
}

// scrollIntoView keeps row i visible in the column's viewport.
func (b *browser) scrollIntoView(col, i int) {
	if b.viewH < 1 {
		return
	}
	if i < b.scroll[col] {
		b.scroll[col] = i
	}
	if i >= b.scroll[col]+b.viewH {
		b.scroll[col] = i - b.viewH + 1
	}
}

// columnRows renders the focused column's rows (see layout); the browser
// only supplies the data. rowFor returns the display line for row i of
// column col, or "" when out of range.
func (b *browser) rowFor(col, i int) string {
	switch col {
	case 0:
		if i < 0 || i >= len(b.artists) {
			return ""
		}
		return b.artists[i].Name
	case 1:
		if i < 0 || i >= len(b.albums) {
			return ""
		}
		return b.albums[i].Title
	default:
		if i < 0 || i >= len(b.tracks) {
			return ""
		}
		return b.tracks[i].Title
	}
}

// removeQueued drops t from the queue wherever it appears; it reports
// whether anything was removed.
func (p *playlist) removeQueued(t contract.Track) bool {
	n := len(p.queue)
	out := p.queue[:0]
	for _, q := range p.queue {
		if q.ID != t.ID {
			out = append(out, q)
		}
	}
	p.queue = out
	return len(out) < n
}
