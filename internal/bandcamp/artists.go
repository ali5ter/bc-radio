package bandcamp

import (
	"context"
	"fmt"
	"time"

	"bc-radio/internal/contract"
)

// bcTimeLayout is Bandcamp's human timestamp layout
// ("07 Jan 2026 12:03:45 GMT"). The one-digit day form also matches
// two-digit days, so one layout covers both.
const bcTimeLayout = "2 Jan 2006 15:04:05 MST"

// parseBCDate parses a Bandcamp timestamp, returning the zero time for an
// empty or unparseable value: timestamps are display data, not load-bearing.
func parseBCDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(bcTimeLayout, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// followingBlob is the data-blob JSON on the following page. Only the
// fields the client needs are named; the blob carries much more (genres,
// collections, suggestions) that v1 ignores.
type followingBlob struct {
	FanData struct {
		Identity map[string]any `json:"identity"`
	} `json:"fan_data"`
	FollowingBands struct {
		ItemCount int      `json:"item_count"` // total follows, beyond the batch
		BatchSize int      `json:"batch_size"` // 20 — the embedded batch cap
		Sequence  []string `json:"sequence"`   // band IDs, in follow order
	} `json:"following_bands_data"`
	ItemCache struct {
		FollowingBands map[string]followingItem `json:"following_bands"`
	} `json:"item_cache"`
}

// followingItem is one entry of item_cache.following_bands, keyed by
// (decimal) band ID.
type followingItem struct {
	BandID       int64  `json:"band_id"`
	Name         string `json:"name"`
	ArtID        int64  `json:"art_id"`
	DateFollowed string `json:"date_followed"`
	URLHints     struct {
		Subdomain    string `json:"subdomain"`
		CustomDomain string `json:"custom_domain"`
	} `json:"url_hints"`
}

// Artists returns the followed artists in follow order, from the user's
// following page. It implements contract.Library.
//
// Bandcamp embeds at most BatchSize (20) items in the page; the paging API
// behind the rest is server-dead (spike finding 2). When item_count exceeds
// the embedded batch, the returned slice is the (complete-so-far) list and
// the error names the gap, so the TUI can show both.
func (c *Client) Artists(ctx context.Context) ([]contract.Artist, error) {
	page, err := c.getHTML(ctx, c.followingURL(), false)
	if err != nil {
		return nil, err
	}
	artists, err := parseFollowing(page)
	if err != nil {
		// The only parse error is the >20 batch-cap note: the list is
		// complete-so-far and still useful, so return it alongside the
		// error rather than throwing it away.
		return artists, fmt.Errorf("bandcamp: parse following page: %w", err)
	}
	return artists, nil
}

// parseFollowing extracts the followed artists from a following page's
// data-blob. The returned slice follows the blob's sequence order; entries
// referenced by the sequence but missing from the item cache are skipped.
func parseFollowing(page string) ([]contract.Artist, error) {
	doc, err := parseDoc(page)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}
	var blob followingBlob
	if err := attrJSON(doc, "data-blob", &blob); err != nil {
		return nil, err
	}
	artists := make([]contract.Artist, 0, len(blob.FollowingBands.Sequence))
	for _, id := range blob.FollowingBands.Sequence {
		item, ok := blob.ItemCache.FollowingBands[id]
		if !ok {
			continue
		}
		artists = append(artists, contract.Artist{
			BandID:       item.BandID,
			Name:         item.Name,
			Subdomain:    item.URLHints.Subdomain,
			CustomDomain: item.URLHints.CustomDomain,
			ArtID:        item.ArtID,
			DateFollowed: parseBCDate(item.DateFollowed),
		})
	}
	if blob.FollowingBands.ItemCount > len(artists) {
		return artists, fmt.Errorf("the following page embeds only %d of %d followed bands (Bandcamp caps the embedded batch at 20); the list is partial",
			len(artists), blob.FollowingBands.ItemCount)
	}
	return artists, nil
}
