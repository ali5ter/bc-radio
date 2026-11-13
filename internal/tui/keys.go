package tui

import (
	"strings"

	"charm.land/bubbletea/v2"
)

// action is what a keypress asks the app to do. Keeping the mapping in one
// place means Update never inspects raw keys and tests can drive the app
// with plain actions.
type action int

// The bound keys, per the PRD keyboard table plus the library and
// fullscreen conveniences.
const (
	actNone action = iota
	actPlayPause
	actNext
	actPrev
	actShuffle
	actRepeat
	actVisual
	actFullscreen
	actVolumeUp
	actVolumeDown
	actScrollUp
	actScrollDown
	actLeft
	actRight
	actSelect
	actRemove
	actLibrary
	actBack
	actHelp
	actQuit
)

// actionFor maps a key press to an action, or actNone for unbound keys.
//
// Only unmodified keys are bound: ctrl and alt combos belong to the
// terminal and the multiplexer, and shift is tolerated for letters (it
// only affects the case). The one exception is ctrl+c: the program runs
// the terminal in raw mode, so the terminal will never interpret it for
// us, and the universal emergency exit has to be handled here or the app
// cannot be force-quit. Text is the typed character for printable keys
// and empty for special keys, which is why printable keys match on Text
// and special keys on String.
func actionFor(msg tea.KeyPressMsg) action {
	if msg.String() == "ctrl+c" {
		return actQuit
	}
	if msg.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta) != 0 {
		return actNone
	}
	if msg.Text != "" {
		switch strings.ToLower(msg.Text) {
		case "q":
			return actQuit
		case "n":
			return actNext
		case "p":
			return actPrev
		case "s":
			return actShuffle
		case "r":
			return actRepeat
		case "v":
			return actVisual
		case "f":
			return actFullscreen
		case "l":
			return actLibrary
		case "x":
			return actRemove
		case "+", "=":
			return actVolumeUp
		case "-", "_":
			return actVolumeDown
		case "?":
			return actHelp
		}
		return actNone
	}
	switch msg.String() {
	case "space":
		return actPlayPause
	case "enter":
		return actSelect
	case "up":
		return actScrollUp
	case "down":
		return actScrollDown
	case "left":
		return actLeft
	case "right":
		return actRight
	case "esc":
		return actBack
	}
	return actNone
}
