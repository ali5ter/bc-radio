package bandcamp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bc-radio/internal/contract"
)

// userAgent masquerades as desktop Chrome: Bandcamp serves its full
// server-rendered markup to it, and the spike verified the data blobs are
// identical with and without a session.
const userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"

const (
	defaultBaseURL = "https://bandcamp.com"
	defaultTimeout = 30 * time.Second

	// Signed stream URLs expire about 24 hours after the album page was
	// rendered; refetch well before then so a long session never serves
	// a dead URL.
	signedURLSafetyMargin = 1 * time.Hour

	// defaultPageTTL bounds the disk page cache. The markup is the fragile
	// part, not the network; a day of freshness is plenty.
	defaultPageTTL = 24 * time.Hour

	// retryBaseBackoff and retryMaxBackoff bound the 429/5xx retry loop
	// in do: 1s, 2s, 4s, 8s, 16s (five retries), then an error.
	retryBaseBackoff = 1 * time.Second
	retryMaxBackoff  = 16 * time.Second
	maxRetries       = 5
)

// Options configures a Client. Username is the only required field.
type Options struct {
	// Username is the Bandcamp account whose follows to load.
	Username string

	// Session and Identity are the Bandcamp session cookies, read from
	// the browser cookie store (spike finding 5). Both are optional and
	// only meaningful together; playback works without them.
	Session  string
	Identity string

	// BaseURL overrides https://bandcamp.com. When set, artist hosts are
	// addressed as path prefixes under it (mrtw.bandcamp.com/music becomes
	// {BaseURL}/mrtw/music) so every request shares one origin — the
	// escape hatch tests use to route fixtures through httptest.
	BaseURL string

	// CacheDir is the on-disk page cache directory. It defaults to
	// $XDG_CACHE_HOME/bc-radio (or os.UserCacheDir).
	CacheDir string

	// PageTTL bounds the disk page cache; it defaults to 24h.
	PageTTL time.Duration
}

// Client is a Bandcamp scraper. It implements contract.Library and
// contract.AudioSource and is safe for concurrent use.
type Client struct {
	username   string
	baseURL    string
	testOrigin bool // BaseURL was explicitly set: rewrite artist hosts to path prefixes
	session    string
	identity   string
	http       *http.Client // pages: full 30s request timeout
	stream     *http.Client // audio: header deadline only, the body streams at real time
	backoff    time.Duration
	cache      *pageCache
	urls       *urlCache
}

// New builds a Client. It fails when Username is empty.
func New(opts Options) (*Client, error) {
	if strings.TrimSpace(opts.Username) == "" {
		return nil, errors.New("bandcamp: Options.Username is required")
	}
	base := opts.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	ttl := opts.PageTTL
	if ttl <= 0 {
		ttl = defaultPageTTL
	}
	dir := opts.CacheDir
	if dir == "" {
		dir = defaultCacheDir()
	}
	return &Client{
		username:   opts.Username,
		baseURL:    strings.TrimRight(base, "/"),
		testOrigin: opts.BaseURL != "",
		session:    opts.Session,
		identity:   opts.Identity,
		http:       &http.Client{Timeout: defaultTimeout},
		stream:     streamClient(),
		backoff:    retryBaseBackoff,
		cache:      &pageCache{dir: dir, ttl: ttl},
		urls:       &urlCache{},
	}, nil
}

// defaultCacheDir is the on-disk page cache location: the user cache
// directory, empty (cache disabled) when it cannot be determined.
func defaultCacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "bc-radio")
}

// followingURL is the absolute URL of the user's following page.
func (c *Client) followingURL() string {
	return c.baseURL + "/" + c.username + "/following/artists_and_labels"
}

// pageURL is the absolute URL of a page on an artist host. In production
// the host is a DNS name; with an explicit BaseURL it becomes a path
// prefix under that origin so tests can route it through httptest.
func (c *Client) pageURL(host, path string) string {
	if c.testOrigin {
		return c.baseURL + "/" + strings.TrimSuffix(host, ".bandcamp.com") + path
	}
	return "https://" + host + path
}

// artistHost resolves the artist's web host: the custom domain verbatim,
// otherwise subdomain + ".bandcamp.com" (the two are mutually exclusive in
// contract.Artist).
func artistHost(a contract.Artist) string {
	if a.CustomDomain != "" {
		return a.CustomDomain
	}
	if a.Subdomain != "" {
		return a.Subdomain + ".bandcamp.com"
	}
	return "bandcamp.com"
}

// do performs a GET with the browser user agent and the optional session
// cookies. HTTP 429 and 5xx responses are retried with exponential backoff
// (one to five retries); any other status, including 404, is returned as
// is for the caller to interpret.
func (c *Client) do(ctx context.Context, u string) (*http.Response, error) {
	backoff := c.backoff
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("bandcamp: fetch %s: %w", u, ctx.Err())
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, retryMaxBackoff)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, fmt.Errorf("bandcamp: fetch %s: %w", u, err)
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		if c.session != "" {
			req.AddCookie(&http.Cookie{Name: "session", Value: c.session})
		}
		if c.identity != "" {
			req.AddCookie(&http.Cookie{Name: "identity", Value: c.identity})
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if attempt == maxRetries {
				return nil, fmt.Errorf("bandcamp: fetch %s: %w", u, err)
			}
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			drainClose(resp)
			if attempt == maxRetries {
				return nil, fmt.Errorf("bandcamp: fetch %s: HTTP %d", u, resp.StatusCode)
			}
			continue
		}
		return resp, nil
	}
}

// getHTML fetches a page as HTML, served from the disk page cache when a
// fresh entry exists, unless force is set.
func (c *Client) getHTML(ctx context.Context, u string, force bool) (string, error) {
	if !force {
		if page, ok := c.cache.get(u); ok {
			return page, nil
		}
	}
	resp, err := c.do(ctx, u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("bandcamp: page not found: %s", u)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bandcamp: fetch %s: HTTP %d", u, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("bandcamp: read %s: %w", u, err)
	}
	page := string(body)
	c.cache.put(u, page)
	return page, nil
}

// drainClose discards a response body and closes it, for responses that
// are being retried or rejected.
func drainClose(resp *http.Response) {
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

// streamClient builds the HTTP client for audio streams. A plain
// http.Client.Timeout would sever every track: the timeout covers the whole
// request, including the body read (verified against net/http: "Client.
// Timeout or context cancellation while reading body"), and the audio
// engine reads a body in real time, so any track longer than the timeout
// would die mid-playback. Instead the client bounds only the request setup
// with a ResponseHeaderTimeout — a dead signed URL still fails within 30s —
// and leaves the body bound to the caller's context alone. HTTP/2 is
// disabled because ResponseHeaderTimeout is not enforced for h2 requests; a
// single audio stream gets nothing from h2, so h1 is the right trade.
func streamClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = false
	transport.ResponseHeaderTimeout = defaultTimeout
	return &http.Client{Transport: transport}
}

// fetchStream GETs a signed stream URL. No cookies are needed (spike
// finding 3: playback is auth-agnostic), but the browser user agent is
// sent anyway — it has been verified to work and costs nothing. It uses
// the header-deadline stream client (see streamClient): the body is read
// at real time by the audio engine and must survive beyond defaultTimeout.
func (c *Client) fetchStream(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("bandcamp: stream %s: %w", u, err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.stream.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bandcamp: stream %s: %w", u, err)
	}
	if resp.StatusCode >= 400 {
		drainClose(resp)
		return nil, fmt.Errorf("bandcamp: stream %s: HTTP %d", u, resp.StatusCode)
	}
	return resp, nil
}
