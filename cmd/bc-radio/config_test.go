package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The precedence: an explicit -theme beats the remembered value, which
// beats the auto default; an invalid explicit value is rejected.
func TestPickThemePrecedence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		explicit   bool
		flag       string
		remembered string
		want       string
		wantErr    bool
	}{
		{"explicit beats remembered", true, "mono", "dark", "mono", false},
		{"explicit auto beats remembered mono", true, "auto", "mono", "auto", false},
		{"remembered when no flag", false, "auto", "mono", "mono", false},
		{"default auto when neither", false, "auto", "", "auto", false},
		{"invalid explicit is rejected", true, "neon", "dark", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pickTheme(tc.explicit, tc.flag, tc.remembered)
			if (err != nil) != tc.wantErr {
				t.Fatalf("pickTheme = %q, err = %v; wantErr = %v", got, err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("pickTheme = %q; want %q", got, tc.want)
			}
		})
	}
}

// The preference file: a readable name (with surrounding whitespace) is the
// preference; absent, empty or untrusted content is "no preference".
func TestReadThemePref(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "theme")

	if got := readThemePref(path); got != "" {
		t.Errorf("absent file reads %q; want no preference", got)
	}

	for _, tc := range []struct {
		content string
		want    string
	}{
		{"mono\n", "mono"},
		{"  light\n", "light"}, // surrounding whitespace is ignored
		{"dark", "dark"},       // no trailing newline is fine
		{"neon\n", ""},         // a name the flag would reject
		{"", ""},               // empty file
		{"\n\n", ""},           // whitespace only
	} {
		if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := readThemePref(path); got != tc.want {
			t.Errorf("readThemePref(%q) = %q; want %q", tc.content, got, tc.want)
		}
	}
}

// The path honours $XDG_CONFIG_HOME and falls back to $HOME/.config.
func TestThemeConfigPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	got, err := themeConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "bc-radio", "theme"); got != want {
		t.Errorf("path = %q; want $XDG_CONFIG_HOME/bc-radio/theme", got)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", filepath.Join(t.TempDir(), "home"))
	got, err = themeConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(os.Getenv("HOME"), ".config", "bc-radio", "theme"); got != want {
		t.Errorf("path = %q; want $HOME/.config/bc-radio/theme", got)
	}
}

// rememberTheme writes the one-line file for an explicit choice and leaves
// the filesystem untouched for an unflagged launch.
func TestRememberTheme(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "bc-radio", "theme")

	o := options{username: "x", theme: "mono", themeExplicit: true}
	o.rememberTheme()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the preference file was not written: %v", err)
	}
	if string(data) != "mono\n" {
		t.Errorf("file content = %q; want \"mono\\n\"", data)
	}
	if got := readThemePref(path); got != "mono" {
		t.Errorf("write/read round trip = %q; want mono", got)
	}

	// Unflagged launch: no write, no file.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	implicit := options{username: "x", theme: "mono", themeExplicit: false}
	implicit.rememberTheme()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("an unflagged launch created the preference file")
	}
}
