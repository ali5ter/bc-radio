package bandcamp

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/ali5ter/bc-radio/internal/contract"
)

// artIDPattern matches the art ID in a bcbits image URL
// (https://f4.bcbits.com/img/a1107809698_2.jpg).
var artIDPattern = regexp.MustCompile(`/a(\d+)(?:_|$)`)

// Albums returns the artist's releases in minimal form (Slug, Title,
// ArtID, plus the ID and BandID the cards also carry), in page order
// (newest first). It implements contract.Library.
//
// Single-track releases appear on /music as track-* cards; v1 skips them
// because track pages return 404 and cannot be scraped (package doc).
func (c *Client) Albums(ctx context.Context, a contract.Artist) ([]contract.Album, error) {
	page, err := c.getHTML(ctx, c.pageURL(artistHost(a), "/music"), false)
	if err != nil {
		return nil, err
	}
	albums, err := parseMusic(page)
	if err != nil {
		return nil, fmt.Errorf("bandcamp: parse music page for %s: %w", a.Name, err)
	}
	return albums, nil
}

// parseMusic extracts the release cards from an artist /music page. Cards
// are <li> elements with a data-item-id attribute; kind "album" is kept,
// kind "track" (singles) is skipped.
func parseMusic(page string) ([]contract.Album, error) {
	doc, err := parseDoc(page)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}
	var albums []contract.Album
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if id, ok := attrValue(n.Attr, "data-item-id"); ok {
				if kind, num, ok := splitItemID(id); ok && kind == "album" {
					if album, ok := albumCard(n, num); ok {
						albums = append(albums, album)
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
	return albums, nil
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
