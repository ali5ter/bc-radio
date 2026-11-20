package bandcamp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ali5ter/bc-radio/internal/contract"
)

// urlKey addresses one signed stream URL: the release and track it belongs
// to.
type urlKey struct {
	albumID int64
	trackID int64
}

// urlEntry is a cached signed stream URL with its expiry, read from the
// URL's ts query parameter (unix seconds, about 24 h after the album page
// was rendered).
type urlEntry struct {
	url     string
	expires time.Time
}

// urlCache is the in-memory signed-URL cache. Signed URLs are disposable
// (spike finding 3), so entries are never served past their expiry, safety
// margin included; the caller re-resolves from the album page instead.
type urlCache struct {
	mu      sync.Mutex
	entries map[urlKey]urlEntry
}

// get returns the entry for k if present and still valid.
func (c *urlCache) get(k urlKey) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[k]
	if !ok {
		return "", false
	}
	if time.Now().Add(signedURLSafetyMargin).After(e.expires) {
		return "", false
	}
	return e.url, true
}

// put stores a signed URL, deriving its expiry from the ts parameter. A
// URL without a parseable ts is kept with the full default lifetime.
func (c *urlCache) put(k urlKey, u string) {
	expires := signedURLExpiry(u)
	if expires.IsZero() {
		expires = time.Now().Add(24 * time.Hour)
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[urlKey]urlEntry)
	}
	c.entries[k] = urlEntry{url: u, expires: expires}
	c.mu.Unlock()
}

// invalidate drops the entry for k, after a failed stream fetch.
func (c *urlCache) invalidate(k urlKey) {
	c.mu.Lock()
	delete(c.entries, k)
	c.mu.Unlock()
}

