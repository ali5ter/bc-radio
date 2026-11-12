package tui

// visualizer draws one frame of the visualizer region from the current
// band frame. Render receives the app's eased band values (already
// contract.BandCount, in [0,1], low to high frequency) and the exact cell
// dimensions the layout has allotted, and must return a string of exactly
// that width and height: the layout does no further sizing, so a visualizer
// that overflows or shrinks a frame would tear the screen.
//
// v1 visuals are all driven by the 16-band analyser frame — the frozen
// contract exposes bands, not PCM. A sample-accurate waveform and a
// 64-band spectrum would need a contract change (or Phase 2).
type visualizer interface {
	// Name is the short display name, shown when the user cycles.
	Name() string
	// Render draws one frame into w × h cells.
	Render(bands []float64, w, h int) string
}

// makeVisualizers builds the v1 set in cycle order: v steps through them.
// particles is the only stateful one; the rest are pure functions of the
// band frame.
func makeVisualizers() []visualizer {
	return []visualizer{
		newSpectrum(),
		newWaveform(),
		newParticles(),
	}
}
