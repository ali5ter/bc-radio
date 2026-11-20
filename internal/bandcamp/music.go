package bandcamp

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/ali5ter/bc-radio/internal/contract"
)

// artIDPattern matches the art ID in a bcbits image URL
// (https://f4.bcbits.com/img/a1107809698_2.jpg).
var artIDPattern = regexp.MustCompile(`/a(\d+)(?:_|$)`)

// singlesTitle and singlesSlug name the synthetic album that groups an
// artist's single-track releases: the title is what the library shows,
// the slug is the opaque key callers (and the browser's freshness
// tracking) round-trip — it is never turned into a URL (Album takes the
// whole Album, and its Single flag, not the slug).
const (
	singlesTitle = "singles"
	singlesSlug  = "singles"
)

// singleCard is one single-track release, read from its track-* card on
// the /music page. The card carries enough to fetch the single's /track/
// page; that page's data-tralbum carries the rest (title, duration,
// stream URL).
type singleCard struct {
	id    int64
	slug  string
	title string
}

// Albums returns the artist's releases in minimal form (Slug, Title,
// ArtID, plus the ID and BandID the cards also carry), in page order
// (newest first). It implements contract.Library.
//
// Single-track releases appear on /music as track-* cards. They are
// grouped into one synthetic "singles" album after the real releases:
// the band's singles listed under one entry, not one row per single.
func (c *Client) Albums(ctx context.Context, a contract.Artist) ([]contract.Album, error) {
	u := c.pageURL(artistHost(a), "/music")
	page, final, err := c.getHTML(ctx, u, false)
	if err != nil {
		return nil, err
	}
	albums, singles, err := parseMusic(page)
	if err != nil {
		return nil, fmt.Errorf("bandcamp: parse music page for %s: %w", a.Name, err)
	}
	// An artist with exactly one release has no listing: Bandcamp answers
	// /music with a 303 to the release page, so the card walk above finds
	// nothing in a page that is a release, not a listing. The moved final
	// URL is the redirect's signature — a true listing answers 200 in
	// place — and its path supplies the kind and slug (Alister's Go
	// Kurosawa: one album, an empty column). A redirect elsewhere (a home
	// page, a killed release) parses to nothing and keeps the column
	// empty, as before.
	if len(albums) == 0 {
		if kind, slug, ok := redirectRelease(u, final); ok {
			switch kind {
			case "album":
				if album, err := albumFromPage(page, slug, a); err == nil {
					albums = []contract.Album{album}
				}
			case "track":
				// One release, a single: recover it as the one-entry
				// singles grouping, so the column reads the same as an
				// artist with several singles.
				if album, ok := singleFromTrackPage(page, a); ok {
					albums = []contract.Album{album}
				}
			}
		}
	}
	if len(singles) > 0 {
		albums = append(albums, contract.Album{
			Title:  singlesTitle,
			Slug:   singlesSlug,
			BandID: a.BandID,
			Single: true,
		})
	}
	return albums, nil
}

// redirectRelease reports the release kind and slug when a /music request
// ended up somewhere else: the path's second-to-last element names the
// kind (album or track) and the last is the slug. Equal paths mean the
// listing answered in place.
func redirectRelease(requested, final string) (kind, slug string, ok bool) {
	ru, err := url.Parse(requested)
	if err != nil {
		return "", "", false
	}
	fu, err := url.Parse(final)
	if err != nil {
		return "", "", false
	}
	if ru.Path == fu.Path {
		return "", "", false
	}
	els := strings.Split(strings.Trim(fu.Path, "/"), "/")
	if len(els) < 2 {
		return "", "", false
	}
	switch els[len(els)-2] {
	case "album", "track":
		return els[len(els)-2], els[len(els)-1], true
	}
	return "", "", false
}

