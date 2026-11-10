package ics

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var testStamp = time.Date(2026, 7, 31, 12, 34, 56, 0, time.FixedZone("CEST", 2*60*60))

func render(t *testing.T, c Calendar) string {
	t.Helper()
	if c.ProdID == "" {
		c.ProdID = "-//Example//Example CLI//EN"
	}
	if c.Stamp.IsZero() {
		c.Stamp = testStamp
	}
	out, err := c.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(out)
}

func mustContain(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("output does not contain %q:\n%s", want, got)
		}
	}
}

func TestAllDayEvent(t *testing.T) {
	day := time.Date(2026, time.December, 24, 0, 0, 0, 0, time.Local)
	got := render(t, Calendar{
		Name: "Holidays",
		Events: []Event{{
			UID:         "holiday-1@example.com",
			Start:       day,
			AllDay:      true,
			Summary:     "Heilig, Abend; Büro\\zu\n",
			Categories:  []string{"Holiday", "Office, closed"},
			Transparent: true,
			Props:       []Property{TextProperty("X-EXAMPLE-NOTE", "a;b")},
		}},
	})
	mustContain(t, got,
		"BEGIN:VCALENDAR\r\n",
		"VERSION:2.0\r\n",
		"PRODID:-//Example//Example CLI//EN\r\n",
		"CALSCALE:GREGORIAN\r\n",
		"METHOD:PUBLISH\r\n",
		"X-WR-CALNAME:Holidays\r\n",
		"UID:holiday-1@example.com\r\n",
		"DTSTAMP:20260731T103456Z\r\n",
		"DTSTART;VALUE=DATE:20261224\r\n",
		"DTEND;VALUE=DATE:20261225\r\n",
		"SUMMARY:Heilig\\, Abend\\; Büro\\\\zu\\n\r\n",
		"CATEGORIES:Holiday,Office\\, closed\r\n",
		"TRANSP:TRANSPARENT\r\n",
		"X-EXAMPLE-NOTE:a\\;b\r\n",
		"END:VEVENT\r\n",
		"END:VCALENDAR\r\n",
	)
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("output contains a bare LF: %q", got)
	}
	if strings.Contains(got, "X-WR-TIMEZONE") {
		t.Errorf("X-WR-TIMEZONE written without a TimeZone:\n%s", got)
	}
}

func TestAllDayExclusiveEnd(t *testing.T) {
	start := time.Date(2026, time.July, 9, 0, 0, 0, 0, time.Local)
	lastDay := time.Date(2026, time.July, 13, 0, 0, 0, 0, time.Local)
	got := render(t, Calendar{Events: []Event{{
		UID: "vacation", Start: start, End: lastDay.AddDate(0, 0, 1), AllDay: true,
	}}})
	mustContain(t, got, "DTSTART;VALUE=DATE:20260709\r\n", "DTEND;VALUE=DATE:20260714\r\n")
}

func TestAllDayUsesCalendarTimeZone(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("time zone data unavailable: %v", err)
	}
	// An all-day event on 2026-05-29 in Berlin (UTC+2), given as UTC instants:
	// the start falls on the previous UTC day.
	got := render(t, Calendar{TimeZone: berlin, Events: []Event{{
		UID:    "allday-1",
		Start:  time.Date(2026, 5, 28, 22, 0, 0, 0, time.UTC),
		End:    time.Date(2026, 5, 29, 22, 0, 0, 0, time.UTC),
		AllDay: true,
	}}})
	mustContain(t, got,
		"X-WR-TIMEZONE:Europe/Berlin\r\n",
		"DTSTART;VALUE=DATE:20260529\r\n",
		"DTEND;VALUE=DATE:20260530\r\n",
	)
}

func TestTimedEvent(t *testing.T) {
	start := time.Date(2026, 5, 29, 10, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
	got := render(t, Calendar{Events: []Event{{
		UID:          "uid-1",
		Start:        start,
		End:          start.Add(30 * time.Minute),
		Summary:      "Standup",
		Location:     "Room 1",
		Description:  "Daily sync.\nBring notes.",
		Status:       "confirmed",
		Class:        "private",
		LastModified: time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC),
		Props: []Property{{
			Name:   "ORGANIZER",
			Params: []Param{{Name: "CN", Value: `Smith "Jr."; Inc.`}},
			Value:  "mailto:smith@example.com",
		}},
	}}})
	mustContain(t, got,
		"DTSTART:20260529T080000Z\r\n",
		"DTEND:20260529T083000Z\r\n",
		"SUMMARY:Standup\r\n",
		"LOCATION:Room 1\r\n",
		"DESCRIPTION:Daily sync.\\nBring notes.\r\n",
		"STATUS:CONFIRMED\r\n",
		"TRANSP:OPAQUE\r\n",
		"CLASS:PRIVATE\r\n",
		"LAST-MODIFIED:20260501T090000Z\r\n",
		`ORGANIZER;CN="Smith ^'Jr.^'; Inc.":mailto:smith@example.com`+"\r\n",
	)
}

func TestTimedEventWithoutEnd(t *testing.T) {
	got := render(t, Calendar{Events: []Event{{UID: "x", Start: testStamp}}})
	if strings.Contains(got, "DTEND") {
		t.Errorf("DTEND written for a zero End:\n%s", got)
	}
	if strings.Contains(got, "SUMMARY") {
		t.Errorf("SUMMARY written for an empty summary:\n%s", got)
	}
}

func TestEventStampOverridesCalendarStamp(t *testing.T) {
	got := render(t, Calendar{Events: []Event{{
		UID: "x", Start: testStamp, Stamp: time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC),
	}}})
	mustContain(t, got, "DTSTAMP:20200102T030405Z\r\n")
}

