package bandcamp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		opts    Options
		wantErr bool
	}{
		{name: "empty username", opts: Options{}, wantErr: true},
		{name: "blank username", opts: Options{Username: "   "}, wantErr: true},
		{name: "minimal", opts: Options{Username: "deerhoof"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := New(tt.opts)
			if tt.wantErr {
				if err == nil {
					t.Fatal("New: want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if c.baseURL != defaultBaseURL {
				t.Errorf("baseURL = %q, want %q", c.baseURL, defaultBaseURL)
			}
			if c.testOrigin {
				t.Error("testOrigin = true, want false (default BaseURL)")
			}
			if c.backoff != retryBaseBackoff {
				t.Errorf("backoff = %v, want %v", c.backoff, retryBaseBackoff)
			}
		})
	}
}

func TestNewCustomBaseURL(t *testing.T) {
	t.Parallel()
	c, err := New(Options{Username: "u", BaseURL: "http://127.0.0.1:9"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !c.testOrigin {
		t.Error("testOrigin = false, want true for explicit BaseURL")
	}
	if got := c.pageURL("mrtw.bandcamp.com", "/music"); got != "http://127.0.0.1:9/mrtw/music" {
		t.Errorf("pageURL = %q, want %q", got, "http://127.0.0.1:9/mrtw/music")
	}
	if got := c.pageURL("mrtw.com", "/music"); got != "http://127.0.0.1:9/mrtw.com/music" {
		t.Errorf("pageURL (custom domain) = %q, want %q", got, "http://127.0.0.1:9/mrtw.com/music")
	}
	if got := c.followingURL(); got != "http://127.0.0.1:9/u/following/artists_and_labels" {
		t.Errorf("followingURL = %q", got)
	}
}

func TestArtistHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		sub    string
		custom string
		want   string
	}{
		{name: "subdomain", sub: "mrtw", want: "mrtw.bandcamp.com"},
		{name: "custom domain wins", sub: "mrtw", custom: "mrtw.com", want: "mrtw.com"},
		{name: "neither", want: "bandcamp.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := artistHost(artistWith(tt.sub, tt.custom)); got != tt.want {
				t.Errorf("artistHost = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDoRetriesOn429(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c, err := New(Options{Username: "u", BaseURL: srv.URL, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.backoff = time.Millisecond
	if _, err := c.do(context.Background(), srv.URL+"/x"); err == nil {
		t.Fatal("do: want 429 error after exhausting retries")
	}
}

func TestDoRetriesThenSucceeds(t *testing.T) {
	t.Parallel()
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	c, err := New(Options{Username: "u", BaseURL: srv.URL, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.backoff = time.Millisecond
	resp, err := c.do(context.Background(), srv.URL+"/x")
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "ok" || calls != 3 {
		t.Errorf("body = %q, calls = %d; want ok, 3", body, calls)
	}
}

func TestDoHonoursContextCancel(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
	}))
	defer srv.Close()
	c, err := New(Options{Username: "u", BaseURL: srv.URL, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := c.do(ctx, srv.URL+"/x"); err == nil {
		t.Fatal("do: want context error")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("do took %v; the cancel should end the retry loop promptly", time.Since(start))
	}
	close(release)
}

func TestGetHTMLCachesToDisk(t *testing.T) {
	t.Parallel()
	var hits int
	srv := httptest.NewServer(countingHandler(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("<html>page</html>"))
	}, &hits))
	defer srv.Close()
	c, err := New(Options{Username: "u", BaseURL: srv.URL, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for i := 0; i < 2; i++ {
		page, err := c.getHTML(context.Background(), srv.URL+"/p", false)
		if err != nil {
			t.Fatalf("getHTML: %v", err)
		}
		if page != "<html>page</html>" {
			t.Fatalf("page = %q", page)
		}
	}
	if hits != 1 {
		t.Errorf("server hits = %d, want 1 (second call served from disk cache)", hits)
	}
}

func TestGetHTMLForceBypassesCache(t *testing.T) {
	t.Parallel()
	var hits int
	srv := httptest.NewServer(countingHandler(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("<html>page</html>"))
	}, &hits))
	defer srv.Close()
	c, err := New(Options{Username: "u", BaseURL: srv.URL, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.getHTML(context.Background(), srv.URL+"/p", false); err != nil {
		t.Fatalf("getHTML: %v", err)
	}
	if _, err := c.getHTML(context.Background(), srv.URL+"/p", true); err != nil {
		t.Fatalf("getHTML (force): %v", err)
	}
	if hits != 2 {
		t.Errorf("server hits = %d, want 2 (force bypasses the disk cache)", hits)
	}
}

func TestGetHTML404(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	c, err := New(Options{Username: "u", BaseURL: srv.URL, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.getHTML(context.Background(), srv.URL+"/missing", false)
	if err == nil || !strings.Contains(err.Error(), "page not found") {
		t.Errorf("err = %v, want a page-not-found error", err)
	}
}

func TestPageCache(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ttl  time.Duration
		age  time.Duration
		want bool
	}{
		{name: "fresh entry", ttl: time.Hour, age: time.Minute, want: true},
		{name: "stale entry", ttl: time.Minute, age: time.Hour},
		{name: "absent entry", ttl: time.Hour, want: false},
		{name: "no dir disables", ttl: time.Hour, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pc := &pageCache{dir: t.TempDir(), ttl: tt.ttl}
			if tt.name == "no dir disables" {
				pc.dir = ""
			}
			if tt.name == "absent entry" {
				_, ok := pc.get("https://x/y")
				if ok {
					t.Fatal("get on a fresh cache = ok, want miss")
				}
				return
			}
			pc.put("https://x/y", "page")
			if tt.age > 0 {
				p := pc.path("https://x/y")
				old := time.Now().Add(-tt.age)
				if err := os.Chtimes(p, old, old); err != nil {
					t.Fatalf("Chtimes: %v", err)
				}
			}
			got, ok := pc.get("https://x/y")
			if ok != tt.want {
				t.Fatalf("get ok = %v, want %v", ok, tt.want)
			}
			if tt.want && got != "page" {
				t.Errorf("get = %q, want page", got)
			}
		})
	}
}

func TestPageCachePath(t *testing.T) {
	t.Parallel()
	pc := &pageCache{dir: "/cache"}
	p := pc.path("https://x/album/y")
	if !filepath.IsAbs(p) || !strings.HasPrefix(p, "/cache/") {
		t.Errorf("path = %q, want under /cache/", p)
	}
	if len(filepath.Base(p)) != 64 {
		t.Errorf("path basename = %q, want a 64-hex-char SHA-256", filepath.Base(p))
	}
}
