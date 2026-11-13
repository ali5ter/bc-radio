// Command bc-radio runs a local-first terminal radio for the bands you follow
// on Bandcamp. It builds a track index from your follows and their
// discography, plays it as random radio (or whatever else you queue), streams
// the tracks over your system audio, and renders a 60 fps interface with live
// spectrum, waveform and particle visualizers.
//
// The interface is interactive: run it in a terminal, press space to start
// the radio, and l to queue specific tracks or albums. See the README for
// setup and keybindings.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"golang.org/x/term"

	"bc-radio/internal/audio"
	"bc-radio/internal/bandcamp"
	"bc-radio/internal/tui"
)

// options is the validated command-line configuration.
type options struct {
	username string
	session  string
	identity string
	cacheDir string
	pageTTL  time.Duration
	volume   float64
}

// parseFlags reads the command line into an options, exiting on invalid input
// with an actionable message.
func parseFlags() options {
	o := options{}
	flag.StringVar(&o.username, "username", "", "the Bandcamp account whose followed bands to play")
	flag.StringVar(&o.session, "session", "", "the Bandcamp session cookie (optional; playback works without it)")
	flag.StringVar(&o.identity, "identity", "", "the Bandcamp identity cookie (optional; playback works without it)")
	flag.StringVar(&o.cacheDir, "cache-dir", "", "the page cache directory (defaults to $XDG_CACHE_HOME/bc-radio)")
	flag.DurationVar(&o.pageTTL, "page-ttl", 24*time.Hour, "how long parsed Bandcamp pages stay in the local cache")
	flag.Float64Var(&o.volume, "volume", 1, "the starting volume, from 0 (silent) to 1 (full)")
	flag.Parse()

	if err := o.validate(); err != nil {
		fatal(err)
	}
	return o
}

// validate enforces the constraints the Bandcamp client and the engine rely
// on: a username, the session cookies in pairs, and a volume in range.
func (o options) validate() error {
	if o.username == "" {
		return fmt.Errorf("no --username: bc-radio plays the followed bands of a Bandcamp account")
	}
	if (o.session == "") != (o.identity == "") {
		return fmt.Errorf("the --session and --identity flags must be given together (Bandcamp ignores a request with only one)")
	}
	if o.volume < 0 || o.volume > 1 {
		return fmt.Errorf("the --volume flag must be between 0 and 1 (got %g)", o.volume)
	}
	return nil
}

// newClient builds the Bandcamp scraper. The same client serves both roles:
// it is the library the TUI browses and the source the engine streams from.
func (o options) newClient() (*bandcamp.Client, error) {
	return bandcamp.New(bandcamp.Options{
		Username: o.username,
		Session:  o.session,
		Identity: o.identity,
		CacheDir: o.cacheDir,
		PageTTL:  o.pageTTL,
	})
}

func main() {
	o := parseFlags()

	// The interface is a TUI; degrade with a clear error rather than let the
	// terminal driver fail obscurely off a terminal.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		fatal(fmt.Errorf("bc-radio is a terminal interface and needs an interactive terminal; run it in one"))
	}

	bc, err := o.newClient()
	if err != nil {
		fatal(fmt.Errorf("bandcamp: %w", err))
	}

	engine, err := audio.New(bc)
	if err != nil {
		fatal(fmt.Errorf("audio: %w", err))
	}
	// Release the output device on any exit path. The TUI's own quit stops
	// the engine first; Close is idempotent and then drops the device.
	defer engine.Close()

	if err := engine.SetVolume(context.Background(), o.volume); err != nil {
		fatal(fmt.Errorf("audio: %w", err))
	}

	app := tui.New(bc, engine)
	if err := app.Run(); err != nil {
		fatal(fmt.Errorf("tui: %w", err))
	}
}

// fatal prints an actionable error to stderr and exits non-zero.
func fatal(err error) {
	fmt.Fprintln(os.Stderr, "bc-radio: "+err.Error())
	os.Exit(1)
}
