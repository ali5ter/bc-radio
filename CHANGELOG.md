# Changelog

All notable changes to bc-radio are recorded here. Each release follows
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- A fifth visualizer, **metaballs** (`v` cycles to it after interference):
  blobs that drift, merge and split like hot liquid, after the MIT-licensed
  metaballs piece on ascii-magic.com. The blobs are the music — one per
  analyser band, the band's energy its size, its frequency its home
  altitude (bass low, treble high) — so the field's shape is the spectrum's
  shape. The field reads in a four-colour heat ramp, cool to warm from the
  edge in: a dim aura, a cool blue rim, a teal body, and a warm core at the
  scheme's spectrum end — so quiet passages hold sixteen small motes ringed
  in the aura, and a loud one is a mass of teal liquid with a copper heart.

### Changed

- The **interference** visualizer now renders on a seven-step density ramp
  (space, dot, middle-dot, and the four block weights) with a warm colour
  ramp from dim through the visual colour to the scheme's hot end, instead
  of the old four-step set that opened at a half-dark block. The quiet
  between the wave crests reads as a dusting of dots that fades into the
  dark, and only the crests reach the heavy blocks — the fan no longer
  tiles the panel like a wallpaper.

## [v1.4.0] - 2026-11-24

### Added

- A fourth visualizer, **interference** (`v` cycles to it after scope): two
  point sources send out circular waves that reinforce and cancel, carving
  the classic fan of nodal lines as the sources drift apart. The pattern is
  after the MIT-licensed wave-interference piece on ascii-magic.com,
  reversed out of its animation and wired to the analyser instead of the
  wall clock — the bass sets the ring density, the mid the emission rate,
  the treble how far the sources drift apart, and the overall level the
  field's amplitude, so a quiet passage reads as a faint concentric pulse
  and a loud one as a full interference fan.
- The demo reel is re-rendered and shows the new visualizer.

## [v1.3.2] - 2026-11-23

### Fixed

- Release catalogs beyond the first 16 releases are now read in full.
  Bandcamp's `/music` page ships its releases past the first 16 in a
  client-only data batch that the site's JavaScript renders into the
  grid while the static page markup omits, and the index read only the
  static markup — an artist like King Gizzard & The Lizard Wizard
  (32 releases) or Øresund Space Collective (108) came back with only
  its first 16, the rest missing from the radio.
- The demo reel is re-rendered against the current index.

### Notes

- A release with no streamable tracks (for example a vinyl-only
  release, whose page carries no track list) has nothing for the radio
  to play, so it does not appear in the index. That is upstream data,
  not a read failure — and it is why an artist's indexed catalog can be
  a few releases smaller than its Bandcamp page.

## [v1.3.1] - 2026-11-21

### Fixed

- Release cards whose link is an absolute URL (for example
  `https://host.bandcamp.com/album/slug`) are now read like relative
  links. Bandcamp serves both forms from the same `/music` listing, and
  the parser accepted only the relative form — an artist whose albums
  used absolute links (Wet Leg, all four of them) was silently missing
  from the index, with only its singles remaining.
- The demo reel is re-rendered against the current index.

## [v1.3.0] - 2026-11-20

### Added

- `-crossfade`: how long tracks blend into each other on an equal-power
  curve (default `0`, the clean gapless switch). With a crossfade set, a
  track's final seconds are mixed with the incoming track — the outgoing
  fading out on a constant-power curve, the incoming fading in — which the
  engine decodes ahead during those seconds, so the blend never costs a
  fetch gap. A track whose duration the metadata does not know, or whose
  successor is not prepared in time, falls back to the gapless cut.
- The radio now hands over gaplessly: the engine prepares the radio's next
  draw while the current track still plays, so a radio hop (and, with a
  crossfade set, its blend) starts the moment the current track ends.
- The radio's prepared draw avoids the track in the speakers as well as the
  last played one, and the redraw after a track starts previews the draw
  that the pending slot will actually play — a radio that announces a track
  plays it.
