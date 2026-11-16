package audio

import (
	"io"

	"github.com/hajimehoshi/oto/v2"
)

// deviceRate is the output sample rate: 44.1 kHz, Bandcamp's stream rate,
// so typical sources need no resampling.
const deviceRate = 44100

// device is the audio output the engine feeds. Implementations pull frames
// from the engine's pump on their own schedule — the pump does no pacing of
// its own; device backpressure is the clock. The production implementation
// is oto/v2 (the module's one cgo dependency); tests inject a fake.
//
// The oto player is a pull device with an internal buffer (half a second
// by default) that it keeps filling even while paused, and the only way to
// clear buffered audio for a non-seekable source is a reset — so replacing
// a track (Load) flushes the device rather than merely resuming it, or up
// to half a second of the old track would play ahead of the new one.
type device interface {
	// play starts or resumes the pull loop. Safe to call repeatedly.
	play()
	// pause stops the pull loop. The device keeps whatever audio it has
	// already pulled in its buffer, which is why a replaced track needs
	// flush, not resume.
	pause()
	// flush discards any audio already pulled into the device's buffer and
	// starts the pull loop. After a flush the next frames the device pulls
	// come from the newly installed chain, so nothing of the previous
	// track is heard.
	flush()
	// unplayed reports the frames the device has already pulled but not yet
	// played, at the device's sample rate. The analyser subtracts it from
	// what it has been fed to anchor its reads on what is audible right
	// now.
	unplayed() int
	// close releases the output device.
	close()
}

// otoDevice is the production device: an oto/v2 player (CoreAudio on
// macOS, PulseAudio/ALSA on Linux, WASAPI on Windows) reading the pump.
type otoDevice struct {
	player oto.Player
}

// newOtoDevice opens the default output device at deviceRate, stereo
// float32, wired to pump. The player is created paused; the engine calls
// play to start it.
func newOtoDevice(pump io.Reader) (device, error) {
	ctx, ready, err := oto.NewContext(deviceRate, 2, oto.FormatFloat32LE)
	if err != nil {
		return nil, err
	}
	<-ready
	return &otoDevice{player: ctx.NewPlayer(pump)}, nil
}

func (d *otoDevice) play()  { d.player.Play() }
func (d *otoDevice) pause() { d.player.Pause() }

// unplayed converts oto's byte count of the internal buffer to frames:
// the device is stereo float32, bytesPerFrame bytes per frame, so the
// division is exact. After Close the buffer is gone and oto reports 0.
func (d *otoDevice) unplayed() int {
	return d.player.UnplayedBufferSize() / bytesPerFrame
}

// flush is the one reset-and-resume: oto/v2's Reset empties the player's
// internal buffer (setting it paused) and Play restarts the pull.
func (d *otoDevice) flush() {
	d.player.Reset()
	d.player.Play()
}

func (d *otoDevice) close() { _ = d.player.Close() }
