package theme

import (
	"bytes"
	"testing"

	"github.com/mittwald/cli-kit/term"
)

func TestPaint(t *testing.T) {
	if got := Bold.Paint("x"); got != "\x1b[1mx\x1b[0m" {
		t.Errorf("Paint = %q", got)
	}
	if got := Style("").Paint("x"); got != "x" {
		t.Errorf("empty style painted: %q", got)
	}
	if got := Bold.Paint(""); got != "" {
		t.Errorf("empty text painted: %q", got)
	}
	if got := Bold.PaintIf(false, "x"); got != "x" {
		t.Errorf("PaintIf(false) = %q", got)
	}
	if got := Fg256(1).PaintLines("a\n\nb"); got != "\x1b[38;5;1ma\x1b[0m\n\n\x1b[38;5;1mb\x1b[0m" {
		t.Errorf("PaintLines = %q", got)
	}
}

func TestBuilders(t *testing.T) {
	if got := Join(Bold, "", Fg256(141)); got != "1;38;5;141" {
		t.Errorf("Join = %q", got)
	}
	if got := RGB(1, 2, 3); got != "38;2;1;2;3" {
		t.Errorf("RGB = %q", got)
	}
	if got := BgRGB(1, 2, 3); got != "48;2;1;2;3" {
		t.Errorf("BgRGB = %q", got)
	}
	if got := Bg256(7); got != "48;5;7" {
		t.Errorf("Bg256 = %q", got)
	}
	if got := Bold.Sequence(); got != "\x1b[1m" {
		t.Errorf("Sequence = %q", got)
	}
}

func TestUse(t *testing.T) {
	t.Cleanup(func() { Use(Default) })
	if Active() != Default {
		t.Fatal("Active is not Default before Use")
	}
	Use(Plain)
	if Active() != Plain {
		t.Error("Use(Plain) not active")
	}
}

// For keeps a style only for a writer that takes color.
func TestStyleFor(t *testing.T) {
	was := term.CurrentColorMode()
	t.Cleanup(func() { term.SetColorMode(was) })
	var out bytes.Buffer

	term.SetColorMode(term.ColorAuto)
	if got := Bold.For(&out); got != "" {
		t.Errorf("For(a buffer) = %q, want the zero Style: a buffer is no terminal", got)
	}
	if got := Bold.For(&out).PaintLines("a\nb"); got != "a\nb" {
		t.Errorf("painted for a buffer = %q, want the text unchanged", got)
	}
	term.SetColorMode(term.ColorAlways)
	if got := Bold.For(&out); got != Bold {
		t.Errorf("For under --color=always = %q, want %q", got, Bold)
	}
	term.SetColorMode(term.ColorNever)
	if got := Bold.For(&out); got != "" {
		t.Errorf("For under --color=never = %q, want the zero Style", got)
	}
}

// Status lines keep the grey they had before the role of their own.
func TestDefaultStatusIsMuted(t *testing.T) {
	if Default.Status == "" || Default.Status != Default.Muted {
		t.Errorf("Default.Status = %q, want Muted's %q", Default.Status, Default.Muted)
	}
}