// signedURLExpiry reads the ts query parameter (unix seconds) of a signed
// stream URL; the zero time when absent or malformed.
func signedURLExpiry(u string) time.Time {
	parsed, err := url.Parse(u)
	if err != nil {
		return time.Time{}
	}
	n, err := strconv.ParseInt(parsed.Query().Get("ts"), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

// Open opens the encoded audio of a track. It implements
// contract.AudioSource; the consumer must close the returned Body.
//
// The resolution chain, per the verified stream-URL lifecycle:
//
//  1. the in-memory URL cache, while the signed URL is still valid;
//  2. the release page (disk page cache) — the album page for an album
//     track, the /track/ page for a single — re-parsing data-tralbum;
//  3. a forced fresh page fetch, when the cached page's URL has already
//     expired;
//  4. if the stream fetch itself fails — the HTTP exchange, a truncated
//     body, or a length mismatch — the URL is invalidated and the chain
//     runs once more.
//
// The body is downloaded completely before Open returns. The audio engine
// decodes in real time (a second of audio per second of wall time), so a
// body read live from the network would keep one HTTP connection open for
// the whole track — anything that severs a slowly draining response
// (a CDN edge, a router, a network blip) silently truncated tracks
// mid-playback. A fully buffered body makes mid-track truncation
// impossible: the download completes at full speed in a few seconds, and
// playback decodes from memory.
//
// A track must carry its location (Subdomain host + a release slug of
// either kind + ID); a track built anywhere else is rejected with a
// clear error.
func (c *Client) Open(ctx context.Context, t contract.Track) (contract.AudioStream, error) {
	if t.Subdomain == "" || (t.AlbumSlug == "" && t.TrackSlug == "") || t.ID == "" {
		return contract.AudioStream{}, fmt.Errorf(
			"bandcamp: track %q carries no location (subdomain/slug/id); only tracks built by this client can be opened", t.Title)
	}
	key, err := urlKeyFor(t)
	if err != nil {
		return contract.AudioStream{}, err
	}
	streamURL, err := c.streamURL(ctx, t, key, false)
	if err != nil {
		return contract.AudioStream{}, err
	}
	data, length, err := c.fetchBody(ctx, streamURL)
	if err != nil {
		c.urls.invalidate(key)
		streamURL, err = c.streamURL(ctx, t, key, true)
		if err != nil {
			return contract.AudioStream{}, err
		}
		if data, length, err = c.fetchBody(ctx, streamURL); err != nil {
			return contract.AudioStream{}, err
		}
	}
	return contract.AudioStream{Body: &memBody{bytes.NewReader(data)}, ContentLength: length}, nil
}

// memBody is the in-memory stream body: a seekable buffer with a no-op
// Close (there is nothing to release). It implements io.Seeker in addition
// to io.ReadCloser, so the audio engine's MP3 decoder can index it —
// reporting the track's exact length and letting the ←/→ keys seek within
// it. A plain NopCloser would read fine but not seek.
type memBody struct {
	*bytes.Reader
}

// Close implements io.Closer: a memory buffer has nothing to release.
func (b *memBody) Close() error { return nil }

// fetchBody fetches u and reads the whole response body, validating it
// against the ContentLength the server committed to. It fails on any read
// error or a short read: a partially delivered stream must not be
// presented as a complete track (the decoder would then end the track
// wherever the data ran out, a few seconds or minutes early). The body is
// returned for the caller to serve from memory.
func (c *Client) fetchBody(ctx context.Context, u string) (data []byte, length int64, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, -1, fmt.Errorf("bandcamp: stream %s: %w", u, err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.stream.Do(req)
	if err != nil {
		return nil, -1, fmt.Errorf("bandcamp: stream %s: %w", u, err)
	}
	if resp.StatusCode >= 400 {
		drainClose(resp)
		return nil, -1, fmt.Errorf("bandcamp: stream %s: HTTP %d", u, resp.StatusCode)
	}
	defer resp.Body.Close()
	data, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, -1, fmt.Errorf("bandcamp: stream %s: reading the body: %w", u, err)
	}
	if resp.ContentLength >= 0 && int64(len(data)) != resp.ContentLength {
		return nil, -1, fmt.Errorf("bandcamp: stream %s: the body ended after %d of %d bytes", u, len(data), resp.ContentLength)
	}
	length = int64(len(data))
	if resp.ContentLength < 0 {
		length = -1 // the server committed to no length; the buffer is what it is
	}
	return data, length, nil
}

// urlKeyFor builds the URL-cache key from a track's AlbumID and ID.
func urlKeyFor(t contract.Track) (urlKey, error) {
	id, err := strconv.ParseInt(t.ID, 10, 64)
	if err != nil {
		return urlKey{}, fmt.Errorf("bandcamp: track %q has non-numeric ID %q", t.Title, t.ID)
	}
	return urlKey{albumID: t.AlbumID, trackID: id}, nil
}

// streamURL returns a currently valid signed stream URL for a track.
//
// Resolution is: URL cache, then the release page (via the disk page cache
// unless force is set), then the URL cache again. If the page parses but
// carries no usable URL for the track — the signature expired while the
// page sat in the cache, or the release is private or removed — and force
// is false, the page is fetched fresh with the cache bypassed, because a
// fresh page should carry a freshly signed URL. force true (a previous
// stream fetch failed) skips the page-cache read and does not loop beyond
// the single fresh fetch. Every fresh fetch uses a cache-busted copy of
// the page URL: the CDN edge caches on the full URL, so without the buster
// a "fresh" refetch could still be served the same stale render whose
// signature the edge already rejected (verified live against Bandcamp's
// mid-migration signing schemes, 2026-11-12).
func (c *Client) streamURL(ctx context.Context, t contract.Track, key urlKey, force bool) (string, error) {
	if !force {
		if u, ok := c.urls.get(key); ok {
			return u, nil
		}
	}
	pageURL := c.pageURL(t.Subdomain, releasePagePath(t))
	var (
		page string
		err  error
	)
	if force {
		page, err = c.freshPage(ctx, pageURL)
	} else {
		page, _, err = c.getHTML(ctx, pageURL, false)
	}
	if err != nil {
		return "", err
	}
	u, ok, err := c.resolveFromPage(t, key, page)
	if err != nil {
		return "", err
	}
	if ok {
		return u, nil
	}
	if force {
		return "", noValidStreamURL(t)
	}
	page, err = c.freshPage(ctx, pageURL)
	if err != nil {
		return "", err
	}
	u, ok, err = c.resolveFromPage(t, key, page)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", noValidStreamURL(t)
	}
	return u, nil
}

// freshPage fetches a fresh render of the canonical page at u: the GET
// carries a cache-buster query parameter, so a CDN edge that is still
// serving a stale render of the canonical URL must re-render from origin,
// and the result is stored under the canonical key so later uncached reads
// get the fresh page (with its freshly signed URLs) instead of the stale
// one.
func (c *Client) freshPage(ctx context.Context, u string) (string, error) {
	page, final, err := c.fetchHTML(ctx, cacheBust(u))
	if err != nil {
		return "", err
	}
	c.cache.put(u, page, final)
	return page, nil
}

// cacheBust returns u with a unique cache-buster query parameter, so a
// cache that keys on the URL treats it as a new page.
func cacheBust(u string) string {
	sep := "?"
	if strings.ContainsRune(u, '?') {
		sep = "&"
	}
	return u + sep + "_=" + strconv.FormatInt(time.Now().UnixNano(), 10)
}

// resolveFromPage parses a release page, caches every signed stream URL it
// finds, and reports whether the target track now has a usable one. A
// (zero, false, nil) result means the page parsed but had nothing usable
// for the track.
func (c *Client) resolveFromPage(t contract.Track, key urlKey, page string) (string, bool, error) {
	release, err := parseTralbum(page)
	if err != nil {
		return "", false, fmt.Errorf("bandcamp: re-resolve stream for %s: %w", t.Title, err)
	}
	for trackID, u := range release.URLs {
		c.urls.put(urlKey{albumID: release.Album.ID, trackID: trackID}, u)
	}
	u, ok := c.urls.get(key)
	return u, ok, nil
}

// releasePagePath is the page path that carries t's stream URL: the
// album page for an album track, the /track/ page for a single (the
// single's own page is its data-tralbum source).
func releasePagePath(t contract.Track) string {
	if t.TrackSlug != "" {
		return "/track/" + t.TrackSlug
	}
	return "/album/" + t.AlbumSlug
}

// noValidStreamURL is the error reported when even a freshly fetched
// release page carries no usable stream URL for a track.
func noValidStreamURL(t contract.Track) error {
	return fmt.Errorf("bandcamp: no valid stream URL for %q (%s%s); the release may be private or removed",
		t.Title, t.Subdomain, releasePagePath(t))
}
