package table

import (
	"bytes"
	"encoding/csv"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/mittwald/cli-kit/theme"
)

func renderRecords(t *testing.T, delimiter rune, headers []string, rows ...[]string) string {
	t.Helper()
	tbl := New(&bytes.Buffer{}, headers, WithDelimiter(delimiter), UppercaseHeaders(), WithRowGap(true), WithIndent(" "))
	for _, row := range rows {
		tbl.Append(row...)
	}
	return render(t, tbl)
}

// The header keeps its original casing: a spreadsheet column reads better as
// "Date" than as the drawn "DATE".
func TestCSVQuotesAndEscapes(t *testing.T) {
	got := renderRecords(t, ',',
		[]string{"Date", "Times", "Event"},
		[]string{"Mon 2025-09-15", "09:04 - 13:24\n14:00 - 17:31", "Offsite, half day"},
		[]string{"Tue 2025-09-16", "08:12 - --:--", `Meeting "Sync"`},
	)
	want := "Date,Times,Event\n" +
		"Mon 2025-09-15,09:04 - 13:24; 14:00 - 17:31,\"Offsite, half day\"\n" +
		"Tue 2025-09-16,08:12 - --:--,\"Meeting \"\"Sync\"\"\"\n"
	if got != want {
		t.Fatalf("csv mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestCSVWithOtherSeparator(t *testing.T) {
	got := renderRecords(t, ';', []string{"A", "B"}, []string{"1;2", "3"})
	if got != "A;B\n\"1;2\";3\n" {
		t.Fatalf("semicolon csv mismatch: %q", got)
	}
}

func TestCSVRejectsInvalidSeparator(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"A"}, WithDelimiter('"'))
	tbl.Append("x")
	if err := tbl.Render(); err == nil {
		t.Fatal("quote accepted as a field separator")
	}
}