- A manual next (`n`) with a crossfade set fades into its successor at once
  on the same equal-power curve: the successor is the track the engine
  already prepared and downloaded, so the skip is instant — no background
  fetch, no hard cut — and the abandoned track fades out over the full
  crossfade window instead of ending where it was. Without a crossfade the
  skip is exactly what it was before (a fresh pick and a background load),
  and so is every skip the engine cannot fade: a paused or idle engine, a
  slot that is not ready, or repeat mode, where "next" would be a restart.
- `←` / `→` on the player screen seek the current track ten seconds back or
  forward, clamped to the track's ends; the position bar moves with the
  move and the audio resumes from it. The library keeps the cursor keys for
  its columns. Seeking works with or without a crossfade set; a seek during
  a crossfade ends the blend first.

### Changed

- The prepared radio draw is consumed when it starts, and the footer redraws
  before the engine's pending slot is refilled, so the preview, the slot and
  the next hop always name the same track.
- A track's encoded audio is downloaded completely (a few seconds at full
  speed) and then played from memory, instead of decoding from the live
  response body. The in-memory buffer is also what makes seeking possible:
  the decoder indexes it for the track's exact length and moves within it,
  and the engine reports the walked length (not the metadata estimate) for
  crossfade timing and the seek clamp.

### Fixed

- Tracks no longer end early. The body was read from the network at playback
  pace, so anything that severed that slowly draining connection — a CDN
  edge, a router, a network blip — silently ended the track wherever it
  died, typically well before its end (reported around 90%). The whole file
  is now fetched before playback starts, so a severed connection is a loud,
  once-retried load error instead of a quiet mid-track cut.
- The demo reel is re-rendered against the v1.3.0 interface: the help
  overlay shows the cursor-key seek, and the tour is unchanged otherwise
  (the tape waits out the index build, pauses, cycles the 3-way visualizer,
  toggles the radio, browses the library, shows the help overlay, and exits
  on the gradient banner).

## [v1.2.1] - 2026-11-20

### Changed

- The demo reel is re-rendered against the v1.2.0 interface: the radio
  starts by itself when the track index lands (the tape's first space
  press is a pause, and the bars hold where the pause found them), the
  visualizer cycle is the 3-way set — spectrum, particles, scope — and
  the tour gains the radio-off line, the new library screen under the
  live playback header, the help overlay over the live screen, and the
  gradient exit banner.

## [v1.2.0] - 2026-11-19

### Added

