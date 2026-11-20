// Package tui implements the Bubbletea user interface of bc-radio.
//
// It consumes contract.Library and contract.Player and owns everything the
// frozen contracts leave to the front end:
//
//   - the queue policy — shuffle, repeat, history, and the radio: a random
//     pick from the flat index of every followed band (playlist.go). With
//     the radio off the app stops picking on its own — the queue plays,
//     then silence; shuffle is the explicit opt-in for a looping session;
//   - the two screens — the player (wordmark header, visualizer, status
//     and footer rows, controls) and the three-column library browser
//     (layout.go, library.go);
//   - the three visualizers — the band-driven spectrum and particles, and
//     the sample-driven scope (visualizer.go and the visual_*.go files);
//   - the keybindings (keys.go) and the colour and style palette
//     (styles.go).
//
// The program runs at 60 fps: a 16 ms tick drives the eased band frame, and
// the player's events (track started, ended, failed) are applied one at a
// time on the model thread. Track loads run in goroutines so a stream fetch
// never blocks a render.
package tui
