package pick

import (
	"slices"
	"strings"
	"testing"

	"github.com/mittwald/cli-kit/theme"
)

// fade paints the "/" of text in Muted and the rest in base, the way a
// caller fades the separators of a branch name.
func fade(text string, base theme.Style) string {
	var b strings.Builder
	for i, part := range strings.Split(text, "/") {
		if i > 0 {
			b.WriteString(th.Muted.Paint("/"))
		}
		b.WriteString(base.Paint(part))
	}
	return b.String()
}

// PaintRowCell sees the row and the style the cell would be drawn in, takes
// precedence over PaintCell, and is not asked about receded rows.
func TestPaintRowCellSeesTheRow(t *testing.T) {
	author := theme.Fg256(218)
	type call struct {
		row, column int
		base        theme.Style
		selected    bool
	}
	var calls []call
	l := newList(Options{
		Items: []Item{
			{Cells: []string{"main", "feat/x"}, CellStyles: []theme.Style{"", author}},
			{Cells: []string{"dev", "fix/y"}},
			{Cells: []string{"old", "chore/z"}, Muted: true},
		},
		CellStyles: []theme.Style{"", th.Accent},
		PaintCell: func(int, string, bool) (string, bool) {
			t.Error("PaintCell called although PaintRowCell is set")
			return "", false
		},
		PaintRowCell: func(row, column int, text string, base theme.Style, selected bool) (string, bool) {
			calls = append(calls, call{row, column, base, selected})
			if column != 1 {
				return "", false
			}
			return fade(text, base), true
		},
	}, 0, true)
	first := l.renderRow(0, 80)
	if want := fade("feat/x", theme.Join(theme.Bold, author)); !strings.Contains(first, want) {
		t.Errorf("row = %q, want the row's own style made bold, painted by the hook (%q)", first, want)
	}
	if !strings.Contains(first, th.Selected.Paint("main")) {
		t.Errorf("row = %q, want a declined column painted the default way", first)
	}
	second := l.renderRow(1, 80)
	if want := fade("fix/y", th.Accent); !strings.Contains(second, want) {
		t.Errorf("row = %q, want the column style painted by the hook (%q)", second, want)
	}
	want := []call{
		{0, 0, th.Selected, true}, {0, 1, theme.Join(theme.Bold, author), true},
		{1, 0, th.Subject, false}, {1, 1, th.Accent, false},
	}
	if !slices.Equal(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}
	calls = nil
	_ = l.renderRow(2, 80)
	if len(calls) != 0 {
		t.Errorf("PaintRowCell called for a muted row: %v", calls)
	}
}

// The row a typed answer stands on is no row of the caller's.
func TestPaintRowCellTypedRow(t *testing.T) {
	var rows []int
	l := newList(Options{
		Items: []Item{{Cells: []string{"main"}}},
		Typed: func(string) bool { return true },
		PaintRowCell: func(row, _ int, text string, _ theme.Style, _ bool) (string, bool) {
			rows = append(rows, row)
			return text, true
		},
	}, 0, true)
	typeText(l, "new")
	l.render(80)
	if !slices.Contains(rows, -1) {
		t.Errorf("rows = %v, want -1 for the typed row", rows)
	}
}

