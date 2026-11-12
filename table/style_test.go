package table

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

func TestFancyFrame(t *testing.T) {
	want := " ╒══════╤══════════════╕\n" +
		" │ ID   │ NAME         │\n" +
		" ╞══════╪══════════════╡\n" +
		" │ 4711 │ Ada Lovelace │\n" +
		" ├──────┼──────────────┤\n" +
		" │ 42   │ Alan         │\n" +
		" ╘══════╧══════════════╛\n"
	if got := render(t, sample(WithStyle(Fancy), WithIndent(" "))); got != want {
		t.Fatalf("fancy table mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestASCIIFrame(t *testing.T) {
	want := "+------+--------------+\n" +
		"| ID   | NAME         |\n" +
		"+------+--------------+\n" +
		"| 4711 | Ada Lovelace |\n" +
		"+------+--------------+\n" +
		"| 42   | Alan         |\n" +
		"+------+--------------+\n"
	if got := render(t, sample(WithStyle(ASCII))); got != want {
		t.Fatalf("ascii table mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// The markdown frame carries a header rule and nothing else, and starts at
// column 0 even with an indent, so the output pastes into a document unchanged.
func TestMarkdownFrameOmitsOuterRulesAndIndent(t *testing.T) {
	want := "| ID   | NAME         |\n" +
		"|------|--------------|\n" +
		"| 4711 | Ada Lovelace |\n" +
		"| 42   | Alan         |\n"
	if got := render(t, sample(WithStyle(Markdown), WithIndent(" "))); got != want {
		t.Fatalf("markdown table mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
	if _, ok := LookupStyle("markdown"); ok {
		t.Fatal("markdown is an output format, not a drawn style")
	}
}

// Every framed style must produce a rectangle: the rules only line up when
// each line is exactly as wide as the table claims to be.
func TestFramedStylesKeepEveryLineAtTotalWidth(t *testing.T) {
	for _, name := range StyleNames() {
		style, _ := LookupStyle(name)
		if !style.Framed() {
			continue
		}
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			tbl := New(&out, []string{"Date", "Times", "Total"}, WithStyle(style), WithIndent(" "), WithRowGap(true))
			tbl.Append("Mon", "09:04 - 13:24\n14:00 - 17:30", "07:50")
			tbl.Append("Tue", "--:--", "00:00")
			got := render(t, tbl)
			width := tbl.TotalWidth()
			for _, line := range lines(got) {
				if w := term.VisibleWidth(line); w != width {
					t.Fatalf("line width %d, TotalWidth %d: %q", w, width, line)
				}
			}
		})
	}
}

func TestPlainTotalWidth(t *testing.T) {
	tbl := sample(WithIndent(" "))
	if got := tbl.TotalWidth(); got != 1+4+2+12 {
		t.Fatalf("TotalWidth = %d", got)
	}
}

// A multi-line cell must not push the closing rule out of column: every line of
// the row is padded to the column width.
func TestFramedRowKeepsMultiLineCellsFramed(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"Day", "Times"}, WithStyle(Light), UppercaseHeaders())
	tbl.Append("Mon", "09:04 - 13:24\r\n14:00 - 17:30")
	want := "┌─────┬───────────────┐\n" +
		"│ DAY │ TIMES         │\n" +
		"├─────┼───────────────┤\n" +
		"│ Mon │ 09:04 - 13:24 │\n" +
		"│     │ 14:00 - 17:30 │\n" +
		"└─────┴───────────────┘\n"
	if got := render(t, tbl); got != want {
		t.Fatalf("multi-line row mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// The frame gets the border tone; cell content keeps the color it arrived with.
func TestFramedTableColorsRulesAndHeaderOnly(t *testing.T) {
	th := theme.Default
	cellTone := theme.Fg256(80)
	tbl := New(&bytes.Buffer{}, []string{"Name"}, WithStyle(Double), WithColor(true), WithTheme(th))
	tbl.Append(cellTone.Paint("Ada"))
	got := render(t, tbl)
	if !strings.Contains(got, th.Border.Sequence()+"╔") {
		t.Fatalf("top rule is not painted in the border tone:\n%q", got)
	}
	if !strings.Contains(got, th.Header.Sequence()+"Name") {
		t.Fatalf("header is not painted in the header tone:\n%q", got)
	}
	if !strings.Contains(got, cellTone.Sequence()+"Ada") {
		t.Fatalf("cell lost its own tone:\n%q", got)
	}
	for _, line := range lines(got) {
		if term.VisibleWidth(line) != tbl.TotalWidth() {
			t.Fatalf("colored line %q measures %d, want %d", line, term.VisibleWidth(line), tbl.TotalWidth())
		}
	}
}

// A markdown row is one line, so a multi-line cell folds into the same cell.
func TestMarkdownFoldsMultiLineCells(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"Day", "Times"}, WithStyle(Markdown), UppercaseHeaders())
	tbl.Append("Mon", "09:04 - 13:24\n14:00 - 17:30")
	tbl.Append("Tue", "08:12 - --:--")
	want := "| DAY | TIMES                          |\n" +
		"|-----|--------------------------------|\n" +
		"| Mon | 09:04 - 13:24<br>14:00 - 17:30 |\n" +
		"| Tue | 08:12 - --:--                  |\n"
	if got := render(t, tbl); got != want {
		t.Fatalf("markdown fold mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// Markdown is verbatim: never painted, even when color is forced, and escape
// sequences a caller put into a cell are removed.
func TestMarkdownIsNeverPainted(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"Name"}, WithStyle(Markdown), WithColor(true))
	tbl.Append(theme.Bold.Paint("Ada"))
	if got := render(t, tbl); strings.Contains(got, "\x1b") {
		t.Fatalf("markdown output carries escapes: %q", got)
	}
}

// A value carrying a pipe must not open a column of its own: markdown splits a
// row on unescaped pipes, and cell values are free text.
func TestMarkdownEscapesPipesInCells(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"Name", "Event"}, WithStyle(Markdown), UppercaseHeaders())
	tbl.Append("Alice | Bob", `back\slash`)
	tbl.Append("Carol", `already \| escaped`)
	got := render(t, tbl)
	want := "| NAME         | EVENT                |\n" +
		"|--------------|----------------------|\n" +
		"| Alice \\| Bob | back\\\\slash          |\n" +
		"| Carol        | already \\\\\\| escaped |\n"
	if got != want {
		t.Fatalf("pipe escaping mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
	for _, line := range lines(got) {
		if columns := markdownColumns(line); columns != 2 {
			t.Fatalf("line splits into %d columns, want 2: %q", columns, line)
		}
	}
}

// markdownColumns counts the cells a markdown reader would find in a row: the
// pipes that are not escaped, minus the two that cap the line.
func markdownColumns(line string) int {
	separators := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++ // whatever follows is escaped, including a pipe
		case '|':
			separators++
		}
	}
	return separators - 1
}

// The drawn styles print their cells as they are: an ascii table's pipes are
// its own rules, and a backslash in a name is not markup there.
func TestDrawnStylesDoNotEscapeCells(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"Name"}, WithStyle(ASCII))
	tbl.Append(`Alice | B\b`)
	if got := render(t, tbl); !strings.Contains(got, `| Alice | B\b |`) {
		t.Fatalf("ascii table altered its cell:\n%s", got)
	}
}

func TestLookupAndParseStyle(t *testing.T) {
	if s, ok := LookupStyle(" FANCY "); !ok || s.Name != "fancy" {
		t.Fatalf("LookupStyle(FANCY) = %q, %v", s.Name, ok)
	}
	if got := strings.Join(StyleNames(), ","); got != "plain,light,rounded,heavy,double,fancy,ascii" {
		t.Fatalf("StyleNames = %s", got)
	}
	_, err := ParseStyle("grid")
	if err == nil || !strings.Contains(err.Error(), `"grid"`) || !strings.Contains(err.Error(), "ascii") {
		t.Fatalf("ParseStyle error should name the value and the accepted styles: %v", err)
	}
	if s, err := ParseStyle("rounded"); err != nil || !s.Framed() {
		t.Fatalf("ParseStyle(rounded) = %+v, %v", s, err)
	}
}

func TestZeroStyleIsPlain(t *testing.T) {
	a := render(t, sample(WithStyle(Style{})))
	b := render(t, sample(WithStyle(Plain)))
	if a != b {
		t.Fatalf("zero style differs from plain:\n%q\n%q", a, b)
	}
}

func TestCustomGutter(t *testing.T) {
	got := render(t, sample(WithStyle(Style{Name: "tight", Gutter: " "})))
	if !strings.HasPrefix(got, "ID   NAME\n4711 Ada Lovelace\n") {
		t.Fatalf("custom gutter not applied: %q", got)
	}
}

// Markdown is pasted or printed as it stands: its cells become plain text,
// and a carriage return a line break, so neither half of the cell hides the
// other.
func TestMarkdownCellsArePlainText(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"Name", "Note", "Pay", "Raw"}, WithStyle(Markdown))
	tbl.Append(untrustedRow...)
	want := "| Name         | Note | Pay                               | Raw         |\n" +
		"|--------------|------|-----------------------------------|-------------|\n" +
		"| Ada Lovelace | ok   | pay the attacker<br>pay your bank | a       bc\ufffd |\n"
	if got := render(t, tbl); got != want {
		t.Fatalf("markdown mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// A drawn table shows its cells as they are unless asked otherwise: it
// cannot tell a caller's colors and links from the data's.
func TestDrawnCellsAreShownAsGiven(t *testing.T) {
	tbl := New(&bytes.Buffer{}, nil, WithColor(false))
	tbl.Append(theme.Bold.Paint("Ada"), "\x1b]8;;https://example.com\x1b\\site\x1b]8;;\x1b\\")
	got := render(t, tbl)
	if !strings.Contains(got, theme.Bold.Paint("Ada")) || !strings.Contains(got, "\x1b]8;;https://example.com") {
		t.Fatalf("drawn cells changed: %q", got)
	}
}

// SanitizeCells keeps colors and takes out everything else; every line of a
// multi-line cell carries its own colors and closes them.
func TestSanitizeCells(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"Name\x1b]0;title\x07", "Note"}, WithStyle(ASCII), WithColor(false), SanitizeCells())
	tbl.Append("\x1b[31mred\nstill red\x1b[0m", "\x1b]8;;https://evil.example\x1b\\click\x1b]8;;\x1b\\\u009b2J\x07")
	want := "+-----------+-------+\n" +
		"| Name      | Note  |\n" +
		"+-----------+-------+\n" +
		"| \x1b[31mred\x1b[0m       | click |\n" +
		"| \x1b[31mstill red\x1b[0m |       |\n" +
		"+-----------+-------+\n"
	if got := render(t, tbl); got != want {
		t.Fatalf("sanitized table mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// PlainCells takes out colors too, but not the header color the table paints.
func TestPlainCells(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"\x1b[31mName"}, WithColor(true), PlainCells())
	tbl.Append("\x1b[1mAda\x1b[0m\x1b]52;c;Zm9v\x07")
	want := theme.Active().Header.Paint("Name") + "\nAda\n"
	if got := render(t, tbl); got != want {
		t.Fatalf("plain table mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

// Of SanitizeCells and PlainCells, the one given last wins.
func TestLastCellOptionWins(t *testing.T) {
	tbl := New(&bytes.Buffer{}, nil, WithColor(false), PlainCells(), SanitizeCells())
	tbl.Append("\x1b[31mred\x1b[0m")
	if got, want := render(t, tbl), "\x1b[31mred\x1b[0m\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A pipe table is one only with its header rule, so a markdown table without
// headers gets an empty header row above it.
func TestMarkdownWithoutHeaders(t *testing.T) {
	tbl := New(&bytes.Buffer{}, nil, WithStyle(Markdown), WithAlignment(AlignLeft, AlignRight))
	tbl.Append("api", "3")
	tbl.Append("worker", "12")
	want := "|        |    |\n" +
		"|--------|---:|\n" +
		"| api    |  3 |\n" +
		"| worker | 12 |\n"
	if got := render(t, tbl); got != want {
		t.Fatalf("markdown mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}

	// Without rows there is still nothing to write, and the drawn frames keep
	// leaving the header out.
	if got := render(t, New(&bytes.Buffer{}, nil, WithStyle(Markdown))); got != "" {
		t.Fatalf("an empty markdown table wrote %q", got)
	}
	tbl = New(&bytes.Buffer{}, nil, WithStyle(ASCII))
	tbl.Append("a", "b")
	if got, want := render(t, tbl), "+---+---+\n| a | b |\n+---+---+\n"; got != want {
		t.Fatalf("ascii table = %q, want %q", got, want)
	}
}

// Characters whose width Unicode leaves ambiguous, the rules of the frames
// among them, are one cell wide whatever the locale. go-runewidth's default
// counts them two cells wide under a Chinese, Japanese or Korean locale; the
// test switches that on, as LC_ALL=ja_JP.UTF-8 or RUNEWIDTH_EASTASIAN=1 do
// at start-up, so it guards the frames on every machine.
func TestAmbiguousWidthCharactersKeepTheFrame(t *testing.T) {
	eastAsian := runewidth.DefaultCondition.EastAsianWidth
	runewidth.DefaultCondition.EastAsianWidth = true
	t.Cleanup(func() { runewidth.DefaultCondition.EastAsianWidth = eastAsian })

	styles := []Style{Markdown}
	for _, name := range StyleNames() {
		style, _ := LookupStyle(name)
		styles = append(styles, style)
	}
	for _, style := range styles {
		t.Run(style.Name, func(t *testing.T) {
			tbl := New(&bytes.Buffer{}, []string{"Größe", "Unit", "Note"}, WithStyle(style), WithColor(false), WithAlignment(AlignLeft, AlignRight))
			tbl.Append("α-Test", "21 °C", "§ 1 → § 2")
			tbl.Append("βγδ", "±5 %", "too long…")
			tbl.Append("•", "×", "“quoted”\n— dash")
			got := render(t, tbl)
			width := tbl.TotalWidth()
			if !style.Framed() {
				// The unframed layout does not pad its last column.
				if want := "α-Test  21 °C  § 1 → § 2"; lines(got)[1] != want {
					t.Fatalf("row = %q, want %q", lines(got)[1], want)
				}
				return
			}
			for _, line := range lines(got) {
				// Every character here is one cell wide, so a line has as
				// many characters as cells.
				if n := utf8.RuneCountInString(line); n != width {
					t.Errorf("line holds %d characters, TotalWidth %d: %q", n, width, line)
				}
				if w := term.VisibleWidth(line); w != width {
					t.Errorf("line width %d, TotalWidth %d: %q", w, width, line)
				}
			}
		})
	}
}

// A rule is as wide as the rows it separates whatever its fill: one wider
// than a cell is repeated as often as it fits and cut for the rest.
func TestRuleFillWiderThanOneCell(t *testing.T) {
	for _, tc := range []struct {
		fill string
		want string // the rule over the columns "abc" and "de"
	}{
		{"-", "+-----+----+"},
		{"=-", "+=-=-=+=-=-+"},
		{"·:.", "+·:.·:+·:.·+"},
		// A wide character that does not fit the last cell leaves it empty.
		{"\u4e00", "+\u4e00\u4e00 +\u4e00\u4e00+"},
		{"\x1b[2m-\x1b[0m", "+" + strings.Repeat("\x1b[2m-\x1b[0m", 5) + "+" + strings.Repeat("\x1b[2m-\x1b[0m", 4) + "+"},
		// A cut-off pattern leaves no escape sequence open.
		{"\x1b[2m=-\x1b[0m", "+" + strings.Repeat("\x1b[2m=-\x1b[0m", 2) + "=+" + strings.Repeat("\x1b[2m=-\x1b[0m", 2) + "+"},
	} {
		style := ASCII
		style.Top.Fill = tc.fill
		tbl := New(&bytes.Buffer{}, nil, WithStyle(style), WithColor(false))
		tbl.Append("abc", "de")
		got := lines(render(t, tbl))
		if got[0] != tc.want {
			t.Errorf("fill %q: rule = %q, want %q", tc.fill, got[0], tc.want)
		}
		if w, row := term.VisibleWidth(got[0]), term.VisibleWidth(got[1]); w != row || w != tbl.TotalWidth() {
			t.Errorf("fill %q: rule is %d cells wide, the row %d, TotalWidth %d", tc.fill, w, row, tbl.TotalWidth())
		}
	}
}
