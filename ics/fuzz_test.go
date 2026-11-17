package ics

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Fold keeps every physical line within 75 octets and valid UTF-8, and
// unfolding gives the line back.
func FuzzFold(f *testing.F) {
	for _, seed := range []string{"", "SUMMARY:short", strings.Repeat("ü", 60), strings.Repeat("a", 75) + "\xff" + strings.Repeat("😀", 30)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, line string) {
		lines := Fold(line)
		for i, l := range lines {
			if len(l) > maxLineOctets {
				t.Fatalf("Fold(%q) line %d has %d octets", line, i, len(l))
			}
			if !utf8.ValidString(l) {
				t.Fatalf("Fold(%q) line %d is not valid UTF-8: %q", line, i, l)
			}
			if i > 0 && !strings.HasPrefix(l, " ") {
				t.Fatalf("Fold(%q) continuation %d does not start with a space: %q", line, i, l)
			}
		}
		unfolded := lines[0]
		for _, l := range lines[1:] {
			unfolded += l[1:]
		}
		if want := strings.ToValidUTF8(line, "�"); unfolded != want {
			t.Fatalf("Fold(%q) unfolds to %q", line, unfolded)
		}
	})
}

// EscapeText leaves nothing that could end the property or the line, and
// nothing a terminal would read as a control.
func FuzzEscapeText(f *testing.F) {
	for _, seed := range []string{"a,b;c\\d", "line\r\nbreak", "\x1b[31mred\u009b2J", "\xff"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := EscapeText(s)
		if !utf8.ValidString(got) {
			t.Fatalf("EscapeText(%q) = %q, not valid UTF-8", s, got)
		}
		for i, r := range got {
			if r != '\t' && (r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0)) {
				t.Fatalf("EscapeText(%q) = %q keeps control %U", s, got, r)
			}
			switch r {
			case ';', ',':
				if i == 0 || got[i-1] != '\\' || escaped(got, i-1) {
					t.Fatalf("EscapeText(%q) = %q leaves %q unescaped", s, got, r)
				}
			}
		}
	})
}

// escaped reports whether the byte at i is itself escaped: an odd number
// of backslashes stands right before it.
func escaped(s string, i int) bool {
	n := 0
	for i--; i >= 0 && s[i] == '\\'; i-- {
		n++
	}
	return n%2 == 1
}
