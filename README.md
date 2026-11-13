# bc-radio

A local-first terminal radio for the bands you follow on Bandcamp. bc-radio builds a track
index from your follows and their discography, plays it as random radio — or whatever else
you queue — streams the tracks over your system audio, and renders a 60 fps interface with
live spectrum, waveform and particle visualizers.

- Frozen contracts between the three packages: `internal/contract/`
- Test fixtures: `testdata/`

## Requirements

- Go 1.27 or newer
- cgo (the audio device layer uses [oto](https://github.com/hajimehoshi/oto), which needs a C
  compiler; on macOS that is the built-in toolchain, on Linux `libasound2-dev` or
  `libpulse-dev`)
- An interactive terminal

## Build

```bash
CGO_ENABLED=1 go build -o bc-radio ./cmd/bc-radio
```

## Usage

```bash
./bc-radio -username yourbandcampname
```

bc-radio opens on the player screen with the radio on (toggle with `d`). At startup it reads
your followed bands and builds a flat track index from their discography — the status line
shows the build, then the radio's state and the size of the index it draws from. Press
`space` and it plays a random index track; as each track ends, it picks another, never the
one that just ended.

Explicitly queued tracks always beat the radio: press `l` to browse the library, pick a track
(and press `enter` to queue it) or an album (`enter` queues every track of it), and the
queued tracks play in order before the radio resumes. Enter on an artist simply opens their
releases.

| Flag | Meaning |
| --- | --- |
| `-username` | The Bandcamp account whose followed bands to play (required) |
| `-session` | Bandcamp session cookie (optional; playback works without it) |
| `-identity` | Bandcamp identity cookie (optional; must be given with `-session`) |
| `-cache-dir` | Page cache directory (default `$XDG_CACHE_HOME/bc-radio`) |
| `-page-ttl` | How long parsed Bandcamp pages stay in the cache (default `24h`) |
| `-volume` | Starting volume, 0 to 1 (default `1`) |

Outside a terminal, bc-radio exits with an actionable error instead of emitting escape
sequences into the pipe.

## Keybindings

| Key | Action | Where |
| --- | --- | --- |
| `space` | Play / pause | Player |
| `n` | Next track | Player |
| `p` | Previous track, or restart if within 5 s | Player |
| `s` | Shuffle on / off | Player |
| `r` | Repeat on / off | Player |
| `d` | Radio on / off (random tracks from your follows) | Player |
| `v` | Next visualizer (spectrum → waveform → particles) | Player |
| `f` | Fullscreen visualizer | Player |
| `l` | Library | Player |
| `+` / `-` | Volume up / down | Player |
| `↑` / `↓` | Select | Library |
| `←` / `→` | Change column (artists → albums → tracks) | Library |
| `enter` | Open the selected artist, or queue the selected album or track | Library |
| `x` | Remove the selected track from the queue | Library |
| `esc` | Back to the player | Library |
| `?` | This keybinding overlay (any key dismisses it) | Anywhere |
| `q` / `ctrl+c` | Quit | Anywhere |

## How it works

Bandcamp has no public consumer API, so the client reads the JSON embedded in the pages
themselves: `data-blob` on your following page and `data-tralbum` on each album page. Parsed
pages are cached on disk for 24 h (Bandcamp's markup is fragile; the cache is the
stability layer). Stream URLs are signed and expire in about a day, so a failed stream
refetches the album page with a cache-busting query and stores the result under the canonical
URL — the failure self-heals the disk cache. HTTP 429 responses back off before retrying.

The radio is the default mode. At startup bc-radio walks every followed band's discography —
artists, releases, then album tracks — into a flat index, reading the same cached pages as
the library browser, and the status line reports its size. With the radio on, each
transition picks a random index track, avoiding the one that just ended; an explicit queue
or repeat-one always plays first. With the radio off, the player loops the tracks you have
gathered, like any other queue.

Tracks decode in pure Go (`beep/mp3`, which wraps `hajimehoshi/go-mp3`) and play through oto.
The queue pre-decodes the next track while the current one plays for gapless transitions, and
a 16-band Goertzel analyser turns the output into band frames that the interface eases into its
60 fps render to drive the visualizers.

The terminal needs to be at least 30 × 8 for the player screen and 60 × 10 for the library;
below that, bc-radio shows a notice instead of a broken layout.
