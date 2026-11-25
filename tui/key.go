package tui

import (
	"strconv"
	"strings"
)

// KeyCode says which key was pressed. A key that types a character is
// [KeyRune], with the character in [Key.Rune].
type KeyCode int

// The keys a terminal reports.
const (
	KeyRune KeyCode = iota
	KeyEnter
	KeyTab
	KeyBackspace
	KeyEscape
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyInsert
	KeyDelete
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
)

// Mod is the set of modifier keys held with a key.
type Mod uint8

// The modifiers a terminal reports. Shift is reported only for keys that
// type no character: a shifted letter arrives as the capital letter.
const (
	ModShift Mod = 1 << iota
	ModAlt
	ModCtrl
)

// Key is one keypress.
type Key struct {
	Code KeyCode
	// Rune is the character typed, for KeyRune: the letter of Ctrl-S is
	// 's' with ModCtrl.
	Rune rune
	Mod  Mod
}

var keyNames = map[KeyCode]string{
	KeyEnter:     "enter",
	KeyTab:       "tab",
	KeyBackspace: "backspace",
	KeyEscape:    "esc",
	KeyUp:        "up",
	KeyDown:      "down",
	KeyLeft:      "left",
	KeyRight:     "right",
	KeyHome:      "home",
	KeyEnd:       "end",
	KeyPageUp:    "pgup",
	KeyPageDown:  "pgdown",
	KeyInsert:    "insert",
	KeyDelete:    "delete",
}

// String names the key the way bindings name it: modifiers first, in the
// order ctrl, alt, shift, joined by "+", then the key: "ctrl+s",
// "shift+tab", "alt+enter", "f5", "space", "a", "A", "?".
func (k Key) String() string {
	var b strings.Builder
	if k.Mod&ModCtrl != 0 {
		b.WriteString("ctrl+")
	}
	if k.Mod&ModAlt != 0 {
		b.WriteString("alt+")
	}
	if k.Mod&ModShift != 0 {
		b.WriteString("shift+")
	}
	switch {
	case k.Code == KeyRune && k.Rune == ' ':
		b.WriteString("space")
	case k.Code == KeyRune:
		b.WriteRune(k.Rune)
	case k.Code >= KeyF1 && k.Code <= KeyF12:
		b.WriteString("f" + strconv.Itoa(int(k.Code-KeyF1)+1))
	default:
		b.WriteString(keyNames[k.Code])
	}
	return b.String()
}

// Printable reports whether the key types its rune into a text field: a
// character without Ctrl or Alt.
func (k Key) Printable() bool {
	return k.Code == KeyRune && k.Mod&(ModCtrl|ModAlt) == 0 && textRune(k.Rune)
}

// keyLabels are the short forms the key legend shows.
var keyLabels = map[string]string{
	"up":        "↑",
	"down":      "↓",
	"left":      "←",
	"right":     "→",
	"enter":     "⏎",
	"pgup":      "pgup",
	"pgdown":    "pgdn",
	"backspace": "⌫",
}

// KeyLabel is the short form of a key name for the legend: "↑" for "up",
// "⏎" for "enter", "^s" for "ctrl+s". Names it does not shorten come back
// as they are.
func KeyLabel(name string) string {
	if label, ok := keyLabels[name]; ok {
		return label
	}
	if rest, ok := strings.CutPrefix(name, "ctrl+"); ok && !strings.Contains(rest, "+") && len([]rune(rest)) == 1 {
		return "^" + rest
	}
	return name
}

// Paste is text pasted into the terminal in one piece, with bracketed paste
// on. Line breaks are "\n"; every other control character but the tab is
// left out.
type Paste struct {
	Text string
	// cut says the paste was too long: Text holds its start, and the rest
	// up to its end is dropped as it arrives.
	cut bool
}

// MouseButton is the button of a mouse event.
type MouseButton int

// The buttons a terminal reports. A wheel turned is a press of WheelUp or
// WheelDown.
const (
	MouseNone MouseButton = iota
	MouseLeft
	MouseMiddle
	MouseRight
	MouseWheelUp
	MouseWheelDown
)

// MouseAction is what happened with the button.
type MouseAction int

// Mouse actions.
const (
	MousePress MouseAction = iota
	MouseRelease
	MouseMotion
)

// Mouse is a mouse event, in cells from the top left corner of the screen,
// 0-based.
type Mouse struct {
	X, Y   int
	Button MouseButton
	Action MouseAction
	Mod    Mod
}

// Resize says the terminal changed size.
type Resize struct {
	Width, Height int
}
