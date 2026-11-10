package term

import (
	"bytes"
	"testing"
)

func TestParseColorMode(t *testing.T) {
	tests := map[string]ColorMode{
		"auto": ColorAuto, "tty": ColorAuto, "IF-TTY": ColorAuto,
		"always": ColorAlways, "": ColorAlways, "true": ColorAlways, "force": ColorAlways,
		"never": ColorNever, "false": ColorNever, "none": ColorNever, " Never ": ColorNever,
	}
	for in, want := range tests {
		got, err := ParseColorMode(in)
		if err != nil || got != want {
			t.Errorf("ParseColorMode(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseColorMode("rainbow"); err == nil {
		t.Error("ParseColorMode(rainbow) succeeded")
	}
}

func TestColorModeStringRoundTrip(t *testing.T) {
	for _, name := range ColorModeNames {
		m, err := ParseColorMode(name)
		if err != nil {
			t.Fatal(err)
		}
		if m.String() != name {
			t.Errorf("%q round-trips to %q", name, m.String())
		}
	}
}

func TestShouldColor(t *testing.T) {
	var buf bytes.Buffer
	t.Setenv("TERM", "xterm-256color")
	if ShouldColor(ColorAuto, &buf) {
		t.Error("auto colors a buffer")
	}
	if !ShouldColor(ColorAlways, &buf) {
		t.Error("always does not color a buffer")
	}
	if ShouldColor(ColorNever, &buf) {
		t.Error("never colors")
	}
}

func TestColorAllowed(t *testing.T) {
	t.Setenv("TERM", "xterm")
	t.Setenv("NO_COLOR", "")
	if ColorAllowed() {
		t.Error("an empty NO_COLOR still allows color")
	}
}

func TestColorAllowedDumbTerminal(t *testing.T) {
	t.Setenv("TERM", "dumb")
	if ColorAllowed() {
		t.Error("TERM=dumb allows color")
	}
}

func TestSetColorMode(t *testing.T) {
	t.Cleanup(func() { SetColorMode(ColorAuto) })
	var buf bytes.Buffer
	SetColorMode(ColorAlways)
	if CurrentColorMode() != ColorAlways || !Colored(&buf) {
		t.Error("ColorAlways not applied")
	}
	SetColorMode(ColorNever)
	if Colored(&buf) {
		t.Error("ColorNever not applied")
	}
	if CanAnimate(&buf) {
		t.Error("a buffer can be animated")
	}
}

func TestSetColorModeRejectsUnknownModes(t *testing.T) {
	t.Cleanup(func() { SetColorMode(ColorAuto) })
	SetColorMode(ColorMode(42))
	if got := CurrentColorMode(); got != ColorAuto {
		t.Errorf("unknown mode recorded as %v, want auto", got)
	}
}
