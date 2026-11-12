package tui

import (
	"strings"
	"testing"
	"time"

	"bc-radio/internal/contract"
	"charm.land/lipgloss/v2"
)

func TestFormatDuration(t *testing.T) {
	cases := map[int]string{
		0:    "0:00",
		59:   "0:59",
		60:   "1:00",
		305:  "5:05",
		3599: "59:59",
		-3:   "0:00",
	}
	for in, want := range cases {
		if got := formatDuration(in); got != want {
			t.Errorf("formatDuration(%d) = %q; want %q", in, got, want)
		}
	}
}

func TestClip(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"abcdefgh", 5, "abcd…"},
		{"abcdefgh", 2, "a…"},
		{"abcdefgh", 1, "a"},
		{"abcdefgh", 0, ""},
	}
	for _, tc := range cases {
		if got := clip(tc.in, tc.n); got != tc.want {
			t.Errorf("clip(%q, %d) = %q; want %q", tc.in, tc.n, got, tc.want)
		}
	}
}

func TestPlace(t *testing.T) {
	// Short lines are padded to exactly w.
	if got := place(10, "abc"); got != "abc       " {
		t.Fatalf("place(10, %q) = %q; want padding to 10", got, got)
	}
	// Long lines are truncated (PlaceHorizontal alone would not clip).
	if got := stripANSI(place(10, strings.Repeat("x", 20))); got != strings.Repeat("x", 10) {
		t.Fatalf("place(10, 20x) = %q; want 10 x's", got)
	}
	// Every line of a multi-line block lands at exactly w.
	for i, l := range strings.Split(place(5, "abcd\nefghij"), "\n") {
		if w, _ := lipgloss.Size(l); w != 5 {
			t.Fatalf("line %d of place output is %d cells; want 5: %q", i, w, l)
		}
	}
}

func TestRenderQueue(t *testing.T) {
	a := &App{width: 80, playlist: newPlaylist()}
	if got := stripANSI(a.renderQueue()); !strings.Contains(got, "add tracks") {
		t.Fatalf("empty queue preview = %q; want the add-tracks hint", got)
	}
	a.playlist.Add(contract.Track{ID: "1", Title: "T1", BandName: "B1"})
	a.playlist.Add(contract.Track{ID: "2", Title: "T2", BandName: "B2"})
	a.playlist.Add(contract.Track{ID: "3", Title: "T3", BandName: "B3"})
	a.playlist.Add(contract.Track{ID: "4", Title: "T4", BandName: "B4"})
	got := stripANSI(a.renderQueue())
	for _, want := range []string{"T1", "T2", "T3", "+1 more"} {
		if !strings.Contains(got, want) {
			t.Errorf("queue preview missing %q: %q", want, got)
		}
	}
}

func TestRenderProgress(t *testing.T) {
	a := &App{
		width: 80,
		pos:   75 * time.Second,
		state: contract.State{
			Track: contract.Track{ID: "A", Title: "A", Duration: 300 * time.Second},
		},
	}
	got := stripANSI(a.renderProgress())
	for _, want := range []string{"1:15", "5:00"} {
		if !strings.Contains(got, want) {
			t.Errorf("progress missing %q: %q", want, got)
		}
	}

	// Idle: just the waiting marker.
	a = &App{width: 80}
	if got := stripANSI(a.renderProgress()); !strings.Contains(got, "waiting") {
		t.Errorf("idle progress = %q; want waiting", got)
	}
}
