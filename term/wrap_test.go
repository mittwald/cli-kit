package term

import "testing"

func TestWrap(t *testing.T) {
	tests := map[string]struct {
		in     string
		width  int
		indent string
		want   string
	}{
		"fits":                {"short line", 20, "  ", "short line"},
		"no width":            {"a long line that is not broken", 0, "  ", "a long line that is not broken"},
		"no width indents":    {"one\ntwo", 0, "  ", "one\n  two"},
		"hanging indent":      {"Dry run: would tag 8.4.25 and push it, then wait for its pipeline.", 40, "         ", "Dry run: would tag 8.4.25 and push it,\n         then wait for its pipeline."},
		"breaks kept":         {"first line\nsecond line is longer than that", 20, "  ", "first line\n  second line is\n  longer than that"},
		"leading spaces kept": {"    indented text that wraps", 16, "", "    indented\ntext that wraps"},
		"inner spaces kept":   {"a  b  c d e f", 9, "", "a  b  c d\ne f"},
		"spaces at the break": {"aaaa     bbbb", 6, "", "aaaa\nbbbb"},
		"trailing spaces go":  {"aaaa bbbb   ", 6, "", "aaaa\nbbbb"},
		"long word whole":     {"see https://example.com/a/very/long/path now", 12, "", "see\nhttps://example.com/a/very/long/path\nnow"},
		"long first word":     {"https://example.com/long next", 10, "", "https://example.com/long\nnext"},
		"colors take no room": {"\x1b[1mbold\x1b[0m words \x1b[31mred\x1b[0m", 10, "", "\x1b[1mbold\x1b[0m words\n\x1b[31mred\x1b[0m"},
		"wide runes":          {"日本語 日本語", 8, "", "日本語\n日本語"},
		"space in escape":     {"\x1b[1 qcursor style kept whole", 12, "", "\x1b[1 qcursor style\nkept whole"},
		"only spaces":         {"          ", 4, "", "          "},
		"tab counts to stop":  {"a\tb c", 9, "", "a\tb\nc"},
		"tab fits":            {"a\tb c", 11, "", "a\tb c"},
		"tab after a word":    {"abcdef \tx y", 10, "", "abcdef \tx\ny"},
		"tab on the new line": {"abcdef g\th i", 12, "  ", "abcdef\n  g\th i"},
		"tab in the indent":   {"aaaa bbbb cccc", 14, "\t", "aaaa bbbb cccc"},
		"tab indents":         {"aaaa bbbb cccc dddd", 14, "\t", "aaaa bbbb cccc\n\tdddd"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := Wrap(tt.in, tt.width, tt.indent); got != tt.want {
				t.Errorf("Wrap(%q, %d, %q) =\n%q\nwant\n%q", tt.in, tt.width, tt.indent, got, tt.want)
			}
		})
	}
}

// An escape sequence standing alone, such as a trailing reset, stays on the
// line it follows instead of breaking it.
func TestWrapKeepsEscapeOnlyTokensOnTheLine(t *testing.T) {
	if got, want := Wrap("\x1b[31maaaa bbbb \x1b[0m", 4, ""), "\x1b[31maaaa\nbbbb\x1b[0m"; got != want {
		t.Fatalf("Wrap = %q, want %q", got, want)
	}
	// The spaces before it still separate the next visible word.
	if got, want := Wrap("aaaa \x1b[1m bbbb cccc", 11, ""), "aaaa\x1b[1m  bbbb\ncccc"; got != want {
		t.Fatalf("Wrap = %q, want %q", got, want)
	}
}

// A style sequence opening the line does not take the first word's place:
// no empty first line.
func TestWrapLeadingEscapeKeepsTheFirstWord(t *testing.T) {
	plain := Wrap(" abcdef end", 4, "")
	styled := Wrap("\x1b[31m abcdef end", 4, "")
	if StripANSI(styled) != plain {
		t.Fatalf("styled %q, plain %q", styled, plain)
	}
}
