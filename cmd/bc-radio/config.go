package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ali5ter/bc-radio/internal/tui"
)

// themeFileName is the preference file inside bc-radio's config directory:
// one line holding the last explicit -theme choice.
const themeFileName = "theme"

// themeConfigPath returns the theme preference's path: $XDG_CONFIG_HOME
// (or $HOME/.config when it is unset), under bc-radio.
func themeConfigPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve the config directory: %w (set $XDG_CONFIG_HOME or $HOME)", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "bc-radio", themeFileName), nil
}

// readThemePref reads the remembered theme. The zero value — "", meaning
// "no preference, run with auto" — covers every way the file can be absent
// or untrusted: a missing or unreadable file, and one holding a name the
// -theme flag would not accept. The preference is a convenience, not a
// contract: a broken file falls back rather than failing the launch.
func readThemePref(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	theme := strings.TrimSpace(string(data))
	if err := tui.ValidateTheme(theme); err != nil {
		return ""
	}
	return theme
}

// writeThemePref persists theme as the preference for future launches,
// creating the config directory when needed.
func writeThemePref(path, theme string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create the config directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(theme+"\n"), 0o644); err != nil {
		return fmt.Errorf("write the theme file: %w", err)
	}
	return nil
}

// pickTheme applies the precedence the persistence obeys: an explicit
// -theme beats the remembered value, which beats the auto default. An
// explicit value is validated here; the remembered one was validated when
// read, and the default is a constant.
func pickTheme(explicit bool, flagValue, remembered string) (string, error) {
	if explicit {
		if err := tui.ValidateTheme(flagValue); err != nil {
			return "", err
		}
		return flagValue, nil
	}
	if remembered != "" {
		return remembered, nil
	}
	return "auto", nil
}

// resolveTheme works out the theme this launch runs with: the explicit
// -theme flag, else the remembered preference, else auto. explicit reports
// whether the caller passed -theme, which decides whether the choice is
// remembered for the next launch.
func (o *options) resolveTheme(explicit bool) error {
	o.themeExplicit = explicit

	remembered := ""
	if path, err := themeConfigPath(); err == nil {
		remembered = readThemePref(path)
	}
	theme, err := pickTheme(explicit, o.theme, remembered)
	if err != nil {
		return err
	}
	o.theme = theme
	return nil
}

// rememberTheme writes the preference file after an explicit -theme choice,
// so the next unflagged launch keeps it. A failure is a one-line notice on
// stderr — the choice still applies to this run; persistence never fails a
// launch.
func (o options) rememberTheme() {
	if !o.themeExplicit {
		return
	}
	path, err := themeConfigPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bc-radio: could not remember the theme: %v (pass -theme on each launch)\n", err)
		return
	}
	if err := writeThemePref(path, o.theme); err != nil {
		fmt.Fprintf(os.Stderr, "bc-radio: could not remember the theme: %v (pass -theme on each launch)\n", err)
	}
}
