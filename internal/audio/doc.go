// Package audio implements the playback pipeline: MP3 decoding via
// beep/mp3 (44.1 kHz stereo, spike-verified), output to the audio device,
// the gapless queue, volume, and the 16-band Goertzel analyser. It
// implements contract.Player and consumes contract.AudioSource.
package audio
