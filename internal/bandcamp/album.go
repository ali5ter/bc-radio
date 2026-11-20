package bandcamp

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/ali5ter/bc-radio/internal/contract"
)

// tralbumBlob is the data-tralbum JSON on an album page. Only the fields
// the client needs are named; the blob also carries pricing, packages,
// play-cap and purchase state that v1 ignores.
type tralbumBlob struct {
	ID       int64  `json:"id"` // tralbum ID
	ItemType string `json:"item_type"`
	Artist   string `json:"artist"`
	URL      string `json:"url"` // canonical page URL, when present
	Current  struct {
		Title       string `json:"title"`
		BandID      int64  `json:"band_id"`
		ArtID       int64  `json:"art_id"`
		ReleaseDate string `json:"release_date"`
	} `json:"current"`
	TrackInfo []tralbumTrack `json:"trackinfo"`
}

// tralbumTrack is one trackinfo entry. The fixture corpus shows id and
// track_id as JSON numbers and duration as a fractional-second number; the
// contract's string-typed ID is filled from the numeric value.
type tralbumTrack struct {
	ID       int64             `json:"id"`
	TrackID  int64             `json:"track_id"`
	Title    string            `json:"title"`
	TrackNum int               `json:"track_num"`
	Duration float64           `json:"duration"`
	File     map[string]string `json:"file"` // encoding key -> signed stream URL
}

// parsedRelease is a parsed album plus the data the public contract types
// do not carry: the signed stream URLs, and the canonical host the release
// currently lives on (an artist's subdomain can change; the blob's url is
// the source of truth).
type parsedRelease struct {
	Album contract.Album
	Host  string // canonical host from the blob's url; empty when absent
	URLs  map[int64]string
}

// Album returns the full release record, tracks included, for one of the
// minimal albums Albums returned. It implements contract.Library. Each
// track's Subdomain carries the release's canonical host (from the
// blob's url when present, else the artist's host), so a queued track
// stays re-openable without the library.
//
// A real release is read from its /album/{slug} page. The synthetic
// singles grouping has no such page; for it the singles' /track/ pages
// are read instead, one fetch per single, best-effort.
func (c *Client) Album(ctx context.Context, a contract.Artist, al contract.Album) (contract.Album, error) {
	if al.Single {
		return c.singles(ctx, a)
	}
	page, _, err := c.getHTML(ctx, c.pageURL(artistHost(a), "/album/"+al.Slug), false)
	if err != nil {
		return contract.Album{}, err
	}
	return albumFromPage(page, al.Slug, a)
}

// singles is the synthetic grouping of the artist's single-track
// releases: one entry per single, each read from its /track/ page, which
// carries a full data-tralbum (verified live 2026-11-19 — TONTRAKUL's
// singles, after the spike's 404s on other artists' track pages). A
// single is a release with one track; the grouping is the library
// listing them under one entry (the "singles" album row) instead of one
// row per single.
//
// The /music page is authoritative for which singles exist; each single's
// /track/ page is best-effort — a single that 404s (killed) is named in
// the error and the readable ones are still returned, the walk's
// standard partial-release behaviour.
func (c *Client) singles(ctx context.Context, a contract.Artist) (contract.Album, error) {
	u := c.pageURL(artistHost(a), "/music")
	page, _, err := c.getHTML(ctx, u, false)
	if err != nil {
		return contract.Album{}, err
	}
	_, singles, err := parseMusic(page)
	if err != nil {
		return contract.Album{}, fmt.Errorf("bandcamp: parse music page for %s: %w", a.Name, err)
	}
	album := contract.Album{
		Title:  singlesTitle,
		Slug:   singlesSlug,
		BandID: a.BandID,
		Single: true,
	}
	host := artistHost(a)
	var missing []string
	for _, s := range singles {
		sp, _, err := c.getHTML(ctx, c.pageURL(host, "/track/"+s.slug), false)
		if err != nil {
			missing = append(missing, s.name())
			continue
		}
		release, err := parseTralbum(sp)
		if err != nil {
			missing = append(missing, s.name())
			continue
		}
		if release.Host != "" {
			host = release.Host
		}
		for i := range release.Album.Tracks {
			release.Album.Tracks[i].Subdomain = host
			release.Album.Tracks[i].AlbumSlug = "" // a single has no /album/ page
			if release.Album.Tracks[i].TrackSlug == "" {
				release.Album.Tracks[i].TrackSlug = s.slug
			}
			release.Album.Tracks[i].AlbumTitle = singlesTitle
		}
		album.Tracks = append(album.Tracks, release.Album.Tracks...)
	}
	if len(missing) > 0 {
		return album, fmt.Errorf("bandcamp: %s — %d of %d singles unavailable (%s)",
			a.Name, len(missing), len(singles), strings.Join(missing, ", "))
	}
	return album, nil
}