- The command line shows a one-sentence description and a usage line that
  names the required username: `bc-radio -u username [flags]` (#12).
- When the track index lands with the engine idle and the radio on, the
  radio starts without a keypress; a failed or empty walk keeps the v1.0
  silence rule and the idle hint.
- The help overlay renders over the live screen: the keybinding box centred
  on top, the playback visible around it, instead of a blank background
  (#19).
- Single-track releases are playable. The library lists each artist's
  singles under one `singles` entry (one row, not one per single; enter
  queues them all), each single resolving its stream URL from its own
  `/track/` page, which carries the same release blob as an album page.
  A single whose page is unreadable is named as a gap and the readable
  singles are kept (Alister's call, after the library re-test showed
  followed artists whose only releases are singles, like TONTRAKUL).

### Changed

- The player header's wordmark is the byte-exact design box again (three
  rows had lost their letters' verticals in a transcription), its heavy `═`
  runs now read in the blue-to-peach gradient as letter shapes, the light
  `─` runs stay the quiet frame, and every info row closes on a two-dash
  tail at the far right (#13).
- The footer: the check-out line stays blank until both the band name and
  its Bandcamp home are known (a name without a URL points nowhere), the
  " at " between them reads quiet, and the up-next line is a blank line
  when nothing can be known (#14).
- The key hints read space, d, l, v, f, q left and `? help` at the far
  right (#14).
- The library screen carries the player's own header — it reflects the
  playback and does not move while the user browses — a blank row, a
  "Library (N artists)" title, the three columns, and a selected-row bar
  of one uniform colour instead of reverse video. The minimum size is
  60 × 12 (#15).
- The radial meter leaves the visualizer set: spectrum, particles, scope,
  cycled with `v`.
- The exit banner's wordmark is the header's own drawing — the letters in
  the theme's gradient, the bars quiet — with a blank lead row, instead of
  the monochrome accent box (#16).
- The dependency `github.com/ebitengine/purego` moves from v0.4.1 to
  v0.6.0, the first release whose amd64-darwin call trampoline works.

### Fixed

- **The amd64 (Intel) binary hung on every launch** — `bc-radio -h`, a
  bare start, all of it: the v0.4.1 purego amd64 trampoline that every C
  call (starting with the `Dlopen` of the Objective-C runtime at startup)
  goes through livelocked in a signal loop, so the process printed nothing
  and only died on ctrl-c. This affects every amd64 release since
  v1.1.0; the v0.6.0 pin above is the fix (#17).
- The wordmark's three ragged rows shifted the header's left edge so the
  negative space no longer read as one box; `TestWordmarkRowsAreUniform`
  pins the 48 × 6 box so it cannot return (#13).
- The header's time row closed with a doubled tail — its own two dashes
  plus the block's, leaving four after the percent where the other info
  rows end with two. The bar now takes the space the times and the
  percent leave, a space separates the percent from the tail, and the
  row ends with the one standard tail, like the other info rows (#13
  follow-up).
- The library rendered the row above the selection in bright text, a
  phantom highlight over the bar; every unselected row now reads quiet,
  the bar alone marking the position (#15 follow-up).
- Pausing no longer leaves the particle field drifting: the motes hold
  exactly where the pause found them — the same frozen pause cue as the
  spectrum and scope — and resume from where they held instead of
  jumping.
- The standing status line reads `◉ radio paused` while a loaded track
  is paused, instead of claiming a playing radio over silence; resuming
  restores `◉ radio on`.
- Radio off now means the app stops picking on its own (Alister's
  re-test decision, 2026-11-19): your queue plays, then the player stops
  — the session no longer replays itself behind the off state. Shuffle
  (`s`) is the explicit opt-in for a looping session; repeat (`r`) keeps
  its one-track behaviour. The standing line follows: `○ radio off` while
  a track plays (the up-next row says what comes), `— space plays the
  queue` idle with a queue, and `— l opens the library` idle without one
  (the hint leads so it survives the ellipsis at a narrow width; the on
  and paused lines are unchanged).
- A skip (`n`) whose next comes from the session pool no longer lands
  back on the track just abandoned: the pool is the played tracks minus
  the skip, so a one-track session ends instead of silently restarting
  (follow-up to the off-state semantics).
- Artists with exactly one release no longer show an empty library
  column: Bandcamp answers their `/music` page with a redirect to the
  release itself, and the client recovered the release from the redirected
  page (the page cache now carries the redirect's destination, so the
  recovery holds on cached reads too) (#20).

## [v1.1.3] - 2026-11-18

### Added

- The `-json` and `-list` flags run the index walk and exit without the
  interface or the audio engine: a route for scripts and CI, no terminal
  required. Exit status is non-zero only when the follow list itself
  fails; the walk's best-effort gaps (a removed band or release) go to
  stderr, keeping piped stdout clean (#6).
- An explicit `-theme` is remembered in a one-line file under
  `$XDG_CONFIG_HOME/bc-radio` (or `~/.config/bc-radio/theme`) so the next
  unflagged launch keeps it. The flag always wins; a missing or unreadable
  file falls back to auto, and the export path never writes it. Delete the
  file, or pass `-theme auto`, to reset (#7).
- On a clean quit the player prints its wordmark with a thanks line and
  the repository pointer, after the alternate screen is restored — the
  banner is the last thing the terminal shows. A failed launch goes to its
  error line instead; no wordmark over a fault (#9).
- Pressing x on a track that is not in the queue raises a brief "not in
  queue" transient, so the key never no-ops silently. Removing stays a
  single keystroke — no confirmation (#11).

### Changed

- The status line is styled by the severity recorded where its message is
  raised — info, warning, or alarm — rather than by its words, so a
  near-successful line no longer reads as an alarm. The palette gains a
  muted gold between the quiet gray and the alarm coral (#3).
- Switch confirmations linger three seconds instead of eight, and a
  transient is scoped to the screen it was raised on, so the standing line
  returns within a glance and a player confirmation no longer surfaces in
  the library title row (#4).
- The radial keeps a faint ghost ring at full radius: a constant
  silhouette for quiet passages, in the spectrum peak-cap tradition,
  never mistaken for data (#5).
- The player header is the six-row bc-radio wordmark with the track info
  right-justified beside it — the transport verb and quoted title, the
  band, the album, and the elapsed/total times with a mini bar and
  percent — and the footer is four rows: the status line with a volume
  tail, a link to the band's Bandcamp home, the up-next line, and the key
  hints in the house style. Idle, the title row says what space will do.
  Below 48 columns the header falls back to the single-row title, and the
  minimum terminal size rises from 30 × 8 to 30 × 10 (#8).
- The library's enter hint now reads "enter queue (starts if idle)" —
  queueing from the library starts at once (#10).
- The demo reel in `examples/` re-rendered against the new header and
  footer, on a fresh page cache; it ends on the exit banner.

## [v1.1.2] - 2026-11-16

### Changed

- First launch no longer opens on a blank status line: while nothing is
  playing, the progress line now says what space will do — start the radio,
  or play the queue if one is loaded (#1).
- The library build is no longer a frozen screen: while the index walks the
  cached discographies, the status line runs a spinner and a per-band
  counter (… 7/20 bands), so a cold-cache build of a minute no longer looks
  hung (#2).
- The demo reel in `examples/` re-rendered against the new lines; it also
  waits for the index build to finish before starting the radio.

## [v1.1.1] - 2026-11-16

### Added

- A VHS-rendered demo reel in `examples/` (linked from the README): a launch,
  the radio, all four visualizers, the library, and the help overlay. No code
  changes since v1.1.0 — the binary is the same, stamped `1.1.1`.

## [v1.1.0] - 2026-11-16

### Added

- A radial meter and a Braille oscilloscope of the raw output; the visualizer
  set is now four (spectrum, particles, radial, scope), cycled with `v`.
- Theming: `dark`, `light`, `mono`, and the default `auto` (follows the
  terminal's background); `NO_COLOR` is honoured.
- A mute hint on the status line while the volume is 0.
- The `-version` flag and this changelog.

### Changed

- The analyser is a ring buffer that anchors its reads on what is audible
  right now (everything written, minus the device's unplayed buffer). The
  visual motion tracks the music at a steady cadence instead of rushing to a
  target, freezing, and rushing again — oto refills its 0.5 s buffer in
  bursts, and the old analyser followed the bursts.
- The spectrum's band state steps on a 50 ms quantum; the perceived step
  size, not the frame rate, is what made the bars read as fast. The
  interface still renders at 60 fps.
- Band analysis uses a pure-Go Hann-windowed FFT folded into 16 bands on a
  logarithmic (decibel) scale, replacing the Goertzel analyser.
- The particles are mapped to the bands: one lane per band, so a loud band
  visibly throws more dust than a quiet one.
- The player title row allocates its width across band, title and album and
  clips before styling, so a long title no longer overflows the row.

### Removed

- The waveform visualizer. Its shape was the 16 bands re-expressed in the
  set's thinnest channel, one dot per column, next to the bars that show the
  same data.

## [v1.0.0] - 2026-11-13

The initial release.

### Added

- A local-first terminal radio for the bands you follow on Bandcamp: a flat
  track index built from your follows and their discographies (best-effort,
  cached for 24 hours), random radio (on by default; `d` toggles it), an
  explicit queue with shuffle and repeat, gapless playback over your system
  audio, a three-column library browser, and a bar spectrum driven by a
  16-band analyser of the playing audio.
