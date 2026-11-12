// Package tui implements the Bubbletea user interface of bc-radio.
//
// It consumes contract.Library and contract.Player and owns everything the
// frozen contracts leave to the front end:
//
//   - the queue policy — shuffle, repeat, history, and the pool that the
//     radio loops when the queue runs dry (playlist.go);
//   - the two screens — the player (title, visualizer, progress, queue
//     preview, controls) and the three-column library browser
//     (layout.go, library.go);
//   - the v1 visualizers — spectrum, waveform and particles, all driven by
//     the 16-band analyser frame (visualizer.go and the visual_*.go files);
//   - the keybindings (keys.go) and the colour and style palette
//     (styles.go).
//
// The program runs at 60 fps: a 16 ms tick drives the eased band frame, and
// the player's events (track started, ended, failed) are applied one at a
// time on the model thread. Track loads run in goroutines so a stream fetch
// never blocks a render.
package tui
