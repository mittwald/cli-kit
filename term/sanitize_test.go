package term

import (
	"slices"
	"strings"
	"testing"
)

func TestSanitize(t *testing.T) {
	tests := map[string]struct{ in, want string }{
		"plain text":            {"hello, wörld ✓\nnext", "hello, wörld ✓\nnext"},
		"colors kept":           {"\x1b[1;31mred\x1b[0m and \x1b[38;5;196mmore\x1b[m", "\x1b[1;31mred\x1b[0m and \x1b[38;5;196mmore\x1b[m"},
		"24-bit colors kept":    {"\x1b[38;2;0;22;31;48;2;1;2;3mx", "\x1b[38;2;0;22;31;48;2;1;2;3mx"},
		"empty parameter":       {"\x1b[;32mx", "\x1b[;32mx"},
		"conceal dropped":       {"\x1b[8msecret\x1b[28m", "secret"},
		"unsafe parameter cut":  {"\x1b[1;8;31mx", "\x1b[1;31mx"},
		"unknown parameter cut": {"\x1b[999;32mx", "\x1b[32mx"},
		"invalid color dropped": {"\x1b[38;5;256;1mx", "\x1b[1mx"},
		"incomplete color":      {"\x1b[1;38;2;0;22mx", "\x1b[1mx"},
		"unknown color kind":    {"\x1b[38;3;4mx", "\x1b[4mx"},
		"keyboard mode dropped": {"\x1b[>4;1mx", "x"},
		"colon parameters":      {"\x1b[4:3mx", "x"},
		"cursor movement":       {"a\x1b[2Ab\x1b[Kc\x1b[2J", "abc"},
		"title and clipboard":   {"\x1b]0;owned\x07a\x1b]52;c;cHduZWQ=\x1b\\b", "ab"},
		"hyperlink":             {"\x1b]8;;https://evil.example\x1b\\https://good.example\x1b]8;;\x1b\\", "https://good.example"},
		"device control":        {"\x1bP+q544e\x1b\\x", "x"},
		"charset escape":        {"\x1b(B\x1b[mreset", "\x1b[mreset"},
		"short escapes":         {"\x1b7saved\x1b8\x1bM", "saved"},
		"escape before rune":    {"\x1bé", "é"},
		"double escape":         {"\x1b\x1b[31mx", "\x1b[31mx"},
		"lone escape at end":    {"abc\x1b", "abc"},
		"control characters":    {"a\x00b\x07c\x08d\x7fe\x0ef", "abcdef"},
		"c1 controls":           {"a\u009b31mb\u0085c", "a31mbc"},
		"invalid utf-8":         {"a\xffb", "a�b"},
		"tabs to stops":         {"a\tb\tc\n\tx\n12345678\ty", "a       b       c\n        x\n12345678        y"},
		"tab after wide runes":  {"日本\tx", "日本    x"},
		"tab after colors":      {"\x1b[1mab\x1b[0m\tc", "\x1b[1mab\x1b[0m      c"},
		"crlf":                  {"one\r\ntwo\r\n", "one\ntwo\n"},
		"returns at the end":    {"kept\r\r\nx", "kept\nx"},
		"progress redrawn":      {"10%\r50%\r100%", "100%"},
		"colors before return":  {"\x1b[32m10%\r\x1b[1m50%\rdone", "\x1b[1;32mdone"},
		"reset before return":   {"\x1b[1;31mx\x1b[0;32m\rdone", "\x1b[0;32mdone"},
		"cleared before return": {"\x1b[1mx\x1b[22m\x1b[2m\ry", "\x1b[22;2my"},
		"return inside a title": {"\x1b]0;a\rb\x07text", "text"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := Sanitize(tt.in); got != tt.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Text that has nothing to take out comes back as it is, not a copy.
func TestSanitizeSafeText(t *testing.T) {
	in := strings.Repeat("plain line\n", 3)
	if got := Sanitize(in); got != in {
		t.Errorf("Sanitize(%q) = %q", in, got)
	}
}

func TestSanitizeLines(t *testing.T) {
	tests := map[string]struct {
		in   string
		want []string
	}{
		"empty":           {"", nil},
		"one blank line":  {"\n", []string{""}},
		"final newline":   {"a\nb\n", []string{"a", "b"}},
		"no final":        {"a\nb", []string{"a", "b"}},
		"color carried":   {"\x1b[31mred\nstill red\x1b[0m\nplain", []string{"\x1b[31mred\x1b[0m", "\x1b[31mstill red\x1b[0m", "plain"}},
		"closed in line":  {"\x1b[31mred\x1b[0m\nplain", []string{"\x1b[31mred\x1b[0m", "plain"}},
		"attributes kept": {"\x1b[1;31m\n\x1b[44mx", []string{"\x1b[1;31m\x1b[0m", "\x1b[1;31m\x1b[44mx\x1b[0m"}},
		"repeated colors": {strings.Repeat("\x1b[31m", 1000) + "\nx", []string{strings.Repeat("\x1b[31m", 1000) + "\x1b[0m", "\x1b[31mx\x1b[0m"}},
		"replaced color":  {"\x1b[31m\n\x1b[32m\nx", []string{"\x1b[31m\x1b[0m", "\x1b[31m\x1b[32m\x1b[0m", "\x1b[32mx\x1b[0m"}},
		"selective reset": {"\x1b[1;2;3;4;31;44m\n\x1b[22;24;39m\nx", []string{
			"\x1b[1;2;3;4;31;44m\x1b[0m", "\x1b[1;2;3;4;31;44m\x1b[22;24;39m\x1b[0m", "\x1b[3;44mx\x1b[0m",
		}},
		"reset inside":     {"\x1b[1;31m\n\x1b[32;0;44m\nx", []string{"\x1b[1;31m\x1b[0m", "\x1b[1;31m\x1b[32;0;44m\x1b[0m", "\x1b[44mx\x1b[0m"}},
		"empty sequence":   {"\x1b[31m\n\x1b[m\nx", []string{"\x1b[31m\x1b[0m", "\x1b[31m\x1b[m", "x"}},
		"extended colors":  {"\x1b[48;2;1;2;3;58;5;4m\nx", []string{"\x1b[48;2;1;2;3;58;5;4m\x1b[0m", "\x1b[48;2;1;2;3;58;5;4mx\x1b[0m"}},
		"extended cleared": {"\x1b[38;5;1;48;5;2;58;5;3m\n\x1b[39;49;59m\nx", []string{"\x1b[38;5;1;48;5;2;58;5;3m\x1b[0m", "\x1b[38;5;1;48;5;2;58;5;3m\x1b[39;49;59m", "x"}},
		"rgb not a dim":    {"\x1b[1m\n\x1b[38;2;0;22;31m\nx", []string{"\x1b[1m\x1b[0m", "\x1b[1m\x1b[38;2;0;22;31m\x1b[0m", "\x1b[1;38;2;0;22;31mx\x1b[0m"}},
		"dropped carried":  {"\x1b[8;31mhidden\nx", []string{"\x1b[31mhidden\x1b[0m", "\x1b[31mx\x1b[0m"}},
		"progress redrawn": {"\x1b[33m10%\r\x1b[32mdone\nx", []string{"\x1b[33m\x1b[32mdone\x1b[0m", "\x1b[32mx\x1b[0m"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := SanitizeLines(tt.in); !slices.Equal(got, tt.want) {
				t.Errorf("SanitizeLines(%q) =\n%q\nwant\n%q", tt.in, got, tt.want)
			}
		})
	}
}

// Every line SanitizeLines returns stands on its own: shown alone, it looks
// the way it looks in the whole text, and it leaves the terminal's colors as
// it found them.
func TestSanitizeLinesStandAlone(t *testing.T) {
	in := "\x1b[1mbold \x1b[31mred\nstill both\x1b[22m\nred only\x1b[0m\nplain"
	for i, line := range SanitizeLines(in) {
		var r rendition
		r.applyText(line)
		if seq := r.sequence(); seq != "" {
			t.Errorf("line %d %q leaves %q set", i, line, seq)
		}
	}
}

// The state a relative rendition carries is as short as the attributes it
// sets, however many sequences set them.
func TestRenditionSequence(t *testing.T) {
	tests := map[string]struct {
		relative bool
		in, want string
	}{
		"nothing":            {false, "", ""},
		"one color":          {false, "\x1b[31m", "\x1b[31m"},
		"last color wins":    {false, "\x1b[31m\x1b[32m\x1b[33m", "\x1b[33m"},
		"order of slots":     {false, "\x1b[44m\x1b[31m\x1b[1m", "\x1b[1;31;44m"},
		"reset forgets":      {false, "\x1b[1;31m\x1b[0m\x1b[4m", "\x1b[4m"},
		"cleared is off":     {false, "\x1b[1;2m\x1b[22m", ""},
		"relative clear":     {true, "\x1b[22m\x1b[39m", "\x1b[22;39m"},
		"relative reset":     {true, "\x1b[1m\x1b[0m\x1b[32m", "\x1b[0;32m"},
		"clear after reset":  {true, "\x1b[0m\x1b[39m", "\x1b[0m"},
		"bold after clear":   {true, "\x1b[22m\x1b[1m", "\x1b[22;1m"},
		"22 clears dim too":  {true, "\x1b[2m\x1b[22m", "\x1b[22m"},
		"unsupported ignore": {false, "\x1b[999m\x1b[8m", ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			r := rendition{relative: tt.relative}
			r.applyText(tt.in)
			if got := r.sequence(); got != tt.want {
				t.Errorf("sequence after %q = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// A long run of tabs and returns is handled in one pass.
func TestSanitizeLongLine(t *testing.T) {
	in := strings.Repeat("\t\x1b[31mx\r", 1<<14) + "end"
	if got := Sanitize(in); got != "\x1b[31mend" {
		t.Errorf("Sanitize(long line) = %q", got)
	}
}

func FuzzSanitize(f *testing.F) {
	for _, seed := range []string{"\x1b[1;31mred\x1b[0m", "a\rb\tc\n", "\x1b]0;t\x07", "\x1b[38;2;1;2", "\xff\x1b"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := Sanitize(in)
		for i := 0; i < len(out); {
			if n := EscapeLen(out[i:]); n > 0 {
				if _, ok := sgrParams(out[i : i+n]); !ok {
					t.Fatalf("Sanitize(%q) kept %q", in, out[i:i+n])
				}
				i += n
				continue
			}
			if out[i] < 0x20 && out[i] != '\n' {
				t.Fatalf("Sanitize(%q) kept control %#x", in, out[i])
			}
			i++
		}
		if again := Sanitize(out); again != out {
			t.Fatalf("Sanitize is not idempotent: %q, then %q", out, again)
		}
	})
}

// A tab stop is counted in cells as a terminal draws them: a joined emoji is
// one cluster two cells wide, not one cell per rune.
func TestSanitizeTabAfterJoinedEmoji(t *testing.T) {
	if got, want := Sanitize("👩‍💻\tx"), "👩‍💻      x"; got != want {
		t.Fatalf("Sanitize = %q, want %q", got, want)
	}
}
