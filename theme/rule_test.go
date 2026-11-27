package theme

import (
	"regexp"
	"testing"

	"github.com/mittwald/cli-kit/term"
)

func TestRulesPaint(t *testing.T) {
	link := term.Hyperlink("https://example.com", "api")
	tests := []struct {
		name  string
		rules Rules
		text  string
		want  string
	}{
		{"no rules", nil, "api", "api"},
		{"whole text", Rules{{Style: "34"}}, "api", "\x1b[34mapi\x1b[0m"},
		{"a match", Rules{{Pattern: regexp.MustCompile("p"), Style: "31"}}, "api", "a\x1b[31mp\x1b[0mi"},
		{"whole text then a match", Rules{{Style: "34"}, {Pattern: regexp.MustCompile("p"), Style: "31"}}, "api",
			"\x1b[34ma\x1b[0m\x1b[31mp\x1b[0m\x1b[34mi\x1b[0m"},
		{"a later whole text paints anew", Rules{{Style: "34"}, {Style: "32"}}, "api", "\x1b[32mapi\x1b[0m"},
		{"the zero style", Rules{{Style: ""}}, "\x1b[34mapi\x1b[0m", "\x1b[34mapi\x1b[0m"},
		{"a link stays", Rules{{Style: "32"}}, "\x1b[34m" + link + "\x1b[0m", "\x1b[32m" + link + "\x1b[0m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rules.Paint(tt.text); got != tt.want {
				t.Errorf("Paint(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestHasColor(t *testing.T) {
	for text, want := range map[string]bool{
		"":                          false,
		"plain":                     false,
		"\x1b[0mreset only\x1b[m":   false,
		"\x1b[32mgreen\x1b[0m":      true,
		"a \x1b[1mbold\x1b[0m word": true,
		term.Hyperlink("https://example.com", "x"): false,
		"\x1b[2Kcursor": false,
	} {
		if got := HasColor(text); got != want {
			t.Errorf("HasColor(%q) = %v, want %v", text, got, want)
		}
	}
}
