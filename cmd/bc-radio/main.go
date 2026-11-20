// Command bc-radio runs a local-first terminal radio for the bands you follow
// on Bandcamp. It builds a track index from your follows and their
// discography, plays it as random radio (or whatever else you queue), streams
// the tracks over your system audio, and renders a 60 fps interface with live
// spectrum and particle visualizers, and an oscilloscope.
//
// The interface is interactive: run it in a terminal — the radio starts as
// soon as the track index builds, space pauses, and l queues specific tracks
// or albums. The -json and -list flags export the built track index without
// the interface — for scripts, and as a linear plain-text path for the 60 fps
// redraw. Configuration is flag-only, with one remembered convenience: an
// explicit -theme is written to a small file under $XDG_CONFIG_HOME/bc-radio
// so the next unflagged launch keeps it (the flag still wins). See the README
// for setup, keybindings and the rest.
package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	Execute()
}

// normalizeArgs rewrites the single-dash long flag form (-username) into
// the double-dash one (--username) before cobra sees it. cobra's
// subcommand scan (stripFlags) only recognises --flag and two-letter -f
// as value-taking flags, so an unrewritten -username would leak its value
// into the command lookup ("unknown command \"ali5ter\""). The stdlib flag
// the CLI used to read accepted the single-dash long form, and the
// documented invocation is bc-radio -username <account> — the rewrite
// keeps both working. The input is not mutated (os.Args shares its backing
// array with the slice this hands back).
func normalizeArgs(args []string) []string {
	out := make([]string, len(args))
	copy(out, args)
	for i, a := range out {
		if len(a) > 2 && strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && !strings.Contains(a, "=") {
			out[i] = "-" + a
		}
	}
	return out
}

// Execute runs the CLI: cobra parses the flags and dispatches run, and a
// failure is printed in the binary's house format ("bc-radio: …") and
// exits non-zero. cobra's own error and usage output are silenced — the
// one line above is all a failure prints, the way the old stdlib-flag
// binary did.
func Execute() {
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	rootCmd.SetArgs(normalizeArgs(os.Args[1:]))
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "bc-radio: "+err.Error())
		os.Exit(1)
	}
}