// singleFromTrackPage recovers the one release when an artist's /music
// page redirects to a single's /track/ page (an artist with exactly one
// release, a single: no listing to redirect from). It is the one-entry
// form of singles — same synthetic album, the track already in hand.
func singleFromTrackPage(page string, a contract.Artist) (contract.Album, bool) {
	release, err := parseTralbum(page)
	if err != nil || len(release.Album.Tracks) == 0 {
		return contract.Album{}, false
	}
	host := artistHost(a)
	if release.Host != "" {
		host = release.Host
	}
	for i := range release.Album.Tracks {
		release.Album.Tracks[i].Subdomain = host
		release.Album.Tracks[i].AlbumSlug = ""
		if release.Album.Tracks[i].TrackSlug == "" {
			release.Album.Tracks[i].TrackSlug = release.Album.Slug
		}
		release.Album.Tracks[i].AlbumTitle = singlesTitle
	}
	album := contract.Album{
		Title:  singlesTitle,
		Slug:   singlesSlug,
		BandID: a.BandID,
		Single: true,
		Tracks: release.Album.Tracks,
	}
	return album, true
}

// albumFromPage parses a release page body into the contract album: the
// tralbum blob for the record, then the slug and the canonical host
// stamped onto every track, so a queued track stays re-openable without
// the library.
func albumFromPage(page, slug string, a contract.Artist) (contract.Album, error) {
	release, err := parseTralbum(page)
	if err != nil {
		return contract.Album{}, fmt.Errorf("bandcamp: parse album %s: %w", slug, err)
	}
	release.Album.Slug = slug
	host := artistHost(a)
	if release.Host != "" {
		host = release.Host
	}
	for i := range release.Album.Tracks {
		release.Album.Tracks[i].Subdomain = host
		if release.Album.Tracks[i].AlbumSlug == "" {
			release.Album.Tracks[i].AlbumSlug = slug
		}
	}
	return release.Album, nil
}

// parseTralbum extracts the full release record from a release page's
// data-tralbum JSON. The client reads two kinds of release page: /album/
// (item_type "album") and /track/ — a single is a release with one
// track, and the track page carries the same blob shape (item_type
// "track", verified live 2026-11-19). A foreign kind — a page that is
// not a release at all — is a parse error.
func parseTralbum(page string) (parsedRelease, error) {
	doc, err := parseDoc(page)
	if err != nil {
		return parsedRelease{}, fmt.Errorf("parse html: %w", err)
	}
	var blob tralbumBlob
	if err := attrJSON(doc, "data-tralbum", &blob); err != nil {
		return parsedRelease{}, err
	}
	if blob.ItemType != "" && blob.ItemType != "album" && blob.ItemType != "track" {
		return parsedRelease{}, fmt.Errorf("page is not a release (item_type %q)", blob.ItemType)
	}
	return releaseFromBlob(blob), nil
}

// releaseFromBlob builds the release record from a tralbum blob: the
// release header, the canonical host and slug from the blob's url (when
// present), and one contract track per trackinfo entry, each with its
// signed stream URL kept for the audio path.
func releaseFromBlob(blob tralbumBlob) parsedRelease {
	release := parsedRelease{
		Album: contract.Album{
			ID:          blob.ID,
			Title:       blob.Current.Title,
			BandID:      blob.Current.BandID,
			ArtID:       blob.Current.ArtID,
			ReleaseDate: parseBCDate(blob.Current.ReleaseDate),
		},
		URLs: make(map[int64]string, len(blob.TrackInfo)),
	}
	var u *url.URL
	if blob.URL != "" {
		if parsed, err := url.Parse(blob.URL); err == nil && parsed.Host != "" {
			u = parsed
			release.Host = u.Host
			if s := path.Base(u.Path); s != "." && s != "/" {
				release.Album.Slug = s
			}
		}
	}
	// A /track/ release stamps the slug onto TrackSlug (its stream page
	// is the track page); an /album/ release onto AlbumSlug. The
	// distinction comes from the blob's item_type, cross-checked with
	// the canonical URL's path (a track page missing item_type is still
	// recognised by its /track/ path).
	single := blob.ItemType == "track"
	if !single && u != nil && strings.Contains(u.Path, "/track/") {
		single = true
	}
	release.Album.Tracks = make([]contract.Track, 0, len(blob.TrackInfo))
	for _, t := range blob.TrackInfo {
		id := t.TrackID
		if id == 0 {
			id = t.ID
		}
		tr := contract.Track{
			ID:         strconv.FormatInt(id, 10),
			Title:      t.Title,
			Number:     t.TrackNum,
			Duration:   time.Duration(t.Duration * float64(time.Second)),
			BandID:     blob.Current.BandID,
			BandName:   blob.Artist,
			AlbumID:    blob.ID,
			AlbumSlug:  release.Album.Slug,
			AlbumTitle: blob.Current.Title,
		}
		if single {
			tr.AlbumSlug = "" // a single has no /album/ page
			tr.TrackSlug = release.Album.Slug
		}
		release.Album.Tracks = append(release.Album.Tracks, tr)
		if u, ok := streamFileURL(t.File); ok {
			release.URLs[id] = u
		}
	}
	return release
}

// streamFileURL picks the signed stream URL from a trackinfo file map. It
// prefers mp3-128 (the encoding v1 plays, verified in the spike); when a
// session exposes other encodings it falls back to the first mp3-* key.
// Non-mp3 keys (e.g. "streaming" previews) are never returned: the audio
// pipeline decodes MP3 only.
func streamFileURL(file map[string]string) (string, bool) {
	if u, ok := file["mp3-128"]; ok && u != "" {
		return u, true
	}
	best := ""
	for key, u := range file {
		if !strings.HasPrefix(key, "mp3") || u == "" {
			continue
		}
		if best == "" || key < best {
			best = key
		}
	}
	if best == "" {
		return "", false
	}
	return file[best], true
}
