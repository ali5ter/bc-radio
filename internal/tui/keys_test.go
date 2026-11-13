package tui

import (
	"testing"

	"charm.land/bubbletea/v2"
)

func TestActionFor(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyPressMsg
		want action
	}{
		// Printable keys match on Text; shift on letters is tolerated.
		{"q", tea.KeyPressMsg{Text: "q"}, actQuit},
		{"Q shifted", tea.KeyPressMsg{Text: "Q"}, actQuit},
		{"n", tea.KeyPressMsg{Text: "n"}, actNext},
		{"p", tea.KeyPressMsg{Text: "p"}, actPrev},
		{"s", tea.KeyPressMsg{Text: "s"}, actShuffle},
		{"r", tea.KeyPressMsg{Text: "r"}, actRepeat},
		{"v", tea.KeyPressMsg{Text: "v"}, actVisual},
		{"f", tea.KeyPressMsg{Text: "f"}, actFullscreen},
		{"l", tea.KeyPressMsg{Text: "l"}, actLibrary},
		{"x", tea.KeyPressMsg{Text: "x"}, actRemove},
		{"+ volume up", tea.KeyPressMsg{Text: "+"}, actVolumeUp},
		{"= volume up", tea.KeyPressMsg{Text: "="}, actVolumeUp},
		{"- volume down", tea.KeyPressMsg{Text: "-"}, actVolumeDown},
		{"_ volume down", tea.KeyPressMsg{Text: "_"}, actVolumeDown},
		{"? help", tea.KeyPressMsg{Text: "?"}, actHelp},
		{"unbound letter", tea.KeyPressMsg{Text: "g"}, actNone},
		{"shifted letter", tea.KeyPressMsg{Text: "N", Mod: tea.ModShift}, actNext},
		// ctrl+c is the emergency exit: raw mode means the terminal will
		// never catch it for us, so the app must quit on it.
		{"ctrl-c quits", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, actQuit},
		// Other ctrl/alt/meta combos belong to the terminal, not the app.
		{"ctrl-d", tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}, actNone},
		{"alt-l", tea.KeyPressMsg{Text: "l", Mod: tea.ModAlt}, actNone},
		// Special keys match on String.
		{"space", tea.KeyPressMsg{Code: tea.KeySpace}, actPlayPause},
		{"enter", tea.KeyPressMsg{Code: tea.KeyEnter}, actSelect},
		{"up", tea.KeyPressMsg{Code: tea.KeyUp}, actScrollUp},
		{"down", tea.KeyPressMsg{Code: tea.KeyDown}, actScrollDown},
		{"left", tea.KeyPressMsg{Code: tea.KeyLeft}, actLeft},
		{"right", tea.KeyPressMsg{Code: tea.KeyRight}, actRight},
		{"esc", tea.KeyPressMsg{Code: tea.KeyEscape}, actBack},
		{"unbound special", tea.KeyPressMsg{Code: tea.KeyTab}, actNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := actionFor(tc.msg); got != tc.want {
				t.Fatalf("actionFor(%q) = %v; want %v", tc.msg.String(), got, tc.want)
			}
		})
	}
}
