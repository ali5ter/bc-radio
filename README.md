# bc-radio

A TUI based radio that plays tracks of the bands you follow on Bandcamp.

bc-radio builds a track index from your follows and their discographies, plays it as random
radio (or a queue of albums or tracks you choose) and streams the music over your system
audio.

Everything you hear comes from Bandcamp, the platform where your follows, likes, and purchases support the artists.

![bc-radio demo](examples/bc-radio-demo.gif)

## Install

```bash
brew install ali5ter/tap/bc-radio
```

Pre-built macOS binaries (arm64 and amd64) are attached to
[the GitHub releases](https://github.com/ali5ter/bc-radio/releases).

## Build

The build requires:

- Go 1.27 or newer
- cgo (the audio device layer uses [oto](https://github.com/hajimehoshi/oto), which needs a C
  compiler; on macOS that is the built-in toolchain, on Linux `libasound2-dev` or
  `libpulse-dev`)
- An interactive terminal (the `-json` and `-list` index exports work off one, in a pipe)

then:

```bash
CGO_ENABLED=1 go build -o bc-radio ./cmd/bc-radio
```

## Usage

```bash
./bc-radio -u yourbandcampname
```

bc-radio opens on the player screen with the radio on (toggle with `d`).

At startup it reads your followed bands and builds a flat track index from their
discography. Then the radio starts a random index track; press `space` to pause, and as
each track ends it picks another. Tracks hand over with a clean gapless cut by default;
with `-crossfade 2s` the last 2 s of a track blend into the next one on an equal-power
curve instead, and a manual next (`n`) fades into its already-downloaded successor over
that same window instead of waiting for a fresh fetch. The cursor keys seek the current
track ten seconds back or forward.

Explicitly queued tracks always beat the radio: press `l` to browse the library, pick a
track (and press `enter` to queue it) or an album (`enter` queues every track of it), and
the queued tracks play in order before the radio resumes.

Stopping the radio (toggle with `d`) simply means only play tracks you've queued and don't pick them randomly.

| Flag | Meaning |
| --- | --- |
| `-username` | The Bandcamp account whose followed bands to play (required) |
| `-session` | Bandcamp session cookie (optional; playback works without it) |
| `-identity` | Bandcamp identity cookie (optional; must be given with `-session`) |
| `-cache-dir` | Page cache directory (default `$XDG_CACHE_HOME/bc-radio`) |
| `-page-ttl` | How long parsed Bandcamp pages stay in the cache (default `24h`) |
| `-volume` | Starting volume, 0 to 1 (default `1`) |
| `-crossfade` | How long tracks blend into each other on an equal-power curve (default `0`) |
| `-theme` | Interface theme: `auto` (follows the terminal), `dark`, `light` or `mono` — see Themes |
| `-json` | Build the track index, print it as JSON, and exit (no interface) |
| `-list` | Build the track index, print it as a plain-text list, and exit (no interface) |

## Index export

`-json` and `-list` build the same track index the radio plays — every followed band, its
releases, and their tracks with durations — print it, and exit without opening the TUI.

```bash
# The index as JSON: bands, then each band's albums, then each album's tracks
# (id, number, title, duration in seconds), with counts at each level.
./bc-radio -username yourbandcampname -json

# A plain-text list, readable top to bottom — the linear fallback for the
# 60 fps interface.
./bc-radio -username yourbandcampname -list
```

The index walks the page cache: parsed Bandcamp pages that stay on disk for `-page-ttl`
(24 h by default). To force a fresh walk, pass a short `-page-ttl` (`1s` re-fetches every
page) or point `-cache-dir` at an empty directory.

## Keybindings

| Key | Action | Where |
| --- | --- | --- |
| `space` | Play / pause | Player |
| `n` | Next track | Player |
| `p` | Previous track, or restart if within 5 s | Player |
| `s` | Shuffle on / off | Player |
| `r` | Repeat on / off | Player |
| `d` | Radio on / off (random tracks from your follows) | Player |
| `v` | Next visualizer (spectrum → particles → scope → interference) | Player |
| `f` | Fullscreen visualizer | Player |
| `l` | Library | Player |
| `+` / `-` | Volume up / down | Player |
| `←` / `→` | Seek 10 s back / forward | Player |
| `↑` / `↓` | Select | Library |
| `←` / `→` | Change column (artists → albums → tracks) | Library |
| `enter` | Open the selected artist, or queue the selected album or track | Library |
| `x` | Remove the selected track from the queue | Library |
| `esc` | Back to the player | Library |
| `?` | This keybinding overlay (any key dismisses it) | Anywhere |
| `q` / `ctrl+c` | Quit | Anywhere |

## Themes

`-theme` chooses the palette: `dark` for dark terminals, `light` for light ones, and
`mono` renders everything in the terminal's default colour. The default, `auto`, follows
the terminal: `NO_COLOR` resolves to mono, and otherwise the palette matches the
terminal's background colour. The theme is saved to a file (`$XDG_CONFIG_HOME/bc-radio/theme`, or
`~/.config/bc-radio/theme` when that is unset), so a later unflagged launch keeps it.
