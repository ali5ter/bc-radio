# Changelog

All notable changes to bc-radio are recorded here. Each release follows
[Semantic Versioning](https://semver.org/).

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
