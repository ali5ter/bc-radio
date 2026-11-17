# Changelog

All notable changes to bc-radio are recorded here. Each release follows
[Semantic Versioning](https://semver.org/).

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