func TestDeterministic(t *testing.T) {
	c := Calendar{Events: []Event{{UID: "x", Start: testStamp, Summary: "Same"}}}
	if first, second := render(t, c), render(t, c); first != second {
		t.Fatal("identical calendars produced different output")
	}
}

func TestMissingFields(t *testing.T) {
	if _, err := (Calendar{}).Marshal(); !errors.Is(err, ErrMissingProdID) {
		t.Errorf("missing PRODID: err = %v", err)
	}
	var out bytes.Buffer
	_, err := Calendar{ProdID: "-//x//y//EN", Events: []Event{{UID: " \t", Summary: "No id"}}}.WriteTo(&out)
	if !errors.Is(err, ErrMissingUID) {
		t.Errorf("missing UID: err = %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("an incomplete calendar wrote %q", out.String())
	}
}

func TestUIDSanitized(t *testing.T) {
	got := render(t, Calendar{Events: []Event{{UID: " a b\tc\x00d ", Start: testStamp}}})
	mustContain(t, got, "UID:abcd\r\n")
}

func TestEscapeTextDropsControls(t *testing.T) {
	if got, want := EscapeText("a\x00b\x1bc\x7fd\te\r\nf"), "abcd\te\\nf"; got != want {
		t.Errorf("EscapeText = %q, want %q", got, want)
	}
	if got := EscapeText("\xffok"); got != "\uFFFDok" {
		t.Errorf("EscapeText invalid UTF-8 = %q", got)
	}
}

func TestParamValue(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"REQ-PARTICIPANT", "REQ-PARTICIPANT"},
		{"Jane Doe", "Jane Doe"},
		{"Doe, Jane", `"Doe, Jane"`},
		{"a^b", "a^^b"},
		{"two\nlines", "two^nlines"},
		{`say "hi"`, "say ^'hi^'"},
	} {
		if got := paramValue(tc.in); got != tc.want {
			t.Errorf("paramValue(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func checkFolded(t *testing.T, lines []string) {
	t.Helper()
	for i, line := range lines {
		if len(line) > maxLineOctets {
			t.Errorf("physical line %d is %d octets: %q", i, len(line), line)
		}
		if !utf8.ValidString(line) {
			t.Errorf("physical line %d is not valid UTF-8: %q", i, line)
		}
		if i > 0 && !strings.HasPrefix(line, " ") {
			t.Errorf("continuation line %d does not start with a space: %q", i, line)
		}
	}
}

func TestFoldMultiByte(t *testing.T) {
	line := "SUMMARY:" + strings.Repeat("ä", 80)
	lines := Fold(line)
	if len(lines) < 2 {
		t.Fatalf("long line was not folded: %q", lines)
	}
	checkFolded(t, lines)
	if unfolded := strings.ReplaceAll(strings.Join(lines, "\r\n"), "\r\n ", ""); unfolded != line {
		t.Errorf("unfolding does not restore the line:\n got %q\nwant %q", unfolded, line)
	}
}

func TestFoldRepairsInvalidUTF8(t *testing.T) {
	lines := Fold("SUMMARY:" + string(bytes.Repeat([]byte{0xff, 'x'}, 80)))
	if len(lines) < 2 {
		t.Fatalf("invalid long line was not folded: %q", lines)
	}
	checkFolded(t, lines)
}

func TestFoldShortLine(t *testing.T) {
	line := strings.Repeat("x", maxLineOctets)
	if lines := Fold(line); len(lines) != 1 || lines[0] != line {
		t.Errorf("a 75-octet line was folded: %q", lines)
	}
}

func TestWrittenLinesAreFolded(t *testing.T) {
	got := render(t, Calendar{Events: []Event{{UID: "fold", Start: testStamp, Summary: strings.Repeat("x", 200)}}})
	physical := strings.Split(strings.TrimSuffix(got, "\r\n"), "\r\n")
	for i, line := range physical {
		if len(line) > maxLineOctets {
			t.Errorf("line %d is %d octets: %q", i, len(line), line)
		}
	}
	if !strings.Contains(got, "\r\n x") {
		t.Errorf("expected a continuation line:\n%s", got)
	}
}

func TestOccurrenceUID(t *testing.T) {
	first := OccurrenceUID("SERIES 1", time.Date(2026, 5, 29, 10, 0, 0, 0, time.FixedZone("CEST", 7200)))
	second := OccurrenceUID("SERIES 1", time.Date(2026, 5, 30, 8, 0, 0, 0, time.UTC))
	if first != "SERIES1-20260529T080000Z" || second != "SERIES1-20260530T080000Z" {
		t.Errorf("OccurrenceUID = %q, %q", first, second)
	}
}

func TestHashUID(t *testing.T) {
	a := HashUID("example.com", "Alice", "2026-07-09", "vacation")
	if a != HashUID("example.com", "Alice", "2026-07-09", "vacation") {
		t.Error("HashUID is not stable")
	}
	if a == HashUID("example.com", "Alice", "2026-07-10", "vacation") {
		t.Error("different parts gave the same UID")
	}
	if HashUID("", "ab", "c") == HashUID("", "a", "bc") {
		t.Error("part boundaries do not affect the UID")
	}
	if !strings.HasSuffix(a, "@example.com") || strings.Contains(a, "Alice") {
		t.Errorf("HashUID = %q", a)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestWriteError(t *testing.T) {
	_, err := Calendar{ProdID: "-//x//y//EN", Stamp: testStamp}.WriteTo(failingWriter{})
	if err == nil || err.Error() != "write failed" {
		t.Fatalf("err = %v, want the writer's error", err)
	}
}
