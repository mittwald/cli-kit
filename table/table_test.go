package table

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mittwald/cli-kit/format"
	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

func render(t *testing.T, tbl *Table) string {
	t.Helper()
	if err := tbl.Render(); err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	return tbl.w.(*bytes.Buffer).String()
}

func sample(opts ...Option) *Table {
	tbl := New(&bytes.Buffer{}, []string{"ID", "Name"}, append([]Option{UppercaseHeaders()}, opts...)...)
	tbl.Append("4711", "Ada Lovelace")
	tbl.Append("42", "Alan")
	return tbl
}

func lines(s string) []string { return strings.Split(strings.TrimSuffix(s, "\n"), "\n") }

// The plain layout is a drop-in for a text/tabwriter listing: two-space
// gutter, no indent, no trailing padding.
func TestPlainLayoutMatchesTabwriterListing(t *testing.T) {
	got := render(t, sample())
	want := "ID    NAME\n" +
		"4711  Ada Lovelace\n" +
		"42    Alan\n"
	if got != want {
		t.Fatalf("plain table mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestPlainLayoutHasNoTrailingPaddingWithIndent(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"ID", "Name", "Times"}, UppercaseHeaders(), WithIndent(" "))
	tbl.Append("4711", "Ada Lovelace", "09:04 - 13:24*")
	tbl.Append("42", "", "")
	got := render(t, tbl)
	want := " ID    NAME          TIMES\n" +
		" 4711  Ada Lovelace  09:04 - 13:24*\n" +
		" 42\n"
	if got != want {
		t.Fatalf("table output mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
	for _, line := range lines(got) {
		if strings.HasPrefix(line, "  ") || strings.HasSuffix(line, " ") {
			t.Fatalf("line has extra padding: %q", line)
		}
	}
}

func TestHeadersKeepTheirCaseByDefault(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"Name"})
	tbl.Append("x")
	if got := render(t, tbl); got != "Name\nx\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRowGapSeparatesRowsOnly(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"A", "B"}, WithRowGap(true))
	tbl.Append("1", "one\ntwo")
	tbl.Append("2", "three")
	got := render(t, tbl)
	want := "A  B\n1  one\n   two\n\n2  three\n"
	if got != want {
		t.Fatalf("row gap mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestRightAlignment(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"Item", "Count"}, WithAlignment(AlignLeft, AlignRight))
	tbl.Append("apples", "7")
	tbl.Append("pears", "1234")
	got := render(t, tbl)
	want := "Item    Count\napples      7\npears    1234\n"
	if got != want {
		t.Fatalf("plain alignment mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
	tbl = New(&bytes.Buffer{}, []string{"Item", "Count"}, WithStyle(Markdown), WithAlignment(AlignLeft, AlignRight))
	tbl.Append("apples", "7")
	got = render(t, tbl)
	want = "| Item   | Count |\n|--------|------:|\n| apples |     7 |\n"
	if got != want {
		t.Fatalf("markdown alignment mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestWideRunesAndPaintedCellsAlign(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"Name", "Note"}, WithStyle(Light))
	tbl.Append("日本", theme.Fg256(141).Paint("ok"))
	tbl.Append("abc", "fine")
	got := render(t, tbl)
	width := tbl.TotalWidth()
	for _, line := range lines(got) {
		if w := term.VisibleWidth(line); w != width {
			t.Fatalf("line %q measures %d, want %d", line, w, width)
		}
	}
}

func TestRenderTwiceGivesSameOutput(t *testing.T) {
	var out bytes.Buffer
	tbl := New(&out, []string{"A|B"}, WithStyle(Markdown))
	tbl.Append("x\ny")
	_ = tbl.Render()
	first := out.String()
	out.Reset()
	_ = tbl.Render()
	if out.String() != first {
		t.Fatalf("second render differs:\n%q\n%q", first, out.String())
	}
}

func TestEmptyTableRendersNothing(t *testing.T) {
	var out bytes.Buffer
	if err := New(&out, nil, WithStyle(Rounded)).Render(); err != nil || out.Len() != 0 {
		t.Fatalf("empty table wrote %q, %v", out.String(), err)
	}
}

func TestTableWithoutHeaders(t *testing.T) {
	tbl := New(&bytes.Buffer{}, nil, WithStyle(ASCII))
	tbl.Append("a", "b")
	got := render(t, tbl)
	want := "+---+---+\n| a | b |\n+---+---+\n"
	if got != want {
		t.Fatalf("headerless table mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestRowsLongerThanHeaderGetColumns(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"A"})
	tbl.Append("1", "extra")
	if got := render(t, tbl); got != "A\n1  extra\n" {
		t.Fatalf("got %q", got)
	}
	if tbl.Len() != 1 {
		t.Fatalf("Len = %d", tbl.Len())
	}
}

func TestPlainHeaderIsPaintedWhenColored(t *testing.T) {
	tbl := sample(WithColor(true))
	got := render(t, tbl)
	header := theme.Active().Header
	if !strings.HasPrefix(got, header.Paint("ID  ")) {
		t.Fatalf("header not painted with the theme's Header style:\n%q", got)
	}
	plain := render(t, sample(WithColor(false)))
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("uncolored table carries escapes: %q", plain)
	}
}

func TestColorFollowsTermByDefault(t *testing.T) {
	prev := term.CurrentColorMode()
	t.Cleanup(func() { term.SetColorMode(prev) })
	term.SetColorMode(term.ColorAlways)
	if got := render(t, sample()); !strings.Contains(got, "\x1b[") {
		t.Fatalf("--color=always table is plain: %q", got)
	}
	term.SetColorMode(term.ColorAuto)
	if got := render(t, sample()); strings.Contains(got, "\x1b[") {
		t.Fatalf("table written to a buffer is painted: %q", got)
	}
}

func TestWithThemeOverridesActiveTheme(t *testing.T) {
	custom := theme.Theme{Header: theme.Underline, Border: theme.Dim}
	got := render(t, sample(WithStyle(Light), WithColor(true), WithTheme(custom)))
	if !strings.Contains(got, theme.Underline.Sequence()+"ID") || !strings.Contains(got, theme.Dim.Sequence()+"┌") {
		t.Fatalf("custom theme not used:\n%q", got)
	}
}

func TestWithFormatSelectsStyleOrRecords(t *testing.T) {
	md := render(t, sample(WithStyle(Rounded), WithFormat(format.Markdown)))
	if !strings.HasPrefix(md, "| ID ") {
		t.Fatalf("markdown format did not select the markdown style:\n%s", md)
	}
	csv := render(t, sample(WithFormat(format.CSV)))
	if csv != "ID,Name\n4711,Ada Lovelace\n42,Alan\n" {
		t.Fatalf("csv format mismatch: %q", csv)
	}
	tsv := render(t, sample(WithFormat(format.TSV)))
	if tsv != "ID\tName\n4711\tAda Lovelace\n42\tAlan\n" {
		t.Fatalf("tsv format mismatch: %q", tsv)
	}
	text := render(t, sample(WithStyle(ASCII), WithFormat(format.Text)))
	if !strings.HasPrefix(text, "+--") {
		t.Fatalf("text format replaced the chosen style:\n%s", text)
	}
}

// A header transform changes the text of a header cell and never the escape
// sequences a caller painted into it: upper-cased, "ESC [1m" would delete a
// line instead of setting bold, and a link would point somewhere else.
func TestHeaderTransformLeavesEscapeSequences(t *testing.T) {
	bold := "\x1b[1mname\x1b[0m"
	link := term.Hyperlink("https://example.com/Docs/readme", "docs")
	tbl := New(&bytes.Buffer{}, []string{bold, link, "plain", "a\x1b[4mb\x1b[0mc"}, UppercaseHeaders(), WithColor(false))
	tbl.Append("1", "2", "3", "4")
	want := "\x1b[1mNAME\x1b[0m  " + term.Hyperlink("https://example.com/Docs/readme", "DOCS") + "  PLAIN  A\x1b[4mB\x1b[0mC\n" +
		"1     2     3      4\n"
	if got := render(t, tbl); got != want {
		t.Fatalf("table mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}

	// Any transform gets the text alone, piece by piece.
	var seen []string
	tbl = New(&bytes.Buffer{}, []string{bold + "!"}, WithColor(false), WithHeaderTransform(func(s string) string {
		seen = append(seen, s)
		return "<" + s + ">"
	}))
	if got, want := render(t, tbl), "\x1b[1m<name>\x1b[0m<!>\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if strings.Join(seen, "|") != "name|!" {
		t.Fatalf("the transform saw %q", seen)
	}

	// With SanitizeCells the color the caller painted survives the transform.
	tbl = New(&bytes.Buffer{}, []string{bold}, UppercaseHeaders(), WithColor(false), SanitizeCells())
	if got, want := render(t, tbl), "\x1b[1mNAME\x1b[0m\n"; got != want {
		t.Fatalf("sanitized header = %q, want %q", got, want)
	}
}

// A tab or a lone carriage return in a cell would move the cursor off the
// grid: the tab to a stop that depends on the column the cell is drawn at,
// the carriage return to the start of the line. A tab becomes spaces, counted
// from the start of the cell, and the carriage return goes, so every line
// keeps its width.
func TestTabsAndCarriageReturnsKeepTheGrid(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"A", "B"}, WithStyle(ASCII), WithColor(false))
	tbl.Append("a\tb", "x")
	tbl.Append("abcdef\rXY", "y\tz")
	tbl.Append("one\r\ntwo\tx", "\x1b[31m\tred\x1b[0m")
	got := render(t, tbl)
	want := "+-----------+-------------+\n" +
		"| A         | B           |\n" +
		"+-----------+-------------+\n" +
		"| a       b | x           |\n" +
		"+-----------+-------------+\n" +
		"| abcdefXY  | y       z   |\n" +
		"+-----------+-------------+\n" +
		"| one       | \x1b[31m        red\x1b[0m |\n" +
		"| two     x |             |\n" +
		"+-----------+-------------+\n"
	if got != want {
		t.Fatalf("table mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
	if strings.ContainsAny(got, "\t\r") {
		t.Fatalf("the table holds a tab or a carriage return: %q", got)
	}
	width := tbl.TotalWidth()
	for _, line := range lines(got) {
		if w := term.VisibleWidth(line); w != width {
			t.Errorf("line width %d, TotalWidth %d: %q", w, width, line)
		}
	}

	// The same cell is as wide in the second column as in the first, and
	// right-aligned as left-aligned.
	tbl = New(&bytes.Buffer{}, nil, WithAlignment(AlignLeft, AlignRight))
	tbl.Append("a\tb", "a\tb")
	tbl.Append("123456789", "123456789")
	if got, want := render(t, tbl), "a       b  a       b\n123456789  123456789\n"; got != want {
		t.Fatalf("plain table = %q, want %q", got, want)
	}
}
