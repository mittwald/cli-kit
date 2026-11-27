package table

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

func TestColumnStyles(t *testing.T) {
	link := term.Hyperlink("https://example.com/4711", "4711")
	styled := func(opts ...Option) string {
		tbl := New(&bytes.Buffer{}, []string{"ID", "State", "Age"},
			append([]Option{WithTheme(theme.Theme{}), WithColumnStyles(
				ColumnStyle{Style: theme.Bold},
				ColumnStyle{Style: "34", Rules: theme.Rules{{Pattern: regexp.MustCompile("failed"), Style: "31"}}},
				ColumnStyle{Style: "35"},
			)}, opts...)...)
		tbl.Append(link, "build failed\nretry", theme.Style("32").Paint("now"))
		tbl.Append("42", "", "5m")
		return render(t, tbl)
	}
	got := styled(WithColor(true))
	for _, want := range []string{
		// The style paints a cell, its link kept.
		theme.Bold.Paint(link),
		// Rules paint over the style, every line on its own.
		"\x1b[34mbuild \x1b[0m\x1b[31mfailed\x1b[0m",
		"\x1b[34mretry\x1b[0m",
		// A cell painted by the program keeps its colors.
		theme.Style("32").Paint("now"),
		theme.Style("35").Paint("5m"),
		theme.Bold.Paint("42"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("table misses %q:\n%q", want, got)
		}
	}
	if strings.Contains(got, "\x1b[35m\x1b[32mnow") || strings.Count(got, "\x1b[34m") != 3 {
		t.Errorf("a painted or empty cell was painted:\n%q", got)
	}
	// Columns line up as without the styles.
	plain := styled(WithColor(false))
	if term.StripANSI(got) != term.StripANSI(plain) {
		t.Errorf("painted table:\n%s\nplain table:\n%s", term.StripANSI(got), plain)
	}
	if strings.Contains(plain, "\x1b[1m") || strings.Contains(plain, "\x1b[34m") {
		t.Errorf("an unpainted table took the column styles:\n%q", plain)
	}
	// Markdown is never painted.
	if md := styled(WithColor(true), WithStyle(Markdown)); strings.Contains(md, "\x1b[") {
		t.Errorf("markdown was painted:\n%q", md)
	}
}

// Plain cells take the colors of the program out of a cell, not those of
// the column styles.
func TestColumnStylesWithPlainCells(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"Name"}, WithColor(true), WithTheme(theme.Theme{}), PlainCells(),
		WithColumnStyles(ColumnStyle{Style: "36"}))
	tbl.Append("\x1b[31mred\x1b[0m")
	if got := render(t, tbl); !strings.Contains(got, theme.Style("36").Paint("red")) {
		t.Errorf("got %q", got)
	}
}

func TestNewPalette(t *testing.T) {
	defaults := NewPalette(nil, "")
	if defaults.Header != "38;5;248;1" || defaults.Age != "38;5;245" || defaults.FreshAge != "38;5;40;1" ||
		len(defaults.Columns) != 6 || defaults.Columns[0] != "38;5;38" {
		t.Errorf("default palette = %+v", defaults)
	}
	env := map[string]string{
		"EXAMPLE_COLOR_TABLE_HEADER":   "red:bold",
		"EXAMPLE_COLOR_TABLE_COLUMN_2": "blue",
		"EXAMPLE_COLOR_TABLE_COLUMN_3": "nonsense", // ignored
		"EXAMPLE_TEXT_COLOR_DURATION":  "gray",
		"EXAMPLE_TEXT_DURATION_FRESH":  "green",
	}
	changed := NewPalette(func(name string) string { return env[name] }, "EXAMPLE")
	if changed.Header != "31;1" || changed.Age != "37" || changed.FreshAge != "32" ||
		changed.Columns[0] != defaults.Columns[0] || changed.Columns[1] != "34" || changed.Columns[2] != defaults.Columns[2] {
		t.Errorf("palette from the environment = %+v", changed)
	}
}
