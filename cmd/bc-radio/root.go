// Package main implements the bc-radio CLI: one cobra command whose flags
// select the run mode (the interface, a JSON export, or a list export),
// plus the theme preference the explicit -theme is remembered in.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
	crossfade     time.Duration
	theme         string
	themeExplicit bool // -theme was passed, not left at its default
	jsonOut       bool
	list          bool
}

// The command-line flags, one variable per flag.
var (
	flagUsername  string
	flagSession   string
	flagIdentity  string
	flagCacheDir  string
	flagPageTTL   time.Duration
	flagVolume    float64
	flagCrossfade time.Duration
	flagTheme     string
	flagJSON      bool
	flagList      bool
	// themeFlag is the registered --theme flag: run reads its Changed bit to
	// tell an explicit -theme from the default. Held in a variable (not
	// looked up in rootCmd from run) so the command's RunE and run do not
	// form an initialization cycle.
	themeFlag *pflag.Flag
)

// rootCmd is the bc-radio CLI. A single command, every argument a flag, in
// the house layout the help prints: one sentence, the usage naming the
// required username, then the flags (issue #12; the 2026-11-19 round
// collapses the description to the single sentence and names the username
// in the usage line, "bc-radio -u username [flags]").
var rootCmd = &cobra.Command{
	Use:   "bc-radio -u username",
	Short: "bc-radio plays the bands you follow on Bandcamp as random radio in your terminal.",
	Args:  cobra.NoArgs,
	RunE:  run,
}

// cobra adds the -v, --version flag itself (InitDefaultVersionFlag) when
// Version is set: the version print and exit, which works off a terminal
// and with no other flags — how brew's formula test runs it.
func init() {
	rootCmd.Version = version
	f := rootCmd.Flags()
	f.StringVarP(&flagUsername, "username", "u", "", "the Bandcamp account whose followed bands to play (required)")
	f.StringVar(&flagSession, "session", "", "the Bandcamp session cookie (optional; playback works without it)")
	f.StringVar(&flagIdentity, "identity", "", "the Bandcamp identity cookie (optional; playback works without it)")
	f.StringVarP(&flagCacheDir, "cache-dir", "c", "", "the page cache directory (defaults to $XDG_CACHE_HOME/bc-radio)")
	// The registered default is zero, which the client maps to 24h: a
	// non-zero default would make cobra print the raw "24h0m0s" spelling.
	f.DurationVar(&flagPageTTL, "page-ttl", 0, "how long parsed Bandcamp pages stay in the local cache (default 24h)")
	f.Float64Var(&flagVolume, "volume", 1, "the starting volume, from 0 (silent) to 1 (full)")
	f.DurationVar(&flagCrossfade, "crossfade", 0, "how long tracks crossfade into each other on an equal-power curve (0 = a clean gapless switch, the default)")
	f.StringVarP(&flagTheme, "theme", "t", "auto", "the interface theme: auto (follows the terminal), dark, light or mono; an explicit choice is remembered for the next launch")
	themeFlag = f.Lookup("theme")
	f.BoolVarP(&flagJSON, "json", "j", false, "build the track index, print it as JSON, and exit (no interface, works off a terminal)")
	f.BoolVarP(&flagList, "list", "l", false, "build the track index, print it as a plain-text list, and exit (no interface, works off a terminal)")
	_ = rootCmd.MarkFlagRequired("username")
}

// validate enforces the constraints the Bandcamp client and the engine rely
// on: the session cookies in pairs, and a volume in range. The username is
// required by cobra itself (MarkFlagRequired), and the theme needs no check
// here: resolveTheme already settled it on a validated value.
func (o options) validate() error {
	if (o.session == "") != (o.identity == "") {
		return fmt.Errorf("the --session and --identity flags must be given together (Bandcamp ignores a request with only one)")
	}
	if o.volume < 0 || o.volume > 1 {
		return fmt.Errorf("the --volume flag must be between 0 and 1 (got %g)", o.volume)
	}
	if o.crossfade < 0 {
		return fmt.Errorf("the --crossfade flag must not be negative (got %s)", o.crossfade)
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

// run is the root command's body: settle the theme, validate, then take
// the mode the flags select. The export path runs off a terminal and
// exits; the interface needs one (a clear error, rather than letting the
// terminal driver fail obscurely).
func run(_ *cobra.Command, _ []string) error {
	// Whether the user passed -theme explicitly (not left at its default)
	// decides whether the choice is remembered for the next launch.
	themeExplicit := themeFlag != nil && themeFlag.Changed

	o := options{
		username:  flagUsername,
		session:   flagSession,
		identity:  flagIdentity,
		cacheDir:  flagCacheDir,
		pageTTL:   flagPageTTL,
		volume:    flagVolume,
		crossfade: flagCrossfade,
		theme:     flagTheme,
		jsonOut:   flagJSON,
		list:      flagList,
	}
	if err := o.resolveTheme(themeExplicit); err != nil {
		return err
	}
	if err := o.validate(); err != nil {
		return err
	}

	// The export path runs off a terminal and exits: it needs no audio
	// device and takes the TTY check out of the picture entirely.
	if o.jsonOut || o.list {
		bc, err := o.newClient()
		if err != nil {
			return fmt.Errorf("bandcamp: %w", err)
		}
		return o.export(context.Background(), bc.Tracks, os.Stdout)
	}

	// The interface is a TUI; degrade with a clear error rather than let
	// the terminal driver fail obscurely off a terminal.
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return fmt.Errorf("an interactive terminal is required (run it in one; -json and -list work off one too)")
	}

	// The interface is about to run: remember an explicit -theme so the
	// next unflagged launch keeps it (a notice on failure, never fatal).
	o.rememberTheme()

	bc, err := o.newClient()
	if err != nil {
		return fmt.Errorf("bandcamp: %w", err)
	}

	engine, err := audio.New(bc, audio.WithCrossfade(o.crossfade))
	if err != nil {
		return fmt.Errorf("audio: %w", err)
	}
	// Release the output device on any exit path. The TUI's own quit stops
	// the engine first; Close is idempotent and then drops the device.
	defer engine.Close()

	if err := engine.SetVolume(context.Background(), o.volume); err != nil {
		return fmt.Errorf("audio: %w", err)
	}

	app := tui.New(bc, engine, o.theme)
	if err := app.Run(); err == nil {
		// Clean exit, alternate screen restored: the banner is the last
		// thing the terminal shows. A failed launch goes to its error
		// line instead — no wordmark over a fault.
		fmt.Fprint(os.Stdout, app.ExitBanner())
		return nil
	}
	return fmt.Errorf("tui: %w", err)
}
