package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// ParseKey reads a key name as [Key.String] writes it: "ctrl+s", "alt+x",
// "shift+tab", "f5", "space", "enter", "a", "+".
func ParseKey(name string) (Key, error) {
	var k Key
	rest := name
	for {
		mod, after, found := strings.Cut(rest, "+")
		if !found || after == "" {
			break
		}
		switch mod {
		case "ctrl":
			k.Mod |= ModCtrl
		case "alt":
			k.Mod |= ModAlt
		case "shift":
			k.Mod |= ModShift
		default:
			return Key{}, fmt.Errorf("tui: unknown modifier %q in key %q", mod, name)
		}
		rest = after
	}
	if rest == "space" {
		return Key{Code: KeyRune, Rune: ' ', Mod: k.Mod}, nil
	}
	for code, n := range keyNames {
		if n == rest {
			k.Code = code
			return k, nil
		}
	}
	if len(rest) >= 2 && rest[0] == 'f' {
		var n int
		if _, err := fmt.Sscanf(rest, "f%d", &n); err == nil && n >= 1 && n <= 12 {
			k.Code = KeyF1 + KeyCode(n-1)
			return k, nil
		}
	}
	if r, size := utf8.DecodeRuneInString(rest); size == len(rest) && r != utf8.RuneError {
		k.Code, k.Rune = KeyRune, r
		return k, nil
	}
	return Key{}, fmt.Errorf("tui: unknown key %q", name)
}

// Headless runs an application without a terminal, one step at a time, for
// tests: keys go in, and the screen comes out as text.
//
//	h := tui.NewHeadless(app, 80, 24)
//	h.Type("versand")
//	h.Press("enter")
//	if !strings.Contains(h.Screen(), "m-5705118") { ... }
type Headless struct {
	app           *App
	width, height int
	buf           *buffer
	cursorX       int
	cursorY       int
}

// NewHeadless returns a headless run of app on a screen of width×height
// cells, drawn once.
func NewHeadless(app *App, width, height int) *Headless {
	h := &Headless{app: app, width: width, height: height}
	app.ensureFocus(app.root)
	h.draw()
	return h
}

func (h *Headless) draw() {
	h.app.width, h.app.height = h.width, h.height
	buf, f := h.app.render(h.width, h.height)
	h.buf, h.cursorX, h.cursorY = buf, f.cursorX, f.cursorY
}

// Press presses keys, named as ParseKey reads them, one after the other.
// It panics on a name ParseKey does not know, since that is a mistake in
// the test.
func (h *Headless) Press(keys ...string) {
	for _, name := range keys {
		k, err := ParseKey(name)
		if err != nil {
			panic(err)
		}
		h.app.HandleKey(k)
		h.step()
	}
}

// Type types text, character by character.
func (h *Headless) Type(text string) {
	for _, r := range text {
		h.app.HandleKey(Key{Code: KeyRune, Rune: r})
		h.step()
	}
}

// Paste pastes text in one piece.
func (h *Headless) Paste(text string) {
	h.app.handlePaste(text)
	h.step()
}

// Feed hands raw bytes to the application as the terminal would send them,
// escape sequences included.
func (h *Headless) Feed(data string) {
	h.app.feed([]byte(data), false)
	h.step()
}

// Click clicks the left button at x, y.
func (h *Headless) Click(x, y int) {
	h.app.handleMouse(Mouse{X: x, Y: y, Button: MouseLeft, Action: MousePress})
	h.step()
}

// Wheel turns the wheel at x, y: up for a negative n, down for a positive
// one, |n| steps.
func (h *Headless) Wheel(x, y, n int) {
	b := MouseWheelDown
	if n < 0 {
		b, n = MouseWheelUp, -n
	}
	for range n {
		h.app.handleMouse(Mouse{X: x, Y: y, Button: b})
	}
	h.step()
}

// Resize changes the size of the screen.
func (h *Headless) Resize(width, height int) {
	h.width, h.height = width, height
	h.step()
}

// Settle waits for the work started with App.Go to end, up to timeout,
// and runs what it and App.Post handed to the application.
func (h *Headless) Settle(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		h.app.pending.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
	h.step()
}

// step runs what was posted and draws a frame.
func (h *Headless) step() {
	h.app.drainPanics()
	h.app.runPosted()
	h.app.drainPanics()
	h.draw()
}

// Lines returns the rows of the screen as text, trailing spaces cut.
func (h *Headless) Lines() []string { return h.buf.lines() }

// Screen returns the screen as text, rows joined by "\n", trailing blank
// rows cut.
func (h *Headless) Screen() string {
	return strings.TrimRight(strings.Join(h.Lines(), "\n"), "\n")
}

// Cursor returns where the terminal's cursor is, -1, -1 where it is hidden.
func (h *Headless) Cursor() (x, y int) { return h.cursorX, h.cursorY }

// Quit reports whether the application asked to quit, and with what error.
func (h *Headless) Quit() (bool, error) { return h.app.quit, h.app.err }

// Draw draws a frame, after the application was changed from outside a
// key, such as by pushing a layer.
func (h *Headless) Draw() { h.step() }
