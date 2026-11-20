package bandcamp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// pageCache is a best-effort on-disk cache of fetched page HTML, keyed by
// the SHA-256 of the absolute page URL. Bandcamp's markup is fragile, so a
// cached page hedges against layout churn as much as against re-fetching;
// entries older than TTL are re-fetched. All failures are silent — the
// cache may never be allowed to break a fetch.
type pageCache struct {
	dir string // empty disables the cache
	ttl time.Duration
}

// pageCacheEntry is the on-disk form of one cached page: the body, and
// the URL the request ended at after redirects. The final URL lets a
// cached read know what the page actually is — a /music request answered
// by a 303 to a single release caches the release page under the /music
// key, and a later read must still recognise that (see Albums).
type pageCacheEntry struct {
	Final string `json:"final"`
	Page  string `json:"page"`
}

// get returns the cached page and its final URL for u if a fresh entry
// exists. A file in a format this build does not write (an older version
// stored bare HTML) fails the decode and reads as a miss: the page is
// re-fetched once and re-stored in the current form.
func (pc *pageCache) get(u string) (page, final string, ok bool) {
	if pc.dir == "" {
		return "", "", false
	}
	p := pc.path(u)
	fi, err := os.Stat(p)
	if err != nil || time.Since(fi.ModTime()) > pc.ttl {
		return "", "", false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", "", false
	}
	var e pageCacheEntry
	if err := json.Unmarshal(b, &e); err != nil {
		return "", "", false
	}
	return e.Page, e.Final, true
}

// put stores the page and its final URL for u. A missing directory is
// created on first use; write errors are ignored (the cache is optional).
func (pc *pageCache) put(u, page, final string) {
	if pc.dir == "" {
		return
	}
	if err := os.MkdirAll(pc.dir, 0o755); err != nil {
		return
	}
	// The page is HTML: suppress the encoder's <>& escaping, or a 300 KB
	// page would balloon on disk for no gain.
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(pageCacheEntry{Final: final, Page: page}); err != nil {
		return
	}
	_ = os.WriteFile(pc.path(u), []byte(sb.String()), 0o644)
}

// path maps an absolute URL to its cache file name.
func (pc *pageCache) path(u string) string {
	sum := sha256.Sum256([]byte(u))
	return filepath.Join(pc.dir, hex.EncodeToString(sum[:]))
}
