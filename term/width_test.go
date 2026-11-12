package term

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
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
		"character set":      {"\x1b(B\x1b[mtext", "text"},
		"escape before rune": {"\x1bé", "é"},
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
		"tab":          {"a\tb", 9},
		"tab alone":    {"\t", 8},
		"tab at stop":  {"12345678\tx", 17},
		"two tabs":     {"ab\t\tc", 17},
		"tab, styled":  {"\x1b[1ma\x1b[0m\t\x1b[31mb", 9},
		"tab, wide":    {"日本\tx", 9},
		"tabs, lines":  {"a\tb\n\tcd", 10},
		"ambiguous":    {"─│…●→", 5},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := VisibleWidth(tt.in); got != tt.want {
				t.Errorf("VisibleWidth(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestWidthAt(t *testing.T) {
	tests := []struct {
		in     string
		column int
		want   int
	}{
		{"abc", 0, 3},
		{"abc", 7, 3},
		{"\tx", 0, 9},
		{"\tx", 4, 5},
		{"\tx", 7, 2},
		{"\tx", 8, 9},
		{"a\tb\tc", 3, 14},
		{"\x1b[1m\t\x1b[0m", 6, 2},
		{"\tx", -5, 9},
		{"", 3, 0},
	}
	for _, tt := range tests {
		if got := WidthAt(tt.in, tt.column); got != tt.want {
			t.Errorf("WidthAt(%q, %d) = %d, want %d", tt.in, tt.column, got, tt.want)
		}
	}
}

// Text is as wide under a Chinese, Japanese or Korean locale as under any
// other: the characters of ambiguous width count one cell, whatever
// go-runewidth took from LANG, LC_ALL or RUNEWIDTH_EASTASIAN.
func TestWidthsDoNotDependOnTheLocale(t *testing.T) {
	was := runewidth.DefaultCondition.EastAsianWidth
	t.Cleanup(func() { runewidth.DefaultCondition.EastAsianWidth = was })
	for _, eastAsian := range []bool{false, true} {
		runewidth.DefaultCondition.EastAsianWidth = eastAsian
		rule := strings.Repeat("─", 60)
		if got := VisibleWidth(rule); got != 60 {
			t.Errorf("east asian %v: VisibleWidth of 60 rules = %d", eastAsian, got)
		}
		if got := ScreenRows([]string{rule}, 80); got != 1 {
			t.Errorf("east asian %v: ScreenRows of 60 rules on 80 columns = %d, want 1", eastAsian, got)
		}
		if got, want := Truncate("abcdef", 4), "abc…"; got != want {
			t.Errorf("east asian %v: Truncate = %q, want %q", eastAsian, got, want)
		}
		if got, want := TruncateMiddle("abcdefgh", 5), "ab…gh"; got != want {
			t.Errorf("east asian %v: TruncateMiddle = %q, want %q", eastAsian, got, want)
		}
		if got, want := PadRight("●", 3), "●  "; got != want {
			t.Errorf("east asian %v: PadRight = %q, want %q", eastAsian, got, want)
		}
		if got, want := Wrap("─── ───", 4, ""), "───\n───"; got != want {
			t.Errorf("east asian %v: Wrap = %q, want %q", eastAsian, got, want)
		}
		if got, want := Sanitize("─\tx"), "─       x"; got != want {
			t.Errorf("east asian %v: Sanitize = %q, want %q", eastAsian, got, want)
		}
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

// Padding counts a tab as the cells it takes up, and the result is as wide
// as asked for: spaces before a tab would move its tab stop, so PadLeft
// writes the tab as the spaces it stood for.
func TestPadWithTabs(t *testing.T) {
	if got, want := PadRight("a\tb", 12), "a\tb   "; got != want {
		t.Errorf("PadRight = %q, want %q", got, want)
	}
	if got, want := PadLeft("a\tb", 12), "   a       b"; got != want {
		t.Errorf("PadLeft = %q, want %q", got, want)
	}
	if got, want := PadLeft("\x1b[1ma\x1b[0m\tb", 12), "   \x1b[1ma\x1b[0m       b"; got != want {
		t.Errorf("PadLeft of styled text = %q, want %q", got, want)
	}
	if got, want := PadLeft("a\tb", 9), "a\tb"; got != want {
		t.Errorf("PadLeft of text that fits = %q, want %q", got, want)
	}
	for _, s := range []string{PadRight("a\tb", 12), PadLeft("a\tb", 12)} {
		if got := VisibleWidth(s); got != 12 {
			t.Errorf("%q is %d cells wide, want 12", s, got)
		}
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
		"tab kept":         {"a\tbcdef", 10, "a\tb…"},
		"tab cut":          {"abc\tdef", 6, "abc…"},
		"tab fits":         {"a\tb", 9, "a\tb"},
		"second line":      {"ab\nabcdefgh", 4, "ab\nabc…"},
		"first line":       {"abcdefgh\nxy", 4, "abc…\nxy"},
		"every line":       {"abcdefgh\n\n日本語\nxy", 4, "abc…\n\n日…\nxy"},
		"lines, width one": {"abc\nd\nef", 1, "a\nd\ne"},
		"color over lines": {"\x1b[31mabcdefgh\nxy\x1b[0m", 4, "\x1b[31mabc…\x1b[0m\nxy\x1b[0m"},
		"color from above": {"\x1b[31mab\nabcdefgh\nxy", 4, "\x1b[31mab\nabc…\x1b[0m\nxy"},
		"closed above":     {"\x1b[31mab\x1b[0m\nabcdefgh", 4, "\x1b[31mab\x1b[0m\nabc…"},
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
		"fits":         {"feature/x", 20, "feature/x"},
		"cut":          {"feature/very-long-branch-name", 21, "feature/ve…ranch-name"},
		"zero":         {"abcdef", 0, "abcdef"},
		"tiny":         {"abcdef", 1, "a"},
		"styled":       {"\x1b[1mabcdefghij\x1b[0m", 5, "ab…ij"},
		"wide":         {"日本語テキスト", 7, "日…スト"},
		"lines":        {"ab\nabcdefgh\nxy", 5, "ab\nab…gh\nxy"},
		"first":        {"abcdefgh\nxy", 4, "ab…h\nxy"},
		"styled lines": {"\x1b[1mab\x1b[0m\nabcdefgh", 5, "ab\nab…gh"},
		"tab":          {"a\tbcdefgh", 9, "a   …efgh"},
		"tab fits":     {"a\tb\nabcdefghijk", 9, "a\tb\nabcd…hijk"},
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

// A tab moves the cursor to the next tab stop of its row and stops at the
// row's last column: it never wraps, the character after it does.
func TestScreenRowsTabs(t *testing.T) {
	tests := []struct {
		line  string
		width int
		want  int
	}{
		{"a\tb", 10, 1},
		{"a\tb\tc\td", 10, 2}, // b in column 9, c in the last column, d on the next row
		{"123456789\tZ", 10, 1},
		{"1234567890\tZ", 10, 2},
		{"12345678\tZ\tY", 10, 2},
		{"a\tb\tc\td", 80, 1},
		{"\t\t\t\t\t\t\t\t\t\tx", 80, 1},
		{strings.Repeat("x", 12) + "\ty", 10, 2},
		{"\x1b[1ma\x1b[0m\tb", 8, 1},
		{"a\tbc", 8, 2},
	}
	for _, tt := range tests {
		if got := ScreenRows([]string{tt.line}, tt.width); got != tt.want {
			t.Errorf("ScreenRows(%q, %d) = %d, want %d", tt.line, tt.width, got, tt.want)
		}
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
	tests := map[string]int{
		"plain": 0, "\x1b[31mx": 5, "\x1b]8;;u\x07x": 7, "\x1b7x": 2, "\x1b[": 2,
		"\x1b(Bx":        3, // a character set: ESC, an intermediate, a final byte
		"\x1b(":          2, // cut off after the intermediate
		"\x1bé":          1, // ESC alone, the rune after it whole
		"\x1b\x1b[31m":   1, // the second ESC starts a sequence of its own
		"\x1bXsos\x1b\\": 7,
	}
	for in, want := range tests {
		if got := EscapeLen(in); got != want {
			t.Errorf("EscapeLen(%q) = %d, want %d", in, got, want)
		}
	}
}
