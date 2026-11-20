// Package bandcamp implements contract.Library and contract.AudioSource
// against Bandcamp's web pages.
//
// It is a scraper, not an API client (live-verified in the Phase 0 spike): the
// official api/tralbum/* endpoints return a 45-byte "bad function" error and
// must never be used. The real data sources are HTML-embedded JSON:
//
//   - the following page's data-blob (bandcamp.com/{user}/following/
//     artists_and_labels) for followed artists;
//   - the artist /music page's markup for the release list — which
//     303-redirects to the release itself for an artist with exactly one
//     release, a page the client recovers from (Albums);
//   - the album page's data-tralbum JSON for tracks and signed stream URLs;
//   - a single's /track/ page, whose data-tralbum carries the one track
//     and its stream URL the same way (verified live 2026-11-19).
//
// Stream URLs are signed and expire about 24 hours after the album page was
// rendered. The client treats them as disposable: it caches them in memory
// with their expiry, re-fetches the album page on expiry or fetch failure,
// and backs off on HTTP 429. Playback is auth-agnostic (spike finding 3), so
// the session cookies are optional; the only required configuration is the
// Bandcamp username.
//
// Singles are in: /music cards of kind track-* are single-track
// releases, grouped into one synthetic "singles" album per artist (the
// library lists them under one entry, not one row per single). Each
// single's /track/ page carries a full data-tralbum with a streamable
// track, so a single is a release with one track and the stream path
// re-fetches its /track/ page for a fresh URL, exactly as it does for
// an album. (History: the spike hit 404s on the track pages it tried,
// which made v1 skip them; at least some /track/ pages do carry the
// blob, verified live 2026-11-19 on TONTRAKUL's singles, so the skip
// became a scope decision — Alister lifted it 2026-11-19.)
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
