package term

import (
	"strings"
	"testing"
)

func TestStripANSI(t *testing.T) {
	tests := map[string]struct{ in, want string }{
		"plain":              {"hello", "hello"},
		"sgr":                {"\x1b[1;31mred\x1b[0m", "red"},
		"cursor movement":    {"\x1b[2K\x1b[1Aline", "line"},
		"osc 8 with st":      {"\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\", "link"},
		"osc with bel":       {"\x1b]0;title\x07text", "text"},
		"two-byte escape":    {"\x1b7saved\x1b8", "saved"},
		"multi-byte runes":   {"\x1b[32mgrün ✓\x1b[0m", "grün ✓"},
		"unterminated csi":   {"abc\x1b[31", "abc"},
		"lone escape at end": {"abc\x1b", "abc"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := StripANSI(tt.in); got != tt.want {
				t.Errorf("StripANSI(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestVisibleWidth(t *testing.T) {
	tests := map[string]struct {
		in   string
		want int
	}{
		"ascii":        {"hello", 5},
		"styled":       {"\x1b[1mbold\x1b[0m", 4},
		"wide runes":   {"日本", 4},
		"emoji":        {"✅ ok", 5},
		"combining":    {"e\u0301", 1},
		"widest line":  {"ab\nabcd\nabc", 4},
		"empty":        {"", 0},
		"link wrapped": {Hyperlink("https://example.com", "site"), 4},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := VisibleWidth(tt.in); got != tt.want {
				t.Errorf("VisibleWidth(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestPad(t *testing.T) {
	if got := PadRight("\x1b[1mab\x1b[0m", 4); got != "\x1b[1mab\x1b[0m  " {
		t.Errorf("PadRight = %q", got)
	}
	if got := PadLeft("日", 4); got != "  日" {
		t.Errorf("PadLeft = %q", got)
	}
	if got := PadRight("abcdef", 3); got != "abcdef" {
		t.Errorf("PadRight wider text = %q", got)
	}
}

func TestTruncate(t *testing.T) {
	tests := map[string]struct {
		in    string
		width int
		want  string
	}{
		"fits":             {"hello", 5, "hello"},
		"cut":              {"hello world", 8, "hello w…"},
		"zero width":       {"hello", 0, "hello"},
		"width one":        {"hello", 1, "h"},
		"wide rune edge":   {"日本語", 4, "日…"},
		"styled cut":       {"\x1b[31mhello world\x1b[0m", 6, "\x1b[31mhello…\x1b[0m"},
		"reset before cut": {"\x1b[31mab\x1b[0mcdefgh", 4, "\x1b[31mab\x1b[0mc…"},
		"grapheme kept":    {"e\u0301e\u0301e\u0301e\u0301", 3, "e\u0301e\u0301…"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := Truncate(tt.in, tt.width)
			if got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.in, tt.width, got, tt.want)
			}
			if tt.width > 0 && VisibleWidth(got) > tt.width {
				t.Errorf("Truncate(%q, %d) is %d cells wide", tt.in, tt.width, VisibleWidth(got))
			}
		})
	}
}

func TestTruncateMiddle(t *testing.T) {
	tests := map[string]struct {
		in    string
		width int
		want  string
	}{
		"fits":   {"feature/x", 20, "feature/x"},
		"cut":    {"feature/very-long-branch-name", 21, "feature/ve…ranch-name"},
		"zero":   {"abcdef", 0, "abcdef"},
		"tiny":   {"abcdef", 1, "a"},
		"styled": {"\x1b[1mabcdefghij\x1b[0m", 5, "ab…ij"},
		"wide":   {"日本語テキスト", 7, "日…スト"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := TruncateMiddle(tt.in, tt.width)
			if got != tt.want {
				t.Errorf("TruncateMiddle(%q, %d) = %q, want %q", tt.in, tt.width, got, tt.want)
			}
			if tt.width > 0 && VisibleWidth(got) > tt.width {
				t.Errorf("TruncateMiddle(%q, %d) is %d cells wide", tt.in, tt.width, VisibleWidth(got))
			}
		})
	}
}

func TestScreenRows(t *testing.T) {
	lines := []string{"short", strings.Repeat("x", 25), ""}
	if got := ScreenRows(lines, 10); got != 5 {
		t.Errorf("ScreenRows(width 10) = %d, want 5", got)
	}
	if got := ScreenRows(lines, 0); got != 3 {
		t.Errorf("ScreenRows(width 0) = %d, want 3", got)
	}
}

func TestScreenRowsWideCharacterAtEdge(t *testing.T) {
	// Five cells of ASCII, then a wide rune that does not fit into the sixth
	// cell of a six-cell terminal: it starts the second row.
	if got := ScreenRows([]string{"abcde日"}, 6); got != 2 {
		t.Errorf("ScreenRows = %d, want 2", got)
	}
	if got := ScreenRows([]string{"abcdef"}, 6); got != 1 {
		t.Errorf("a line exactly as wide as the terminal takes %d rows", got)
	}
}

func TestTruncateClosesHyperlink(t *testing.T) {
	in := Hyperlink("https://example.com", "a long link text")
	got := Truncate(in, 6)
	want := "\x1b]8;;https://example.com\x1b\\a lon…\x1b]8;;\x1b\\"
	if got != want {
		t.Errorf("Truncate = %q, want %q", got, want)
	}
}

func TestEscapeLen(t *testing.T) {
	tests := map[string]int{"plain": 0, "\x1b[31mx": 5, "\x1b]8;;u\x07x": 7, "\x1b7x": 2, "\x1b[": 2}
	for in, want := range tests {
		if got := EscapeLen(in); got != want {
			t.Errorf("EscapeLen(%q) = %d, want %d", in, got, want)
		}
	}
}
