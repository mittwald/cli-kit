package theme

import (
	"regexp"
	"strings"
	"testing"
)

func TestParseStyle(t *testing.T) {
	tests := []struct {
		spec      string
		trueColor bool
		want      Style
	}{
		{"245", false, "38;5;245"},
		{"0", false, "38;5;0"},
		{"160:bold", false, "38;5;160;1"},
		{" Red : B ", false, "31;1"},
		{"lightblue:underline", false, "94;4"},
		{"fg=white:bg=red", false, "37;41"},
		{"background=darkgray", false, "100"},
		{"bg=17", false, "48;5;17"},
		{"foreground=cyan", false, "36"},
		{"italic:blink:reverse", false, "3;5;7"},
		{"i:u:invert:fuzzy:dim", false, "3;4;7;2;2"},
		{"strikethrough", false, "9"},
		{"bold::unknown:shade=red", false, "1"},
		// Hex colors are 24-bit only where the terminal takes them.
		{"#ff8000", true, "38;2;255;128;0"},
		{"0xF80", true, "38;2;255;136;0"},
		{"bg=#000000", true, "48;2;0;0;0"},
		{"300", true, "38;2;51;0;0"}, // past the palette, so three hex digits
		{"#ff8000", false, "38;5;208"},
		{"0xF80", false, "38;5;208"},
		{"bg=#000000", false, "48;5;16"},
		{"#808080", false, "38;5;244"},
		{"300", false, "38;5;233"},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			colorterm := ""
			if tt.trueColor {
				colorterm = "truecolor"
			}
			t.Setenv("COLORTERM", colorterm)
			if got, ok := ParseStyle(tt.spec); !ok || got != tt.want {
				t.Errorf("ParseStyle(%q) = %q, %v; want %q", tt.spec, got, ok, tt.want)
			}
		})
	}
	for _, spec := range []string{"", ":", "nonsense", "shade=red", "fg=nonsense", "256x", "#12345", "#ggg", "0x", "+12", "1_0", "\x1b[31m"} {
		if got, ok := ParseStyle(spec); ok || got != "" {
			t.Errorf("ParseStyle(%q) = %q, %v; want nothing", spec, got, ok)
		}
	}
}

func TestPaintMatches(t *testing.T) {
	word := regexp.MustCompile(`err\w*`)
	tests := []struct {
		name  string
		style Style
		text  string
		re    *regexp.Regexp
		want  string
	}{
		{"plain text", "1", "an error here", word, "an \x1b[1merror\x1b[0m here"},
		{"no match", "1", "fine", word, "fine"},
		{"zero style", "", "an error", word, "an error"},
		{"empty text", "1", "", word, ""},
		{"empty matches paint nothing", "1", "ab", regexp.MustCompile(`x*`), "ab"},
		{
			"painted text resumes its color",
			"1", "\x1b[2mok, error, ok\x1b[0m", word,
			"\x1b[2mok, \x1b[0m\x1b[1merror\x1b[0m\x1b[2m, ok\x1b[0m",
		},
		{
			"attributes add up",
			"1", "\x1b[2m\x1b[31mx error\x1b[m", word,
			"\x1b[2m\x1b[31mx \x1b[0m\x1b[1merror\x1b[0m\x1b[2;31m\x1b[m",
		},
		{
			"a reset in a sequence drops what came before it",
			"1", "\x1b[2m\x1b[0;31merror", word,
			"\x1b[2m\x1b[0;31m\x1b[0m\x1b[1merror\x1b[0m\x1b[31m",
		},
		{
			"palette index 0 is no reset",
			"1", "\x1b[2m\x1b[38;5;0merror", word,
			"\x1b[2m\x1b[38;5;0m\x1b[0m\x1b[1merror\x1b[0m\x1b[2;38;5;0m",
		},
		{
			"no match inside an escape sequence",
			"1", "\x1b]8;;https://example.com/errors\x1b\\link\x1b]8;;\x1b\\", word,
			"\x1b]8;;https://example.com/errors\x1b\\link\x1b]8;;\x1b\\",
		},
		{
			"colon colors resume",
			"1", "\x1b[38:5:196merror after", word,
			"\x1b[38:5:196m\x1b[0m\x1b[1merror\x1b[0m\x1b[38:5:196m after",
		},
		{"nil pattern paints everything", "1", "\x1b[31mred\x1b[0m text", nil, "\x1b[1mred text\x1b[0m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.style.PaintMatches(tt.text, tt.re); got != tt.want {
				t.Errorf("PaintMatches(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestSGRState(t *testing.T) {
	tests := []struct {
		name      string
		sequences []string
		want      string
	}{
		{"nothing", nil, ""},
		{"empty sequence resets", []string{"1", ""}, ""},
		{"zero resets", []string{"1", "0"}, ""},
		{"zero within resets what came before", []string{"1", "00;31"}, "\x1b[31m"},
		{"empty parameter resets", []string{"1", ";31"}, "\x1b[31m"},
		{"attributes add up", []string{"1", "31"}, "\x1b[1;31m"},
		{"a color replaces a color", []string{"31", "32", "38;5;1", "34"}, "\x1b[34m"},
		{"background and foreground", []string{"41", "33", "48;2;1;2;3"}, "\x1b[33;48;2;1;2;3m"},
		{"palette index 0 is no reset", []string{"1", "38;5;0"}, "\x1b[1;38;5;0m"},
		{"attributes switch off", []string{"1;2;3;4;5;7;8;9;53;31;41", "22;23;24;25;27;28;29;55;39;49"}, ""},
		{"underline color", []string{"4;58;5;9", "59"}, "\x1b[4m"},
		{"incomplete color", []string{"38;5"}, ""},
		{"unknown selector", []string{"1", "38;9;0"}, ""},
		{"unknown codes are skipped", []string{"1;10;51;99"}, "\x1b[1m"},
		{"leading zeros", []string{"01;031"}, "\x1b[1;31m"},
		{"colon colors", []string{"38:5:196;48:2::1:2:3"}, "\x1b[38:5:196;48:2::1:2:3m"},
		{"a colon color replaces a color", []string{"31", "38:5:196"}, "\x1b[38:5:196m"},
		{"curly underline", []string{"4:3;58:5:1"}, "\x1b[4:3;58:5:1m"},
		{"underline off by sub-parameter", []string{"4:3", "4:0"}, ""},
		{"other sub-parameters", []string{"1:2;31", "0:1"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var st sgrState
			for _, params := range tt.sequences {
				st.apply(params)
			}
			if got := st.sequence(); got != tt.want {
				t.Errorf("after %q: %q, want %q", tt.sequences, got, tt.want)
			}
		})
	}
}

// Text that changes its color often costs as much to restore after a match
// as text that changes it once.
func TestPaintMatchesStaysLinear(t *testing.T) {
	text := strings.Repeat("\x1b[31mx\x1b[32mx", 2000)
	got := Bold.PaintMatches(text, regexp.MustCompile("x"))
	if len(got) > 20*len(text) {
		t.Errorf("painting %d bytes made %d", len(text), len(got))
	}
}
