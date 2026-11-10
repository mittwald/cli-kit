package theme

import "testing"

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
