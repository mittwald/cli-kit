package pick

import (
	"slices"
	"strings"
	"testing"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// A CursorRow background spans the whole width of the row under the cursor,
// survives the resets of the cells' own colors, and leaves the cells their
// usual colors.
func TestCursorRow(t *testing.T) {
	bg := theme.BgRGB(69, 56, 86)
	mark := theme.Join(theme.Bold, theme.Fg256(255))
	author := theme.Fg256(218)
	l := newList(Options{
		Items: []Item{
			{Cells: []string{"main", "alice"}, CellStyles: []theme.Style{"", author}},
			{Cells: []string{"dev", "bob"}},
		},
		CursorRow:  bg,
		CursorMark: mark,
	}, 0, true)
	row := l.renderRow(0, 40)
	if !strings.HasPrefix(row, bg.Sequence()) || !strings.HasSuffix(row, theme.Reset) {
		t.Fatalf("row = %q, want it opened with the background and closed with a reset", row)
	}
	if got := term.VisibleWidth(row); got != 40 {
		t.Errorf("row is %d cells wide, want the whole width", got)
	}
	for _, want := range []string{mark.Paint(cursorMarker) + bg.Sequence(), th.Subject.Paint("main") + bg.Sequence(), author.Paint("alice") + bg.Sequence()} {
		if !strings.Contains(row, want) {
			t.Errorf("row = %q, want %q", row, want)
		}
	}
	if strings.Contains(row, th.Selected.Sequence()) || strings.Contains(row, theme.Join(theme.Bold, author).Sequence()) {
		t.Errorf("row = %q, want no Selected color and no bold cell", row)
	}
	if other := l.renderRow(1, 40); strings.Contains(other, bg.Sequence()) || strings.Contains(other, mark.Sequence()) {
		t.Errorf("row off the cursor = %q, want no highlight", other)
	}

	plain := newList(Options{Items: []Item{{Cells: []string{"main"}}}, CursorRow: bg, CursorMark: mark}, 0, false)
	if row := plain.renderRow(0, 40); strings.Contains(row, "\x1b") || row != cursorMarker+"main" {
		t.Errorf("row without color = %q, want it plain and unpadded", row)
	}
}

// Without CursorRow and CursorMark the row under the cursor looks as before.
func TestCursorRowDefault(t *testing.T) {
	l := newList(Options{Items: []Item{{Cells: []string{"main"}}}}, 0, true)
	if row := l.renderRow(0, 40); row != th.Selected.Paint(cursorMarker)+th.Selected.Paint("main") {
		t.Errorf("row = %q", row)
	}
}

func TestTouchesBackground(t *testing.T) {
	for params, want := range map[string]bool{
		"": true, "0": true, "1;0": true, "41": true, "49": true, "101": true,
		"48;5;1": true, "48:2::1:2:3": true, "1": false, "38;5;41": false,
		"38;2;40;41;42": false, "1;38;5;255": false, "38:5:41": false, "31;42": true,
	} {
		if got := touchesBackground(params); got != want {
			t.Errorf("touchesBackground(%q) = %v, want %v", params, got, want)
		}
	}
}

func TestWithBackground(t *testing.T) {
	bg := theme.Style("48;5;1")
	for in, want := range map[string]string{
		"x":                        "\x1b[48;5;1mx\x1b[0m",
		"\x1b[1mx\x1b[0my":         "\x1b[48;5;1m\x1b[1mx\x1b[0m\x1b[48;5;1my\x1b[0m",
		"\x1b[mx\x1b[0;31my\x1b[2": "\x1b[48;5;1m\x1b[m\x1b[48;5;1mx\x1b[0;31m\x1b[48;5;1my\x1b[2\x1b[0m",
		"\x1b[41mx\x1b[49my":       "\x1b[48;5;1m\x1b[41m\x1b[48;5;1mx\x1b[49m\x1b[48;5;1my\x1b[0m",
	} {
		if got := withBackground(in, bg); got != want {
			t.Errorf("withBackground(%q) = %q, want %q", in, got, want)
		}
	}
}

// Compact drops the blank lines around the rows and nothing else.
func TestCompact(t *testing.T) {
	items := []Item{{Cells: []string{"a"}}, {Cells: []string{"b"}}}
	for _, compact := range []bool{false, true} {
		l := newList(Options{Title: "Pick", Items: items, Compact: compact}, 0, false)
		l.resize(0, 24)
		lines := l.render(80)
		blank := slices.Contains(lines, "")
		if blank == compact {
			t.Errorf("Compact %v: lines %q", compact, lines)
		}
		if want := 5; compact && len(lines) != want {
			t.Errorf("compact list has %d lines, want %d: %q", len(lines), want, lines)
		}
	}
}

// Keys rewords the default key line and can move the view switch to its end.
func TestKeyLabels(t *testing.T) {
	views := []View{{Name: "open"}, {Name: "all"}}
	items := []Item{{Cells: []string{"a"}}}
	keys := KeyLabels{Cancel: "Esc abort", View: "Tab/←/→ scope", ViewShort: "Tab scope", ViewLast: true}
	l := newList(Options{Items: items, Views: views, Keys: keys}, 0, false)
	long := l.hint(200)
	if !strings.HasSuffix(long, "Esc abort · Tab/←/→ scope") || strings.Contains(long, "Tab view") {
		t.Errorf("hint = %q", long)
	}
	if short := l.hint(70); !strings.HasSuffix(short, "Esc abort · Tab scope") {
		t.Errorf("short hint = %q", short)
	}
	nested := newList(Options{Items: items, Nested: true, Keys: KeyLabels{Back: "Esc up"}}, 0, false)
	if hint := nested.hint(200); !strings.HasSuffix(hint, "Esc up") {
		t.Errorf("nested hint = %q", hint)
	}
	plain := newList(Options{Items: items, Views: views}, 0, false)
	if hint := plain.hint(200); !strings.Contains(hint, "PgUp/PgDn page · Tab view · ") || !strings.HasSuffix(hint, "Esc cancel") {
		t.Errorf("default hint = %q", hint)
	}
	hostile := newList(Options{Items: items, Keys: KeyLabels{Cancel: "Esc \x1b[31mabort\n"}}, 0, false)
	if hint := hostile.hint(200); strings.ContainsAny(hint, "\x1b\n") {
		t.Errorf("hint = %q, want the labels made plain", hint)
	}
}

// ActiveView paints the name of the view in force in the position line.
func TestActiveView(t *testing.T) {
	active := theme.Join(theme.Bold, theme.Fg256(189))
	views := []View{{Name: "open"}, {Name: "all"}}
	l := newList(Options{Items: []Item{{Cells: []string{"a"}}}, Views: views, ActiveView: active}, 0, true)
	line := l.renderPosition(80)
	if !strings.Contains(line, active.Paint("[open]")) || !strings.Contains(line, th.Muted.Paint(" · all")) {
		t.Errorf("position = %q", line)
	}
	plain := newList(Options{Items: []Item{{Cells: []string{"a"}}}, Views: views}, 0, true)
	if line := plain.renderPosition(80); !strings.Contains(line, "[open]") || strings.Contains(line, active.Sequence()) {
		t.Errorf("default position = %q", line)
	}
}

// TitleStyle, HintStyle and EmptyStyle replace the theme's styles for the
// title line, the key line with the views not in force, and the line that
// stands in for missing rows; a failed load keeps its warning.
func TestListStyles(t *testing.T) {
	title := theme.Join(theme.Bold, theme.Fg256(255))
	hint := theme.Fg256(242)
	empty := theme.Fg256(221)
	views := []View{{Name: "open"}, {Name: "all"}}
	l := newList(Options{
		Title: "Pick", Items: []Item{{Cells: []string{"a"}}}, Views: views,
		TitleStyle: title, HintStyle: hint, EmptyStyle: empty,
	}, 0, true)
	l.resize(0, 24)
	lines := l.render(80)
	if !strings.HasPrefix(lines[0], title.Sequence()) {
		t.Errorf("title = %q, want it in %q", lines[0], title)
	}
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, hint.Sequence()) {
		t.Errorf("key line = %q, want it in %q", last, hint)
	}
	if position := l.renderPosition(80); !strings.Contains(position, hint.Paint("all")) {
		t.Errorf("position = %q, want the other view in %q", position, hint)
	}

	l.setFilter([]rune("zzz"))
	if text, style := l.emptyLine(); text != "  no match" || style != empty {
		t.Errorf("empty line = %q in %q, want no match in %q", text, style, empty)
	}
	l.failures[l.view] = "could not load"
	if _, style := l.emptyLine(); style != th.Warning {
		t.Errorf("failed load in %q, want the warning style", style)
	}
}

// A column empty on every row takes a single space, like a one-cell marker
// column, so it does not widen the gap to the next column.
func TestEmptyColumnGap(t *testing.T) {
	l := newList(Options{Items: []Item{
		{Cells: []string{"", "feat/x"}},
		{Cells: []string{"", "main"}},
	}}, 0, false)
	l.resize(0, 24)
	if row := l.renderRowWith(0, 80, l.fittedWidths(80)); !strings.HasPrefix(row, "❯  feat/x") {
		t.Errorf("row = %q, want one space for the empty column", row)
	}
}
