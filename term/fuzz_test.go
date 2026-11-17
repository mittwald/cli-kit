package term

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Truncate and TruncateMiddle never leave a line wider than asked for.
func FuzzTruncate(f *testing.F) {
	for _, seed := range []string{"feature/very-long-branch-name", "\x1b[31m日本語のテキスト\x1b[0m", "a\tb\tc", "e\u0301e\u0301e\u0301", "line one\nline two is longer", "\x1b]8;;https://x\x1b\\link\x1b]8;;\x1b\\"} {
		f.Add(seed, 5)
	}
	f.Fuzz(func(t *testing.T, s string, width int) {
		if width < 1 || width > 200 {
			return
		}
		s = Sanitize(s) // the functions measure text a terminal may be shown
		for name, cut := range map[string]func(string, int) string{"Truncate": Truncate, "TruncateMiddle": TruncateMiddle} {
			got := cut(s, width)
			for line := range strings.SplitSeq(got, "\n") {
				if w := VisibleWidth(line); w > max(width, cells(Ellipsis)) {
					t.Fatalf("%s(%q, %d) = %q, a line %d cells wide", name, s, width, got, w)
				}
			}
		}
	})
}

// PlainLine leaves one line of text: no escape sequence, no control, no
// line break, no bidirectional control and no white space at either end.
func FuzzPlainLine(f *testing.F) {
	for _, seed := range []string{"invoice\u202efdp.exe", "a\r\nb\u2028c", "\x1b[31mred\x1b]0;t\x07", " \u00a0x\t", "\u009b2J"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := PlainLine(s)
		if !utf8.ValidString(got) || strings.TrimSpace(got) != got {
			t.Fatalf("PlainLine(%q) = %q", s, got)
		}
		for _, r := range got {
			if control(r) || lineBreak(r) || bidiControl(r) || r == '\t' {
				t.Fatalf("PlainLine(%q) = %q keeps %U", s, got, r)
			}
		}
	})
}
