package table

import (
	"bytes"
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
// its format, whatever its cells held.
func TestRecordsCarryNoControlCharacters(t *testing.T) {
	for _, tc := range []struct {
		delimiter rune
		want      string
	}{
		{',', "Name,Note\nAda Lovelace,ok,pay the attacker; pay your bank,a bc\ufffd\n"},
		{'\t', "Name\tNote\nAda Lovelace\tok\tpay the attacker; pay your bank\ta bc\ufffd\n"},
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
