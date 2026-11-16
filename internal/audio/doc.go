// Package audio implements the playback engine of bc-radio: it decodes
// Bandcamp MP3 streams, plays them to the audio device, and exposes
// contract.Player to the TUI.
//
// Architecture:
//
//   - the output device is an oto/v2 player — the module's one cgo
//     dependency (CoreAudio on macOS, PulseAudio or ALSA on Linux, WASAPI
//     on Windows) — which pulls from the pump in its own goroutine; the
//     pull is the clock, and device backpressure paces decoding at real
//     time;
//   - the pump decodes the engine's current chain — MP3 via beep/mp3 (pure
//     Go), a resampler only when the source rate differs from the device
//     rate, then the shared digital gain — and feeds everything it emits,
//     silent tail included, to the analyser, whose reads anchor on what is
//     audible now, so the bands always reflect what is heard;
//   - a track switch happens inside the pump, which is what makes
//     transitions gapless: the next track's chain is prepared in the
//     background (stream fetched, decoder built) while the current track
//     still plays, and the pump picks it up the moment the current one
//     drains, in the same buffer fill.
//
// The device runs at 44.1 kHz, Bandcamp's stream rate, so typical sources
// need no resampling.
//
// Replacing a track (Load) flushes the device: the oto player keeps up to
// half a second of already-pulled audio in its internal buffer, and the
// only way to clear it for a non-seekable source is a reset. A replaced
// track is therefore never heard ahead of its replacement.
//
// Note for the TUI: while the engine is paused the device still pulls until
// its buffer is full, so the decoder — and State().Position — advance by
// at most the buffer's worth; the UI should freeze its position display
// while not playing.
package audio
