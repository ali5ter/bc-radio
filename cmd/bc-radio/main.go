// Command bc-radio runs a local-first terminal radio for the bands you follow
// on Bandcamp. It builds a track index from your follows and their
// discography, plays it as random radio (or whatever else you queue), streams
// the tracks over your system audio, and renders a 60 fps interface with live
// spectrum, particle and radial visualizers, plus an oscilloscope.
//
// The interface is interactive: run it in a terminal, press space to start
// the radio, and l to queue specific tracks or albums. The -json and -list
// flags export the built track index without the interface — for scripts,
// and as a linear plain-text path for the 60 fps redraw. Configuration is
// flag-only, with one remembered convenience: an explicit -theme is written
// to a small file under $XDG_CONFIG_HOME/bc-radio so the next unflagged
// launch keeps it (the flag still wins). See the README for setup,
// keybindings and the rest.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"golang.org/x/term"

	"github.com/ali5ter/bc-radio/internal/audio"
	"github.com/ali5ter/bc-radio/internal/bandcamp"
	"github.com/ali5ter/bc-radio/internal/tui"
)

// version is set at release time by goreleaser's ldflags; development
// builds report "dev".
var version = "dev"

// options is the validated command-line configuration.
type options struct {
	username      string
	session       string
	identity      string
	cacheDir      string
	pageTTL       time.Duration
	volume        float64
	theme         string
	themeExplicit bool // -theme was passed, not left at its default
	jsonOut       bool
	list          bool
}

// showVersion is true when the -version flag was given: the binary prints
// its version and exits before anything else (it must work off a terminal,
// which is how brew's formula test invokes it).
var showVersion bool

// parseFlags reads the command line into an options, exiting on invalid input
// with an actionable message.
func parseFlags() options {
	o := options{}
	flag.BoolVar(&showVersion, "version", false, "print the version and exit")
	flag.StringVar(&o.username, "username", "", "the Bandcamp account whose followed bands to play")
	flag.StringVar(&o.session, "session", "", "the Bandcamp session cookie (optional; playback works without it)")
	flag.StringVar(&o.identity, "identity", "", "the Bandcamp identity cookie (optional; playback works without it)")
	flag.StringVar(&o.cacheDir, "cache-dir", "", "the page cache directory (defaults to $XDG_CACHE_HOME/bc-radio)")
	flag.DurationVar(&o.pageTTL, "page-ttl", 24*time.Hour, "how long parsed Bandcamp pages stay in the local cache")
	flag.Float64Var(&o.volume, "volume", 1, "the starting volume, from 0 (silent) to 1 (full)")
	flag.StringVar(&o.theme, "theme", "auto", "the interface theme: auto (follows the terminal), dark, light or mono; an explicit choice is remembered for the next launch")
	flag.BoolVar(&o.jsonOut, "json", false, "build the track index, print it as JSON, and exit (no interface, works off a terminal)")
	flag.BoolVar(&o.list, "list", false, "build the track index, print it as a plain-text list, and exit (no interface, works off a terminal)")
	flag.Parse()

	// -version works with no other flags and off a terminal (brew's formula
	// test runs it through system); exit before any validation or setup.
	if showVersion {
		fmt.Println("bc-radio " + version)
		os.Exit(0)
	}

	if err := o.resolveTheme(); err != nil {
		fatal(err)
	}
	if err := o.validate(); err != nil {
		fatal(err)
	}
	return o
}

// validate enforces the constraints the Bandcamp client and the engine rely
// on: a username, the session cookies in pairs, and a volume in range. The
// theme needs no check here: resolveTheme already settled it on a validated
// value.
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
	if o.jsonOut && o.list {
		return fmt.Errorf("use only one of -json and -list")
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

	// The export path runs off a terminal and exits: it needs no audio
	// device and takes the TTY check out of the picture entirely.
	if o.jsonOut || o.list {
		bc, err := o.newClient()
		if err != nil {
			fatal(err)
		}
		if err := o.export(context.Background(), bc.Tracks, os.Stdout); err != nil {
			fatal(err)
		}
		return
	}

	// The interface is a TUI; degrade with a clear error rather than let the
	// terminal driver fail obscurely off a terminal.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		fatal(fmt.Errorf("an interactive terminal is required (run it in one; -json and -list work off one too)"))
	}

	// The interface is about to run: remember an explicit -theme so the next
	// unflagged launch keeps it (a notice on failure, never fatal).
	o.rememberTheme()

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

	app := tui.New(bc, engine, o.theme)
	if err := app.Run(); err != nil {
		fatal(fmt.Errorf("tui: %w", err))
	}
}

// fatal prints an actionable error to stderr and exits non-zero.
func fatal(err error) {
	fmt.Fprintln(os.Stderr, "bc-radio: "+err.Error())
	os.Exit(1)
}
