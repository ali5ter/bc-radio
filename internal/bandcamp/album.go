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

// Album returns the full release record for one slug, tracks included. It
// implements contract.Library. Each track's Subdomain carries the release's
// canonical host (from the blob's url when present, else the artist's
// host), so a queued track stays re-openable without the library.
func (c *Client) Album(ctx context.Context, a contract.Artist, slug string) (contract.Album, error) {
	page, err := c.getHTML(ctx, c.pageURL(artistHost(a), "/album/"+slug), false)
	if err != nil {
		return contract.Album{}, err
	}
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

// parseTralbum extracts the full release record from an album page's
// data-tralbum JSON.
func parseTralbum(page string) (parsedRelease, error) {
	doc, err := parseDoc(page)
	if err != nil {
		return parsedRelease{}, fmt.Errorf("parse html: %w", err)
	}
	var blob tralbumBlob
	if err := attrJSON(doc, "data-tralbum", &blob); err != nil {
		return parsedRelease{}, err
	}
	if blob.ItemType != "" && blob.ItemType != "album" {
		return parsedRelease{}, fmt.Errorf("page is not an album (item_type %q)", blob.ItemType)
	}
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
	if blob.URL != "" {
		if u, err := url.Parse(blob.URL); err == nil && u.Host != "" {
			release.Host = u.Host
			if s := path.Base(u.Path); s != "." && s != "/" {
				release.Album.Slug = s
			}
		}
	}
	release.Album.Tracks = make([]contract.Track, 0, len(blob.TrackInfo))
	for _, t := range blob.TrackInfo {
		id := t.TrackID
		if id == 0 {
			id = t.ID
		}
		release.Album.Tracks = append(release.Album.Tracks, contract.Track{
			ID:         strconv.FormatInt(id, 10),
			Title:      t.Title,
			Number:     t.TrackNum,
			Duration:   time.Duration(t.Duration * float64(time.Second)),
			BandID:     blob.Current.BandID,
			BandName:   blob.Artist,
			AlbumID:    blob.ID,
			AlbumSlug:  release.Album.Slug,
			AlbumTitle: blob.Current.Title,
		})
		if u, ok := streamFileURL(t.File); ok {
			release.URLs[id] = u
		}
	}
	return release, nil
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
