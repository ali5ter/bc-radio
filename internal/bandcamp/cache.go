package bandcamp

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
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

// get returns the cached page for u if a fresh entry exists.
func (pc *pageCache) get(u string) (string, bool) {
	if pc.dir == "" {
		return "", false
	}
	p := pc.path(u)
	fi, err := os.Stat(p)
	if err != nil || time.Since(fi.ModTime()) > pc.ttl {
		return "", false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// put stores the page for u. A missing directory is created on first use;
// write errors are ignored (the cache is optional).
func (pc *pageCache) put(u, page string) {
	if pc.dir == "" {
		return
	}
	if err := os.MkdirAll(pc.dir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(pc.path(u), []byte(page), 0o644)
}

// path maps an absolute URL to its cache file name.
func (pc *pageCache) path(u string) string {
	sum := sha256.Sum256([]byte(u))
	return filepath.Join(pc.dir, hex.EncodeToString(sum[:]))
}
