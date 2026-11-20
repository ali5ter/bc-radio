package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// TestExitBanner pins the banner's shape: a blank line, the wordmark,
// one-space indented, then the two-line thank-you — eleven rows, each
// within the clip width. One art row is pinned in full: the drawing is
// byte-exact (regenerated from the design file, never retyped), so a
// regression in the transcription must fail here.
func TestExitBanner(t *testing.T) {
	a := &App{width: 80}
	got := strings.Split(stripANSI(a.ExitBanner()), "\n")
	if len(got) != 11 {
		t.Fatalf("banner has %d rows; want 11:\n%s", len(got), strings.Join(got, "\n"))
	}
	// A blank row separates the app from the banner (issue #16).
	if got[0] != "" {
		t.Errorf("banner row 1 = %q; want a blank row above the wordmark", got[0])
	}
	if got[1] != " "+wordmarkR0 {
		t.Errorf("banner art row 1 = %q; want the one-space-indented wordmark", got[1])
	}
	for i, row := range got[1:7] {
		if w, _ := lipgloss.Size(row); w > 80 {
			t.Errorf("banner art row %d is %d cells; must fit the terminal: %q", i+2, w, row)
		}
	}
	want := []string{
		"",
		" Thanks for using bc-radio. Support artists on BandCamp.com",
		" GitHub · ali5ter/bc-radio",
		"",
	}
	for i, w := range want {
		if got[7+i] != w {
			t.Errorf("banner row %d = %q; want %q", 8+i, got[7+i], w)
		}
	}
	// A narrow terminal clips the banner, never widens it.
	a.width = 20
	for i, row := range strings.Split(stripANSI(a.ExitBanner()), "\n") {
		if w, _ := lipgloss.Size(row); w > 40 {
			t.Errorf("narrow banner row %d is %d cells; want the 40-cell floor: %q", i+1, w, row)
		}
	}
}