// TSV never quotes: a quoted field would be read literally by cut(1). The only
// lossy case is a tab inside a value, which becomes a space.
func TestTSVNeverQuotesAndStripsTabs(t *testing.T) {
	got := renderRecords(t, '\t',
		[]string{"Date", "Times", "Event"},
		[]string{"Mon 2025-09-15", "09:04 - 13:24\r\n14:00 - 17:31", "Offsite, half day"},
		[]string{"Tue 2025-09-16", "08:12 - --:--", "Meeting \"Sync\"\tExternal"},
	)
	want := "Date\tTimes\tEvent\n" +
		"Mon 2025-09-15\t09:04 - 13:24; 14:00 - 17:31\tOffsite, half day\n" +
		"Tue 2025-09-16\t08:12 - --:--\tMeeting \"Sync\" External\n"
	if got != want {
		t.Fatalf("tsv mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
	for _, line := range lines(got) {
		if strings.Count(line, "\t") != 2 {
			t.Fatalf("record does not carry exactly three fields: %q", line)
		}
	}
}

// A painted cell must not leak escape sequences into a record, whatever put
// the color there.
func TestRecordsStripStyling(t *testing.T) {
	tbl := New(&bytes.Buffer{}, []string{"Name"}, WithDelimiter(','), WithColor(true))
	tbl.Append(theme.Bold.Paint("Ada"))
	if got := render(t, tbl); got != "Name\nAda\n" {
		t.Fatalf("painted cell leaked into the record: %q", got)
	}
}

func TestRecordLineJoin(t *testing.T) {
	tbl := New(&bytes.Buffer{}, nil, WithDelimiter(','), WithRecordLineJoin(" / "))
	tbl.Append("a\nb", "c\rd")
	if got := render(t, tbl); got != "a / b,c / d\n" {
		t.Fatalf("line join mismatch: %q", got)
	}
}

// untrustedRow carries what a cell from elsewhere may hold: a clipboard write
// in both its forms, an 8-bit CSI, a bell, backspaces over text, a carriage
// return, a tab, a NUL and bytes that are not UTF-8.
var untrustedRow = []string{
	"Ada\x1b]52;c;Zm9v\x07\u009d52;c;Zm9v\u009c Lovelace",
	"\u009b2Jok\x07\x08\x08",
	"pay the attacker\rpay your bank",
	"a\tb\x00c\xff",
}

// A record carries no control character but the separators and line ends of
// its format, whatever its cells held. The header record is as long as the
// row, which has two cells more.
func TestRecordsCarryNoControlCharacters(t *testing.T) {
	for _, tc := range []struct {
		delimiter rune
		want      string
	}{
		{',', "Name,Note,,\nAda Lovelace,ok,pay the attacker; pay your bank,a bc\ufffd\n"},
		{'\t', "Name\tNote\t\t\nAda Lovelace\tok\tpay the attacker; pay your bank\ta bc\ufffd\n"},
	} {
		got := renderRecords(t, tc.delimiter, []string{"Name\u0085", "No\x1b[31mte"}, untrustedRow)
		if got != tc.want {
			t.Errorf("records with %q mismatch\nwant:\n%q\ngot:\n%q", tc.delimiter, tc.want, got)
		}
		for _, r := range got {
			if r != '\n' && r != tc.delimiter && (r < 0x20 || (r >= 0x7f && r <= 0x9f)) {
				t.Errorf("record carries %U: %q", r, got)
			}
		}
	}
}

// Rows may be shorter or longer than the header and than each other. Every
// record still has the same number of fields, the missing ones empty, so a
// reader that insists on that, as encoding/csv does, takes the output.
func TestRecordsHaveTheSameNumberOfFields(t *testing.T) {
	headers := []string{"A", "B", "C"}
	rows := [][]string{{"1"}, {"1", "2", "3", "4"}, {}, {""}, {"", "", "", "x"}}
	want := [][]string{
		{"A", "B", "C", ""},
		{"1", "", "", ""},
		{"1", "2", "3", "4"},
		{"", "", "", ""},
		{"", "", "", ""},
		{"", "", "", "x"},
	}

	got := renderRecords(t, ',', headers, rows...)
	if text := "A,B,C,\n1,,,\n1,2,3,4\n,,,\n,,,\n,,,x\n"; got != text {
		t.Errorf("csv mismatch\nwant:\n%q\ngot:\n%q", text, got)
	}
	records, err := csv.NewReader(strings.NewReader(got)).ReadAll()
	if err != nil || !reflect.DeepEqual(records, want) {
		t.Errorf("encoding/csv reads %q, %v, want %q", records, err, want)
	}

	got = renderRecords(t, '\t', headers, rows...)
	if text := "A\tB\tC\t\n1\t\t\t\n1\t2\t3\t4\n\t\t\t\n\t\t\t\n\t\t\tx\n"; got != text {
		t.Errorf("tsv mismatch\nwant:\n%q\ngot:\n%q", text, got)
	}
	for i, line := range lines(got) {
		if fields := strings.Split(line, "\t"); !reflect.DeepEqual(fields, want[i]) {
			t.Errorf("tsv record %d = %q, want %q", i, fields, want[i])
		}
	}
}

// A record of one empty field would be an empty line, which readers of CSV
// skip, so the row would be gone. It is written as a quoted empty field.
func TestCSVKeepsEmptyRecords(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers []string
		rows    [][]string
		text    string
		want    [][]string
	}{
		{"rows without cells", nil, [][]string{{}, {}}, "\"\"\n\"\"\n", [][]string{{""}, {""}}},
		{"rows of one empty cell", nil, [][]string{{""}, {"x"}, {""}}, "\"\"\nx\n\"\"\n", [][]string{{""}, {"x"}, {""}}},
		{"under a header", []string{"Note"}, [][]string{{"a"}, {}, {"b"}}, "Note\na\n\"\"\nb\n", [][]string{{"Note"}, {"a"}, {""}, {"b"}}},
		{"empty header", []string{""}, [][]string{{"a"}}, "\"\"\na\n", [][]string{{""}, {"a"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := renderRecords(t, ',', tc.headers, tc.rows...)
			if got != tc.text {
				t.Errorf("csv = %q, want %q", got, tc.text)
			}
			records, err := csv.NewReader(strings.NewReader(got)).ReadAll()
			if err != nil || !reflect.DeepEqual(records, tc.want) {
				t.Errorf("encoding/csv reads %q, %v, want %q", records, err, tc.want)
			}
		})
	}

	// TSV has no quotes: there the record is the empty line.
	if got := renderRecords(t, '\t', []string{"Note"}, []string{"a"}, []string{}, []string{"b"}); got != "Note\na\n\nb\n" {
		t.Errorf("tsv = %q", got)
	}

	// A table without headers and rows still writes nothing.
	if got := renderRecords(t, ',', nil); got != "" {
		t.Errorf("an empty table wrote %q", got)
	}
}

// A failing writer is reported whichever way a record is written.
func TestCSVReportsWriteErrors(t *testing.T) {
	for _, rows := range [][][]string{{{"a"}}, {{""}}, {{"a"}, {""}, {"b"}}} {
		tbl := New(failingWriter{}, nil, WithDelimiter(','))
		for _, row := range rows {
			tbl.Append(row...)
		}
		if err := tbl.Render(); err == nil {
			t.Errorf("rows %q: the write error was lost", rows)
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

var errWrite = errors.New("write failed")

// Record output is written as it is by default: a spreadsheet takes a cell
// that starts with "=" for a formula. EscapeFormulas puts a single quote in
// front of such cells, header cells included, and leaves the others alone.
func TestEscapeFormulas(t *testing.T) {
	headers := []string{"=Name", "Note"}
	rows := [][]string{
		{"=1+1", "+cmd|' /C calc'!A0"},
		{"-2+3", "@SUM(A1)"},
		{"\t=1+1", "\r=1+1"},
		{"\tplain", "  =1+1"},
		{"\x1b[31m=1+1\x1b[0m", "a\n=b"},
		// No formulas: signed numbers, and a sign that is not at the start.
		{"-12.5", "+1,000"},
		{"a=b", "x - y"},
		{"", "-"},
	}
	want := [][]string{
		{"'=Name", "Note"},
		{"'=1+1", "'+cmd|' /C calc'!A0"},
		{"'-2+3", "'@SUM(A1)"},
		{"' =1+1", "'; =1+1"},
		{"' plain", "'  =1+1"},
		{"'=1+1", "a; =b"},
		{"-12.5", "+1,000"},
		{"a=b", "x - y"},
		{"", "'-"},
	}
	for _, delimiter := range []rune{',', '\t'} {
		tbl := New(&bytes.Buffer{}, headers, WithDelimiter(delimiter), EscapeFormulas())
		for _, row := range rows {
			tbl.Append(row...)
		}
		reader := csv.NewReader(strings.NewReader(render(t, tbl)))
		reader.Comma = delimiter
		reader.LazyQuotes = true
		records, err := reader.ReadAll()
		if err != nil || !reflect.DeepEqual(records, want) {
			t.Errorf("with %q: records %q, %v, want %q", delimiter, records, err, want)
		}
	}

	// Without the option nothing is prefixed.
	tbl := New(&bytes.Buffer{}, headers, WithDelimiter(','))
	tbl.Append("=1+1", "@SUM(A1)")
	if got, want := render(t, tbl), "=Name,Note\n=1+1,@SUM(A1)\n"; got != want {
		t.Errorf("csv = %q, want %q", got, want)
	}

	// A drawn table shows its cells as they are.
	tbl = New(&bytes.Buffer{}, nil, EscapeFormulas())
	tbl.Append("=1+1")
	if got := render(t, tbl); got != "=1+1\n" {
		t.Errorf("drawn table = %q", got)
	}
}