// What a painter returns is drawn only when it adds colors and nothing else
// to the text it was given; otherwise the default drawing stands.
func TestPainterOutputIsChecked(t *testing.T) {
	for name, painted := range map[string]string{
		"other text":     "\x1b[1mmaim\x1b[0m",
		"more text":      "main!",
		"cursor move":    "\x1b[2Amain",
		"hyperlink":      "\x1b]8;;https://example.com\x1b\\main\x1b]8;;\x1b\\",
		"clipboard":      "\x1b]52;c;aGk=\x07main",
		"terminal reset": "\x1bcmain",
		"control":        "ma\rin",
		"unfinished sgr": "\x1b[1main",
	} {
		t.Run(name, func(t *testing.T) {
			l := newList(Options{
				Title: "t",
				Items: []Item{{Cells: []string{"main", "x"}, Detail: "main", Details: nil}},
				PaintRowCell: func(_, column int, _ string, _ theme.Style, _ bool) (string, bool) {
					return painted, column == 0
				},
				PaintDetail: func(string, theme.Style) string { return painted },
			}, 24, true)
			lines := l.render(80)
			if !strings.Contains(lines[0], th.Detail.Paint("main")) {
				t.Errorf("title = %q, want the detail in its style", lines[0])
			}
			for _, line := range lines {
				if strings.Contains(line, painted) {
					t.Errorf("line %q carries the painter's output %q", line, painted)
				}
			}
		})
	}
}

func TestPaintedFrom(t *testing.T) {
	for _, tc := range []struct {
		painted, text string
		want          bool
	}{
		{"main", "main", true},
		{"\x1b[1;38;5;141mma\x1b[0min\x1b[m", "main", true},
		{"\x1b[38:2::1:2:3mx\x1b[0m", "x", true},
		{"", "", true},
		{"\x1b[0m", "", true},
		{"mai", "main", false},
		{"mainn", "main", false},
		{"\x1b[1Kmain", "main", false},
		{"\x1b(Bmain", "main", false},
	} {
		if got := paintedFrom(tc.painted, tc.text); got != tc.want {
			t.Errorf("paintedFrom(%q, %q) = %v, want %v", tc.painted, tc.text, got, tc.want)
		}
	}
}

// PaintDetail paints the detail in the title line and the ends of an open
// range; Field.Paint paints a field of Details. Neither is called without
// color.
func TestPaintDetailAndFields(t *testing.T) {
	hash := theme.Fg256(71)
	items := []Item{
		{Cells: []string{"a"}, Detail: "origin/feat/a"},
		{Cells: []string{"b"}, Detail: "origin/main"},
		{Cells: []string{"c"}, Details: []Field{
			{Text: "57e092c6", Style: hash},
			{Text: "src/cmd", Style: th.Value, Paint: fade},
		}},
	}
	opts := Options{Title: "Base:", Items: items, Range: true, PaintDetail: fade}
	l := newList(opts, 24, true)
	if title := l.render(120)[0]; !strings.HasSuffix(title, " "+fade("origin/feat/a", th.Detail)) {
		t.Errorf("title = %q, want the detail painted by PaintDetail", title)
	}
	l.apply(key{kind: keyDown})
	l.apply(key{kind: keyDown})
	title := l.render(120)[0]
	if !strings.Contains(title, hash.Paint("57e092c6")) || !strings.Contains(title, fade("src/cmd", th.Value)) {
		t.Errorf("title = %q, want each field in its style, the painted one painted", title)
	}
	if got, want := stripSGR(title), "Base: · 57e092c6 · src/cmd"; got != want {
		t.Errorf("title reads %q, want %q", got, want)
	}

	// An open range shows its two ends, each painted.
	ranged := newList(opts, 24, true)
	ranged.apply(key{kind: keyEnter})
	ranged.apply(key{kind: keyDown})
	title = ranged.render(120)[0]
	if !strings.Contains(title, fade("origin/main", th.Detail)) || !strings.Contains(title, fade("origin/feat/a", th.Detail)) {
		t.Errorf("title = %q, want both ends of the range painted", title)
	}

	calls := 0
	count := func(text string, _ theme.Style) string { calls++; return text }
	plain := newList(Options{Title: "t", Items: []Item{
		{Cells: []string{"a"}, Detail: "x/y"},
	}, PaintDetail: count}, 24, false)
	for _, line := range plain.render(80) {
		if strings.Contains(line, "\x1b") {
			t.Errorf("line %q carries an escape sequence without color", line)
		}
	}
	if calls != 0 {
		t.Errorf("PaintDetail called %d times without color", calls)
	}
}
