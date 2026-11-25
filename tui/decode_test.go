package tui

import (
	"testing"
	"unicode/utf8"
)

func TestDecodeKeys(t *testing.T) {
	tests := []struct {
		in   string
		want string
		n    int
	}{
		{"a", "a", 1},
		{"A", "A", 1},
		{" ", "space", 1},
		{"\r", "enter", 1},
		{"\r\n", "enter", 2},
		{"\t", "tab", 1},
		{"\x7f", "backspace", 1},
		{"\x13", "ctrl+s", 1},
		{"\x01", "ctrl+a", 1},
		{"\x00", "ctrl+space", 1},
		{"\x1f", "ctrl+_", 1},
		{"\x1b[A", "up", 3},
		{"\x1bOB", "down", 3},
		{"\x1b[1;5C", "ctrl+right", 6},
		{"\x1b[1;3D", "alt+left", 6},
		{"\x1b[1;2A", "shift+up", 6},
		{"\x1b[Z", "shift+tab", 3},
		{"\x1b[3~", "delete", 4},
		{"\x1b[5;5~", "ctrl+pgup", 6},
		{"\x1b[15~", "f5", 5},
		{"\x1bOP", "f1", 3},
		{"\x1b[[B", "f2", 4},
		{"\x1bx", "alt+x", 2},
		{"\x1b\x1b[A", "alt+up", 4},
		{"\x1b\r", "alt+enter", 2},
		{"\x1b[115;5u", "ctrl+s", 8},
		{"\x1b[97;2u", "A", 7},
		{"\x1b[122;6u", "ctrl+shift+z", 8},
		{"\x1b[13;2u", "shift+enter", 7},
		{"ä", "ä", 2},
	}
	for _, tt := range tests {
		ev, n := decode([]byte(tt.in), true)
		k, ok := ev.(Key)
		if !ok || k.String() != tt.want || n != tt.n {
			t.Errorf("decode(%q) = %v, %d; want %s, %d", tt.in, ev, n, tt.want, tt.n)
		}
	}
}

func TestDecodeWaitsForTheRestOfASequence(t *testing.T) {
	for _, in := range []string{"\x1b", "\x1b[", "\x1b[1;5", "\xc3", "\x1b[200~half a paste"} {
		if _, n := decode([]byte(in), true); n != 0 {
			t.Errorf("decode(%q, more) consumed %d, want 0", in, n)
		}
	}
	ev, n := decode([]byte("\x1b"), false)
	if k, ok := ev.(Key); !ok || k.Code != KeyEscape || n != 1 {
		t.Errorf("a lone ESC = %v, %d; want esc", ev, n)
	}
}

func TestDecodePaste(t *testing.T) {
	in := "\x1b[200~line one\r\nline\x1b two\ttab\u202e\x1b[201~x"
	ev, n := decode([]byte(in), true)
	p, ok := ev.(Paste)
	if !ok || p.Text != "line one\nline two\ttab" || n != len(in)-1 {
		t.Errorf("paste = %#v, %d", ev, n)
	}
}

func TestDecodeMouse(t *testing.T) {
	tests := []struct {
		in   string
		want Mouse
	}{
		{"\x1b[<0;10;5M", Mouse{X: 9, Y: 4, Button: MouseLeft}},
		{"\x1b[<0;10;5m", Mouse{X: 9, Y: 4, Button: MouseLeft, Action: MouseRelease}},
		{"\x1b[<64;1;1M", Mouse{Button: MouseWheelUp}},
		{"\x1b[<65;1;1M", Mouse{Button: MouseWheelDown}},
		{"\x1b[<18;3;4M", Mouse{X: 2, Y: 3, Button: MouseRight, Mod: ModCtrl}},
		{"\x1b[<32;3;4M", Mouse{X: 2, Y: 3, Button: MouseLeft, Action: MouseMotion}},
	}
	for _, tt := range tests {
		ev, n := decode([]byte(tt.in), true)
		if ev != tt.want || n != len(tt.in) {
			t.Errorf("decode(%q) = %#v, %d; want %#v", tt.in, ev, n, tt.want)
		}
	}
}

func TestParseKeyRoundTrips(t *testing.T) {
	for _, name := range []string{"a", "A", "?", "+", "space", "enter", "esc", "ctrl+s", "ctrl+alt+x", "shift+tab", "alt+enter", "f12", "pgdown", "ctrl+space"} {
		k, err := ParseKey(name)
		if err != nil {
			t.Fatalf("ParseKey(%q): %v", name, err)
		}
		if k.String() != name {
			t.Errorf("ParseKey(%q).String() = %q", name, k.String())
		}
	}
	if _, err := ParseKey("hyper+x"); err == nil {
		t.Error("ParseKey took an unknown modifier")
	}
}

func FuzzDecode(f *testing.F) {
	for _, seed := range []string{"a", "\x1b[1;5A", "\x1b[200~x\x1b[201~", "\x1b[<0;1;1M", "\x1b\x1b\x1b", "\x1b[999999999999999999999~", "\xff\xfe", "\x1b[<1;;M"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		for len(data) > 0 {
			ev, n := decode(data, false)
			if n <= 0 || n > len(data) {
				t.Fatalf("decode(%q) consumed %d", data, n)
			}
			switch ev := ev.(type) {
			case Key:
				if ev.Code == KeyRune && !utf8.ValidRune(ev.Rune) {
					t.Fatalf("invalid rune %U", ev.Rune)
				}
			case Paste:
				for _, r := range ev.Text {
					if r < 0x20 && r != '\n' && r != '\t' {
						t.Fatalf("paste holds control %U", r)
					}
				}
			case Mouse:
				if ev.X < 0 || ev.Y < 0 {
					t.Fatalf("mouse at %d,%d", ev.X, ev.Y)
				}
			}
			data = data[n:]
		}
	})
}
