// Package bandcamp implements the Bandcamp client. It is a scraper, not an
// API client (docs/spike.md): it parses the following page's data-blob
// JSON, the artist /music pages, and album pages' data-tralbum JSON;
// caches them locally; and opens signed stream URLs for playback. It
// implements contract.Library and contract.AudioSource.
package bandcamp
