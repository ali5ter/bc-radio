package tui

// frame is one render of the visualizer region: the app's eased band frame
// (contract.BandCount values in [0,1], low to high frequency, on the
// analyser's log scale — a loud passage reads tall, a quiet one short), the
// most recent post-gain samples (oldest to newest, raw in [-1,1]; empty
// before audio is produced), the pause state, and the exact cell dimensions
// the layout has allotted.
//
// paused is set when a track is loaded but not playing. The spectrum and
// the scope need no special handling — the analyser's anchor pins while
// the device's buffer fills, so their inputs freeze and the visuals hold
// their last frame. A stateful visualizer must act on the flag itself:
// the particle field and the interference clock would otherwise keep
// simulating through the silence.
//
// Render must return a string of exactly that width and height: the layout
// does no further sizing, so a visualizer that overflows or shrinks a frame
// would tear the screen.
type frame struct {
	bands  []float64
	scope  []float64
	paused bool
	w      int
	h      int
}

// visualizer draws one frame of the visualizer region.
//
// A frame carries both data paths the contract exposes: the 16-band
// analyser frame (frequency domain) and the time-domain samples. The band-
// driven visuals (spectrum, particles) show what the music is; the scope
// shows what it is doing, sample by sample.
type visualizer interface {
	// Name is the short display name, shown when the user cycles.
	Name() string
	// Render draws one frame into f.w × f.h cells.
	Render(f frame) string
}

// makeVisualizers builds the set in cycle order: v steps through them.
// spectrum, particles and interference hold state (the peak caps, the
// particle field, the phase clock); the scope is a pure function of the
// frame. The radial meter left the set in the 2026-11-19 polish round: it
// read too quiet next to the spectrum to earn its slot in the cycle.
func makeVisualizers() []visualizer {
	return []visualizer{
		newSpectrum(),
		newParticles(),
		newScope(),
		newInterference(),
	}
}
