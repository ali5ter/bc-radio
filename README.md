# bc-radio

A local-first terminal radio for the bands you follow on Bandcamp. bc-radio builds a track
index from your follows and their discographies, plays it as random radio — or whatever else
you queue — and streams the tracks over your system audio: everything you hear comes from
Bandcamp, the platform where your follows, likes, and purchases support the artists. The
interface renders at 60 fps with live visualizers: a bar spectrum, dust mapped to the bands,
a radial meter, and an oscilloscope of the raw output.

- The product specification and the spike findings are private documents; the public record
  is [the changelog](CHANGELOG.md) and the source
- The packages: `internal/bandcamp` (the Bandcamp client), `internal/audio` (playback and
  analysis), `internal/tui` (the interface), with the frozen contracts between them in
  `internal/contract/`
- Test fixtures: `testdata/` (captured Bandcamp pages for the schema tests; the MP3s are
  generated test tones)

## Requirements

- Go 1.27 or newer
- cgo (the audio device layer uses [oto](https://github.com/hajimehoshi/oto), which needs a C
  compiler; on macOS that is the built-in toolchain, on Linux `libasound2-dev` or
  `libpulse-dev`)
- An interactive terminal

## Install

```bash
brew install ali5ter/tap/bc-radio
```

Pre-built macOS binaries (arm64 and amd64) are attached to
[the GitHub releases](https://github.com/ali5ter/bc-radio/releases).

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
| `-theme` | Interface theme: `auto` (follows the terminal), `dark`, `light` or `mono` |

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
| `v` | Next visualizer (spectrum → particles → radial → scope) | Player |
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

## Themes

`-theme` chooses the palette: `dark` for dark terminals, `light` for light ones, and `mono`
renders everything in the terminal's default colour, with the hierarchy carried by bold and
faint instead. The default, `auto`, follows the terminal: `NO_COLOR` resolves to mono, and
otherwise the palette matches the terminal's background colour. An explicit choice never
moves.

## How it works

Bandcamp has no public consumer API, so the client reads the JSON Bandcamp embeds in its own
pages — your following list and each album page. Parsed pages are cached on disk for 24
hours (by default) so restarts are fast and a markup change does not take the library down.
Stream links are time-limited; when one expires, the client re-resolves the album page for a
fresh link, and a failed stream shows as a brief notice before the radio moves on. Repeated
failures back off before retrying.

Nothing is stored for later: tracks are streamed and played as they arrive, and the only
local state is the page cache above.

The radio is the default mode. At startup bc-radio walks every followed band's discography —
artists, releases, then album tracks — into a flat index, reading the same cached pages as
the library browser, and the status line reports its size. With the radio on, each
transition picks a random index track, avoiding the one that just ended; an explicit queue
or repeat-one always plays first. With the radio off, the player loops the tracks you have
gathered, like any other queue.

Tracks decode in pure Go (`beep/mp3`, which wraps `hajimehoshi/go-mp3`) and play through oto.
The queue pre-decodes the next track while the current one plays for gapless transitions.
Analysis is pure Go as well: a Hann-windowed FFT of 2048 samples, half-overlapped, folded
into the 16 bands on a log (decibel) scale, so a loud passage reads tall and a quiet one
short — the visualizers show the music's own dynamics. The audio device refills its buffer
in bursts, so the analyser keeps a rolling copy of everything it is given and anchors its
reads on what is audible right now: the motion tracks the music at a steady cadence instead
of in step with the buffer. The interface eases each band toward its target at the
terminal's clock rate, and the oscilloscope draws the analyser's most recent samples raw,
sample by sample.

The terminal needs to be at least 30 × 8 for the player screen and 60 × 10 for the library;
below that, bc-radio shows a notice instead of a broken layout.
