// Package bandcamp implements contract.Library and contract.AudioSource
// against Bandcamp's web pages.
//
// It is a scraper, not an API client (live-verified in the Phase 0 spike): the
// official api/tralbum/* endpoints return a 45-byte "bad function" error and
// must never be used. The real data sources are HTML-embedded JSON:
//
//   - the following page's data-blob (bandcamp.com/{user}/following/
//     artists_and_labels) for followed artists;
//   - the artist /music page's markup for the release list;
//   - the album page's data-tralbum JSON for tracks and signed stream URLs.
//
// Stream URLs are signed and expire about 24 hours after the album page was
// rendered. The client treats them as disposable: it caches them in memory
// with their expiry, re-fetches the album page on expiry or fetch failure,
// and backs off on HTTP 429. Playback is auth-agnostic (spike finding 3), so
// the session cookies are optional; the only required configuration is the
// Bandcamp username.
//
// v1 limitation: single-track releases are not playable. Track pages
// (/{host}/track/{slug}) return 404, so /music cards of kind track-* are
// skipped and Albums returns albums only.
//
// One interpretation note for the frozen contract: contract.Track.Subdomain
// carries the artist's full host ("mrtwinsister.bandcamp.com", or a custom
// domain verbatim) rather than a bare subdomain, because an artist on a
// custom domain has no subdomain and the field is the client's refetch
// location. The contract's own comment — "artist host for stream-URL
// fetches" — is what this package honours.
//
// The sole external dependency is golang.org/x/net/html, used for tolerant
// HTML parsing: Bandcamp's markup is machine-generated but not guaranteed
// well-formed, and a real parser beats string surgery.
package bandcamp
