package tui

// frame is one render of the visualizer region: the app's eased band frame
// (contract.BandCount values in [0,1], low to high frequency, on the
// analyser's log scale — a loud passage reads tall, a quiet one short), the
// most recent post-gain samples (oldest to newest, raw in [-1,1]; empty
// before audio is produced), and the exact cell dimensions the layout has
// allotted.
// Render must return a string of exactly that width and height: the layout
// does no further sizing, so a visualizer that overflows or shrinks a frame
// would tear the screen.
type frame struct {
	bands []float64
	scope []float64
	w     int
	h     int
}

// visualizer draws one frame of the visualizer region.
//
// A frame carries both data paths the contract exposes: the 16-band
// analyser frame (frequency domain) and the time-domain samples. The band-
// driven visuals (spectrum, particles, radial) show what the music is; the
// scope shows what it is doing, sample by sample.
type visualizer interface {
	// Name is the short display name, shown when the user cycles.
	Name() string
	// Render draws one frame into f.w × f.h cells.
	Render(f frame) string
}

// makeVisualizers builds the set in cycle order: v steps through them.
// spectrum and particles hold state (the peak caps, the particle field);
// the rest are pure functions of the frame.
func makeVisualizers() []visualizer {
	return []visualizer{
		newSpectrum(),
		newParticles(),
		newRadial(),
		newScope(),
	}
}