// parseMusic extracts the release cards from an artist /music page. Cards
// are <li> elements with a data-item-id attribute: kind "album" becomes a
// minimal contract.Album, kind "track" a singleCard. A single listed on
// two cards (a featured banner repeats the grid card) is de-duplicated by
// its numeric ID.
func parseMusic(page string) (albums []contract.Album, singles []singleCard, err error) {
	doc, err := parseDoc(page)
	if err != nil {
		return nil, nil, fmt.Errorf("parse html: %w", err)
	}
	seen := make(map[int64]bool)
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if id, ok := attrValue(n.Attr, "data-item-id"); ok {
				if kind, num, ok := splitItemID(id); ok {
					switch kind {
					case "album":
						if album, ok := albumCard(n, num); ok {
							albums = append(albums, album)
						}
					case "track":
						if !seen[num] {
							seen[num] = true
							if card, ok := singleCardFromNode(n, num); ok {
								singles = append(singles, card)
							}
						}
					}
				}
				return // card content never nests another card
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return albums, singles, nil
}

// splitItemID splits a data-item-id of the form "album-123" or
// "track-456" into its kind and numeric ID.
func splitItemID(id string) (kind string, num int64, ok bool) {
	for i := 0; i < len(id); i++ {
		if id[i] == '-' {
			n, err := strconv.ParseInt(id[i+1:], 10, 64)
			if err != nil {
				return "", 0, false
			}
			return id[:i], n, true
		}
	}
	return "", 0, false
}

// albumCard builds a minimal contract.Album from one release card: the ID
// from data-item-id, the BandID from data-band-id, the slug from the
// /album/ href, the ArtID from the cover image URL, and the title from the
// title element.
func albumCard(card *html.Node, id int64) (contract.Album, bool) {
	album := contract.Album{ID: id}
	if v, ok := attrValue(card.Attr, "data-band-id"); ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			album.BandID = n
		}
	}
	for n := nextNode(card); n != nil; n = nextNode(n) {
		switch {
		case n.Type != html.ElementNode:
			continue
		case n.Data == "a" && album.Slug == "":
			if href, ok := attrValue(n.Attr, "href"); ok && strings.HasPrefix(href, "/album/") {
				album.Slug = href[len("/album/"):]
			}
		case n.Data == "img" && album.ArtID == 0:
			if src, ok := attrValue(n.Attr, "src"); ok {
				if m := artIDPattern.FindStringSubmatch(src); m != nil {
					album.ArtID, _ = strconv.ParseInt(m[1], 10, 64)
				}
			}
		case n.Data == "p" && album.Title == "":
			if class, ok := attrValue(n.Attr, "class"); ok && strings.Contains(class, "title") {
				album.Title = text(n)
			}
		}
	}
	if album.Slug == "" {
		return contract.Album{}, false
	}
	return album, true
}

// name is the single's display name for a skip message: the card's title
// when the page carried one, else its slug.
func (s singleCard) name() string {
	if s.title != "" {
		return s.title
	}
	return s.slug
}

// singleCardFromNode builds a singleCard from one track-* release card:
// the ID from data-item-id, the slug from the /track/ href, the title
// from the title element (used to name a single whose /track/ page later
// fails). The same shape as albumCard, with the /track/ prefix.
func singleCardFromNode(card *html.Node, id int64) (singleCard, bool) {
	sc := singleCard{id: id}
	for n := nextNode(card); n != nil; n = nextNode(n) {
		if n.Type != html.ElementNode {
			continue
		}
		switch {
		case n.Data == "a" && sc.slug == "":
			if href, ok := attrValue(n.Attr, "href"); ok && strings.HasPrefix(href, "/track/") {
				sc.slug = href[len("/track/"):]
			}
		case n.Data == "p" && sc.title == "":
			if class, ok := attrValue(n.Attr, "class"); ok && strings.Contains(class, "title") {
				sc.title = text(n)
			}
		}
	}
	if sc.slug == "" {
		return singleCard{}, false
	}
	return sc, true
}
