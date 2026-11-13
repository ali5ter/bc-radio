package bandcamp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"bc-radio/internal/contract"
)

// Tracks returns every track of every release of every followed artist —
// the flat index behind random radio, in walk order (follows, then each
// artist's releases, then each release's tracks). It implements
// contract.Library.
//
// The walk is best-effort: an artist whose /music page fails, or a release
// whose page fails, is skipped and named in the returned error, never fatal
// — the index is the union of what could be read, and err describes the
// gaps. (The follow list itself failing with an empty result is the one
// hard stop: there is nothing to walk.)
//
// Every page the walk reads goes through the shared disk page cache, so a
// rebuild after the TTL is the only time the network is touched again. The
// walk also warms that cache for the releases it reads: Open re-resolves
// signed stream URLs from the same album pages, so the first radio pick
// after a rebuild opens without fetching the album page a second time.
//
// The walk is sequential on purpose: it is a one-time-per-day cost, and a
// polite rate is the difference between a cache warm-up and a 429 storm.
func (c *Client) Tracks(ctx context.Context) ([]contract.Track, error) {
	artists, err := c.Artists(ctx)
	if len(artists) == 0 {
		if err == nil {
			return nil, nil // nobody followed: an empty index is a valid answer
		}
		return nil, err
	}

	var (
		tracks       []contract.Track
		skipArtists  []string // artists whose /music page was unreadable
		skipReleases []string // releases whose album page was unreadable
	)
	for _, a := range artists {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return tracks, ctxErr
		}
		albums, err := c.Albums(ctx, a)
		if err != nil {
			skipArtists = append(skipArtists, a.Name)
			continue
		}
		for _, al := range albums {
			full, err := c.Album(ctx, a, al.Slug)
			if err != nil {
				skipReleases = append(skipReleases, al.Title)
				continue
			}
			tracks = append(tracks, full.Tracks...)
		}
	}

	var problems []string
	if err != nil {
		problems = append(problems, err.Error()) // e.g. the >20 batch-cap note
	}
	if len(skipArtists) > 0 {
		problems = append(problems, unavailable("artist", skipArtists))
	}
	if len(skipReleases) > 0 {
		problems = append(problems, unavailable("release", skipReleases))
	}
	if len(problems) == 0 {
		return tracks, nil
	}
	return tracks, errors.New(strings.Join(problems, "; "))
}

// unavailable renders a skip list for an error message: up to three names,
// then a count, so a bad day does not turn the status line into a wall of
// titles. what is the singular noun ("artist"); it is pluralized here when
// more than one is named, so "1 artist" and "3 artists" both read right.
func unavailable(what string, names []string) string {
	if len(names) > 1 {
		what += "s"
	}
	if len(names) > 3 {
		return fmt.Sprintf("%d %s unavailable (%s, %d more)", len(names), what, strings.Join(names[:3], ", "), len(names)-3)
	}
	return fmt.Sprintf("%d %s unavailable (%s)", len(names), what, strings.Join(names, ", "))
}
