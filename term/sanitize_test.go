package term

import (
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSanitize(t *testing.T) {
	tests := map[string]struct{ in, want string }{
		"plain text":            {"hello, wörld ✓\nnext", "hello, wörld ✓\nnext"},
		"colors kept":           {"\x1b[1;31mred\x1b[0m and \x1b[38;5;196mmore\x1b[m", "\x1b[1;31mred\x1b[0m and \x1b[38;5;196mmore\x1b[m"},
		"24-bit to 256 colors":  {"\x1b[38;2;0;22;31;48;2;1;2;3mx", "\x1b[38;5;233;48;5;16mx"},
		"24-bit among others":   {"\x1b[1;38;2;255;0;0;4mx", "\x1b[1;38;5;196;4mx"},
		"24-bit as attributes":  {"\x1b[38;2;8;7;0mx\x1b[48;2;5;8;21my", "\x1b[38;5;232mx\x1b[48;5;232my"},
		"256 colors rewritten":  {"\x1b[38;5;007mx", "\x1b[38;5;7mx"},
		"underline color cut":   {"\x1b[4;58;5;8;31mx", "\x1b[4;31mx"},
		"underline rgb dropped": {"\x1b[58;2;8;8;8mx\x1b[59my", "xy"},
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
		"c1 controls":           {"a\u0085b\u0084c\u009cd", "abcd"},
		"8-bit csi":             {"a\u009b2Jb\u009b31mc\u009b1 qd", "abcd"},
		"8-bit csi malformed":   {"a\u009b\u00e9b\u009b31\u0085m", "aéb31m"},
		"8-bit osc to bel":      {"a\u009d52;c;Zm9v\x07b", "ab"},
		"8-bit osc to st":       {"a\u009d0;title\u009cb", "ab"},
		"8-bit osc to esc st":   {"a\u009d8;;https://evil.example\x1b\\b", "ab"},
		"8-bit strings":         {"\u0090q\u009c\u0098x\x07\u009ey\u009c\u009fz\u009cend", "end"},
		"8-bit unterminated":    {"Ko\u009eenka\nnext\x07", "Koenka\nnext"},
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
		"return before reset":   {"\x1b[31merror: build failed\r\x1b[0m\nnext", "\x1b[31merror: build failed\x1b[0m\nnext"},
		"return before erase":   {"hello\r\x1b[K", "hello"},
		"return, colors, crlf":  {"\x1b[1mkept\x1b[41m\r\x1b[m\r\nx", "\x1b[1mkept\x1b[41m\x1b[m\nx"},
		"return before a tab":   {"abcdefghi\r\tx", "        x"},
		"return before a space": {"abc\r d", " d"},
		"two returns, colors":   {"abc\r\x1b[1m\rdef", "\x1b[1mdef"},
		"colors after return":   {"abc\r\x1b[1m\x1b[31mdef", "\x1b[1m\x1b[31mdef"},
		"return before control": {"abc\r\x07\u009b2J", "abc"},
		"unterminated string":   {"line1\n\x1b]0;x\nline2\nline3\n", "line1\n\nline2\nline3\n"},
		"unterminated strings":  {"a\x1bPq\nb\x1b_x\nc\x1b^\x1bXy\nd", "a\nb\nc\nd"},
		"string ended later":    {"a\x1b]0;x\nb\x1bPy\nc\x07d\x1b]z\ne", "ad\ne"},
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
		"extended colors":  {"\x1b[48;2;1;2;3;58;5;4m\nx", []string{"\x1b[48;5;16m\x1b[0m", "\x1b[48;5;16mx\x1b[0m"}},
		"extended cleared": {"\x1b[38;5;1;48;5;2;58;5;3m\n\x1b[39;49;59m\nx", []string{"\x1b[38;5;1;48;5;2m\x1b[0m", "\x1b[38;5;1;48;5;2m\x1b[39;49m", "x"}},
		"rgb not a dim":    {"\x1b[1m\n\x1b[38;2;0;22;31m\nx", []string{"\x1b[1m\x1b[0m", "\x1b[1m\x1b[38;5;233m\x1b[0m", "\x1b[1;38;5;233mx\x1b[0m"}},
		"rgb not a reset":  {"\x1b[1m\n\x1b[48;2;0;0;0m\nx", []string{"\x1b[1m\x1b[0m", "\x1b[1m\x1b[48;5;16m\x1b[0m", "\x1b[1;48;5;16mx\x1b[0m"}},
		"return and reset": {"\x1b[31mfailed\r\x1b[0m\nx", []string{"\x1b[31mfailed\x1b[0m", "x"}},
		"string cut off":   {"a\n\x1b]0;x\nb", []string{"a", "", "b"}},
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
	for _, seed := range []string{
		"\x1b[1;31mred\x1b[0m", "a\rb\tc\n", "\x1b]0;t\x07", "\x1b[38;2;1;2", "\xff\x1b",
		"\u009b31m\u009d52;c;Zm9v\u009c", "\u0090q\x1b\\\u009ex",
		"\x1b[32m+\x1b[m\x1b[32mTWO changed\x1b[m\x1b[41m\r\x1b[m\n", "\x1b[58;5;8;38;2;8;7;0mx", "a\x1b]0;t\nb",
	} {
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
			r, size := utf8.DecodeRuneInString(out[i:])
			if r == utf8.RuneError && size == 1 {
				t.Fatalf("Sanitize(%q) kept invalid UTF-8 at %d", in, i)
			}
			if control(r) && r != '\n' {
				t.Fatalf("Sanitize(%q) kept control %U", in, r)
			}
			i += size
		}
		plain := Plain(in)
		if plain != StripANSI(out) || strings.Contains(plain, "\x1b") {
			t.Fatalf("Plain(%q) = %q, not Sanitize without its colors", in, plain)
		}
		line := PlainLine(in)
		if strings.ContainsFunc(line, func(r rune) bool { return control(r) || lineBreak(r) || bidiControl(r) }) ||
			line != strings.TrimSpace(line) || strings.Contains(line, "  ") {
			t.Fatalf("PlainLine(%q) = %q", in, line)
		}
		if again := PlainLine(line); again != line {
			t.Fatalf("PlainLine is not idempotent: %q, then %q", line, again)
		}
		if again := Sanitize(out); again != out {
			t.Fatalf("Sanitize is not idempotent: %q, then %q", out, again)
		}
		// What is left of the input once it is plain is visible text, and
		// visible text survives the sequences put around it.
		if line != "" {
			checkVisibleTextKept(t, line)
		}
		// A string cut off in one line leaves the lines after it alone.
		if rest := Sanitize("\x1b]0;" + line + "\nnext"); !strings.HasSuffix(rest, "\nnext") {
			t.Fatalf("Sanitize(%q) lost the line after an unterminated string: %q", "\x1b]0;"+line+"\nnext", rest)
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

// An escape string runs to its terminator across newlines, as a terminal
// reads it: none of a clipboard write's payload may show.
func TestSanitizeDropsEscapeStringsAcrossLines(t *testing.T) {
	in := "before\x1b]52;c;secret\npayload\x1b\\after"
	if got, want := Sanitize(in), "beforeafter"; got != want {
		t.Fatalf("Sanitize = %q, want %q", got, want)
	}
	if got := SanitizeLines(in); len(got) != 1 || got[0] != "beforeafter" {
		t.Fatalf("SanitizeLines = %q, want [beforeafter]", got)
	}
	// Without a terminator the string runs to the end of its line, not to
	// the end of the text.
	if got, want := Sanitize("a\x1bPq\nrest"), "a\nrest"; got != want {
		t.Fatalf("Sanitize = %q, want %q", got, want)
	}
}

// A line of git diff --color for a file with CRLF line endings ends with a
// carriage return between two color sequences. It is the line's ending, not
// a redraw: the added lines stay.
func TestSanitizeKeepsLinesOfAColoredCRLFDiff(t *testing.T) {
	in := "\x1b[36m@@ -1,2 +1,3 @@\x1b[m\n" +
		" one\x1b[m\r\n" +
		"\x1b[31m-two\x1b[m\r\n" +
		"\x1b[32m+\x1b[m\x1b[32mTWO changed\x1b[m\x1b[41m\r\x1b[m\n" +
		"\x1b[32m+\x1b[m\x1b[32mthree\x1b[m\x1b[41m\r\x1b[m\n"
	const want = "@@ -1,2 +1,3 @@\n one\n-two\n+TWO changed\n+three\n"
	if got := Plain(in); got != want {
		t.Errorf("Plain =\n%q\nwant\n%q", got, want)
	}
	if got := StripANSI(Sanitize(in)); got != want {
		t.Errorf("Sanitize without its colors =\n%q\nwant\n%q", got, want)
	}
	if got := StripANSI(strings.Join(SanitizeLines(in), "\n")); got != strings.TrimSuffix(want, "\n") {
		t.Errorf("SanitizeLines without their colors =\n%q", got)
	}
}

// Text survives whatever harmless sequences stand around it: colors, cursor
// movement, titles, hyperlinks, an underline color, and a carriage return
// that ends a line, with or without a sequence after it.
func TestSanitizeKeepsVisibleText(t *testing.T) {
	for _, text := range []string{
		"word", "two words", "  indented", "grün ✓ 日本語", "50% [=====>    ]", "a]b[c;d m", "+added", "\ufffd",
	} {
		checkVisibleTextKept(t, text)
	}
}

// checkVisibleTextKept fails t when text, which holds nothing Sanitize takes
// out, does not come through Plain, Sanitize and SanitizeLines whole once
// escape sequences and line endings are put around it.
func checkVisibleTextKept(t *testing.T, text string) {
	t.Helper()
	decorated := "\x1b[1;31m" + text + "\x1b[0m\r\n" + // a painted line of a CRLF file
		"\x1b[32m" + text + "\x1b[41m\r\x1b[m\n" + // git's CRLF, with colors around the return
		text + "\r\x1b[0m\n" + // a reset after the return
		"\x1b]0;title\x07" + text + "\x1b[2K\x1b[1A\n" + // a title, an erased line, a cursor movement
		"\x1b]8;;https://example.com\x1b\\" + text + "\x1b]8;;\x1b\\\n" + // a hyperlink
		"\x1b[58;5;8m\x1b[38;2;8;7;0m" + text + "\x1b[59m\n" + // colors that are dropped or converted
		"\u009b31m" + text + "\u009d0;title\u009c\n" + // the 8-bit forms
		"gone\r" + text + "\x1bP\n" + // a redrawn line, and a string that is cut off
		text
	want := strings.Repeat(text+"\n", 8) + text
	if got := Plain(decorated); got != want {
		t.Errorf("Plain lost visible text of %q:\n%q\nwant\n%q", text, got, want)
	}
	if got := StripANSI(Sanitize(decorated)); got != want {
		t.Errorf("Sanitize lost visible text of %q:\n%q\nwant\n%q", text, got, want)
	}
	if got := StripANSI(strings.Join(SanitizeLines(decorated), "\n")); got != want {
		t.Errorf("SanitizeLines lost visible text of %q:\n%q\nwant\n%q", text, got, want)
	}
}

// Unterminated escape strings, each cut off at the end of its line, are
// handled in time linear in the text's length.
func TestSanitizeUnterminatedStringsIsLinear(t *testing.T) {
	in := strings.Repeat("x\x1b]0;title\n", 200000)
	start := time.Now()
	out := Sanitize(in)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Sanitize of %d unterminated strings took %v", 200000, elapsed)
	}
	if want := strings.Repeat("x\n", 200000); out != want {
		t.Fatal("unterminated strings took more than the rest of their lines")
	}
}

// A 24-bit color becomes the nearest of the 256: a color of the cube, or a
// gray where that is nearer.
func TestNearest256(t *testing.T) {
	tests := map[[3]int]int{
		{0, 0, 0}:       16,
		{255, 255, 255}: 231,
		{255, 0, 0}:     196,
		{95, 135, 175}:  67,
		{100, 130, 180}: 67,
		{128, 128, 128}: 244,
		{8, 8, 8}:       232,
		{238, 238, 238}: 255,
		{250, 250, 250}: 231,
		{47, 114, 115}:  24,
	}
	for rgb, want := range tests {
		if got := nearest256(rgb[0], rgb[1], rgb[2]); got != want {
			t.Errorf("nearest256(%v) = %d, want %d", rgb, got, want)
		}
	}
}

// Tabs are expanded in time linear in the line's length.
func TestSanitizeManyTabsIsLinear(t *testing.T) {
	line := strings.Repeat("x\t", 200000)
	start := time.Now()
	out := Sanitize(line)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Sanitize of %d tabs took %v", 200000, elapsed)
	}
	if want := strings.Repeat("x       ", 200000); out != want {
		t.Fatal("tab stops wrong on a long line")
	}
}

// The payload of an 8-bit escape string goes with it on its own line, and a
// line of SanitizeLines carries none of it either.
func TestSanitizeLinesDrops8BitStrings(t *testing.T) {
	got := SanitizeLines("\x1b[31mred\u009d52;c;Zm9v\x07\nnext")
	want := []string{"\x1b[31mred\x1b[0m", "\x1b[31mnext\x1b[0m"}
	if !slices.Equal(got, want) {
		t.Fatalf("SanitizeLines = %q, want %q", got, want)
	}
}

func TestPlain(t *testing.T) {
	tests := map[string]struct{ in, want string }{
		"plain text":   {"hello, wörld\nnext", "hello, wörld\nnext"},
		"colors gone":  {"\x1b[1;31mred\x1b[0m and \x1b[38;5;196mmore", "red and more"},
		"clipboard":    {"a\x1b]52;c;Zm9v\x07b\u009d52;c;Zm9v\u009cc", "abc"},
		"controls":     {"a\x00b\x07c\x08d\x7fe\u0085f", "abcdef"},
		"return":       {"pay the attacker\rpay your bank", "pay your bank"},
		"tab stops":    {"a\tb", "a       b"},
		"invalid utf8": {"a\xff\xfeb", "a\ufffdb"},
		"bidi kept":    {"\u202eabc\u202c", "\u202eabc\u202c"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := Plain(tt.in); got != tt.want {
				t.Errorf("Plain(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestPlainLine(t *testing.T) {
	tests := map[string]struct{ in, want string }{
		"plain text":       {"Ada Lovelace", "Ada Lovelace"},
		"line breaks":      {"Re: one\r\n two\nthree\rfour\vfive\fsix", "Re: one two three four five six"},
		"unicode breaks":   {"a\u0085b\u2028c\u2029d", "a b c d"},
		"tabs":             {"a\tb\t\tc", "a b c"},
		"return keeps all": {"pay the attacker\rpay your bank", "pay the attacker pay your bank"},
		"white space runs": {"  a   b\u00a0\u00a0c  ", "a b c"},
		"bidi overrides":   {"invoice\u202efdp.exe", "invoicefdp.exe"},
		"bidi isolates":    {"a\u2066b\u2067c\u2068d\u2069e\u202af\u202bg\u202ch\u202di", "abcdefghi"},
		"marks kept":       {"a\u200fb\u200dc", "a\u200fb\u200dc"},
		"colors gone":      {"\x1b[31mred\x1b[0m", "red"},
		"string across":    {"a\x1b]52;c;Zm9v\nrest\x07b", "ab"},
		"8-bit across":     {"a\u009d52;c;Zm9v\nrest\x07b", "ab"},
		"only escapes":     {"\x1b[2J \x1b]0;t\x07\r\n\t", ""},
		"invalid utf8":     {"a\xff\xfeb", "a\ufffdb"},
		"empty":            {"", ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := PlainLine(tt.in); got != tt.want {
				t.Errorf("PlainLine(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// A line of 8-bit escape strings without a terminator is handled in time
// linear in its length, and each loses its introducer alone.
func TestSanitizeUnterminated8BitStringsIsLinear(t *testing.T) {
	line := strings.Repeat("\u009dx", 200000)
	start := time.Now()
	out := Sanitize(line)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Sanitize of %d unterminated strings took %v", 200000, elapsed)
	}
	if want := strings.Repeat("x", 200000); out != want {
		t.Fatal("unterminated strings lost more than their introducers")
	}
}
