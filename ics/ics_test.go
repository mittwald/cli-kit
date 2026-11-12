package ics

import (
	"bytes"
	"errors"
	"slices"
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
	// The C1 controls go too: a terminal the file is printed on would take
	// U+009B for ESC [ and U+009D for ESC ]. U+00A0 and what follows stay.
	if got, want := EscapeText("a\u0080b\u009b2Jc\u009d52;c;Zm9v\u009cd\u0085e\u009f\u00a0ü"), "ab2Jc52\\;c\\;Zm9vde\u00a0ü"; got != want {
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
		if got := (Param{Value: tc.in}).encode(); got != tc.want {
			t.Errorf("encode(%q) = %q, want %q", tc.in, got, tc.want)
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

// eventLines returns the content lines of the first event in an encoded
// calendar, unfolded, without BEGIN:VEVENT and END:VEVENT.
func eventLines(t *testing.T, calendar string) []string {
	t.Helper()
	unfolded := strings.ReplaceAll(calendar, "\r\n ", "")
	_, rest, ok := strings.Cut(unfolded, "BEGIN:VEVENT\r\n")
	if !ok {
		t.Fatalf("no event in:\n%s", calendar)
	}
	body, _, ok := strings.Cut(rest, "END:VEVENT\r\n")
	if !ok {
		t.Fatalf("unterminated event in:\n%s", calendar)
	}
	return strings.Split(strings.TrimSuffix(body, "\r\n"), "\r\n")
}

// checkPhysicalLines checks that every line of an encoded calendar is valid
// UTF-8 of at most 75 octets and ends in CRLF.
func checkPhysicalLines(t *testing.T, calendar string) {
	t.Helper()
	if !strings.HasSuffix(calendar, "\r\n") {
		t.Errorf("calendar does not end in CRLF: %q", calendar)
	}
	for i, line := range strings.Split(strings.TrimSuffix(calendar, "\r\n"), "\r\n") {
		if len(line) > maxLineOctets {
			t.Errorf("physical line %d is %d octets: %q", i, len(line), line)
		}
		if !utf8.ValidString(line) {
			t.Errorf("physical line %d is not valid UTF-8: %q", i, line)
		}
		if strings.ContainsAny(line, "\r\n") {
			t.Errorf("physical line %d holds a bare line break: %q", i, line)
		}
	}
}

func TestParamQuote(t *testing.T) {
	for _, tc := range []struct {
		param Param
		want  string
	}{
		{Param{Value: "Jane Doe"}, "Jane Doe"},
		{Param{Value: "Jane Doe", Quote: true}, `"Jane Doe"`},
		{Param{Value: "REQ-PARTICIPANT", Quote: true}, `"REQ-PARTICIPANT"`},
		{Param{Value: "", Quote: true}, `""`},
		// Quoting does not replace the RFC 6868 encoding of what a
		// quoted-string cannot hold.
		{Param{Value: `Smith "Jr."; ^x`, Quote: true}, `"Smith ^'Jr.^'; ^^x"`},
		{Param{Value: "two\nlines", Quote: true}, `"two^nlines"`},
		{Param{Value: "a\x00b\x7fc\td", Quote: true}, "\"abc\td\""},
		{Param{Value: "Jane\u009b2J\u0085 Doe", Quote: true}, "\"Jane2J Doe\""},
		{Param{Value: "bad\xff\xfe", Quote: true}, "\"bad\uFFFD\""},
		// A value that needs quotes is quoted either way.
		{Param{Value: "Doe, Jane", Quote: false}, `"Doe, Jane"`},
		{Param{Value: "Doe, Jane", Quote: true}, `"Doe, Jane"`},
	} {
		if got := tc.param.encode(); got != tc.want {
			t.Errorf("%+v encodes as %q, want %q", tc.param, got, tc.want)
		}
	}
}

func TestParamLineBreak(t *testing.T) {
	for _, tc := range []struct {
		value, lineBreak, want string
	}{
		// Empty keeps the RFC 6868 encoding, a CRLF pair counted as one.
		{"a\nb", "", "a^nb"},
		{"a\r\nb", "", "a^nb"},
		{"a\rb", "", "a^nb"},
		// A replacement replaces each CR and each LF.
		{"a\nb", " ", "a b"},
		{"a\rb", " ", "a b"},
		{"a\r\nb", " ", "a  b"},
		{"\na\n", " ", " a "},
		// The replacement is encoded like the value around it.
		{"a\nb", "^", "a^^b"},
		{"a\nb", `"`, "a^'b"},
		{"a\nb", ", ", `"a, b"`},
		{"a\nb", "\x07", "ab"},
		{"a\nb", "\xff", "a\uFFFDb"},
		// Other control characters are dropped, invalid UTF-8 repaired.
		{"a\n\x00b\x1b", " ", "a b"},
		{"\xff\xfea\nb", " ", "\uFFFDa b"},
	} {
		got := (Param{Value: tc.value, LineBreak: tc.lineBreak}).encode()
		if got != tc.want {
			t.Errorf("value %q with LineBreak %q encodes as %q, want %q", tc.value, tc.lineBreak, got, tc.want)
		}
	}
}

// The parameter options change the value, not how the line around it is
// escaped and folded.
func TestParamOptionsFold(t *testing.T) {
	name := `Ünal "Ü" Øvergård-Ålesund ^ ` + strings.Repeat("ß€𝄞", 8) + "\r\nTeam"
	attendee := Property{
		Name: "ATTENDEE",
		Params: []Param{
			{Name: "CN", Value: name, Quote: true, LineBreak: " "},
			{Name: "ROLE", Value: "REQ-PARTICIPANT"},
		},
		Value: "mailto:unal@example.com",
	}
	wantLine := `ATTENDEE;CN="Ünal ^'Ü^' Øvergård-Ålesund ^^ ` + strings.Repeat("ß€𝄞", 8) +
		`  Team";ROLE=REQ-PARTICIPANT:mailto:unal@example.com`
	if got := attendee.String(); got != wantLine {
		t.Fatalf("String() = %q, want %q", got, wantLine)
	}
	got := render(t, Calendar{Events: []Event{{UID: "x", Start: testStamp, Props: []Property{attendee}}}})
	checkPhysicalLines(t, got)
	if !strings.Contains(got, "\r\n ") {
		t.Errorf("the long line was not folded:\n%s", got)
	}
	if !slices.Contains(eventLines(t, got), wantLine) {
		t.Errorf("unfolding does not restore %q:\n%s", wantLine, got)
	}
}

func TestEventPropertyOrder(t *testing.T) {
	attendee := func(mail string) Property {
		return Property{Name: "ATTENDEE", Params: []Param{{Name: "CN", Value: mail, Quote: true}}, Value: "mailto:" + mail}
	}
	ev := Event{
		UID:          "order",
		Start:        testStamp,
		End:          testStamp.Add(time.Hour),
		Summary:      strings.Repeat("Planning, weekly; ", 5),
		Location:     "Room 1",
		Status:       "confirmed",
		Class:        "private",
		LastModified: testStamp,
		Props: []Property{
			attendee("ann@example.com"),
			TextProperty("X-NOTE", "a;b"),
			{Name: "organizer", Value: "mailto:jane@example.com"},
			attendee("bob@example.com"),
		},
	}
	original := slices.Clone(ev.Props)
	summary := "SUMMARY:" + strings.Repeat(`Planning\, weekly\; `, 5)

	got := render(t, Calendar{
		// Names match regardless of case; a repeated name keeps its first
		// position, and a name no property has is skipped.
		EventPropertyOrder: []string{"uid", "DTSTAMP", "DTSTART", "DTEND", "ORGANIZER", "Attendee", "SUMMARY", "X-MISSING", "UID", "STATUS"},
		Events:             []Event{ev},
	})
	want := []string{
		"UID:order",
		"DTSTAMP:20260731T103456Z",
		"DTSTART:20260731T103456Z",
		"DTEND:20260731T113456Z",
		"ORGANIZER:mailto:jane@example.com",
		`ATTENDEE;CN="ann@example.com":mailto:ann@example.com`,
		`ATTENDEE;CN="bob@example.com":mailto:bob@example.com`,
		summary,
		"STATUS:CONFIRMED",
		// The properties the order does not name, in the default order.
		"LOCATION:Room 1",
		"TRANSP:OPAQUE",
		"CLASS:PRIVATE",
		"LAST-MODIFIED:20260731T103456Z",
		`X-NOTE:a\;b`,
	}
	if lines := eventLines(t, got); !slices.Equal(lines, want) {
		t.Errorf("event lines:\n got %q\nwant %q", lines, want)
	}
	// Moved properties are still folded.
	checkPhysicalLines(t, got)
	if !slices.EqualFunc(ev.Props, original, func(a, b Property) bool { return a.String() == b.String() }) {
		t.Errorf("writing reordered the caller's Props: %v", ev.Props)
	}

	// Without an order, Props follow the standard properties as given.
	got = render(t, Calendar{Events: []Event{ev}})
	want = []string{
		"UID:order",
		"DTSTAMP:20260731T103456Z",
		"DTSTART:20260731T103456Z",
		"DTEND:20260731T113456Z",
		summary,
		"LOCATION:Room 1",
		"STATUS:CONFIRMED",
		"TRANSP:OPAQUE",
		"CLASS:PRIVATE",
		"LAST-MODIFIED:20260731T103456Z",
		`ATTENDEE;CN="ann@example.com":mailto:ann@example.com`,
		`X-NOTE:a\;b`,
		"ORGANIZER:mailto:jane@example.com",
		`ATTENDEE;CN="bob@example.com":mailto:bob@example.com`,
	}
	if lines := eventLines(t, got); !slices.Equal(lines, want) {
		t.Errorf("default event lines:\n got %q\nwant %q", lines, want)
	}
}

func TestMissingEnd(t *testing.T) {
	start := time.Date(2026, 12, 24, 9, 30, 0, 0, time.UTC)
	west := time.FixedZone("UTC-5", -5*60*60)
	for _, tc := range []struct {
		name   string
		mode   EndMode
		allDay bool
		zone   *time.Location
		want   string // the DTEND line; empty when there is none
	}{
		{"default timed", EndDefault, false, nil, ""},
		{"default all-day", EndDefault, true, nil, "DTEND;VALUE=DATE:20261225"},
		{"omit timed", EndOmit, false, nil, ""},
		{"omit all-day", EndOmit, true, nil, ""},
		{"derive timed", EndDerive, false, nil, "DTEND:20261224T093000Z"},
		{"derive all-day", EndDerive, true, nil, "DTEND;VALUE=DATE:20261225"},
		// The next day is counted from the start's date in the zone, not
		// converted into the zone once more.
		{"derive all-day west", EndDerive, true, west, "DTEND;VALUE=DATE:20261225"},
		{"as given timed", EndAsGiven, false, nil, "DTEND:00010101T000000Z"},
		{"as given all-day", EndAsGiven, true, nil, "DTEND;VALUE=DATE:00010101"},
		// The zero time falls on the last day of year 0 west of UTC.
		{"as given all-day west", EndAsGiven, true, west, "DTEND;VALUE=DATE:00001231"},
		{"unknown mode timed", EndMode(99), false, nil, ""},
		{"unknown mode all-day", EndMode(99), true, nil, "DTEND;VALUE=DATE:20261225"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := render(t, Calendar{MissingEnd: tc.mode, TimeZone: tc.zone, Events: []Event{{UID: "x", Start: start, AllDay: tc.allDay}}})
			var dtend []string
			for _, line := range eventLines(t, got) {
				if strings.HasPrefix(line, "DTEND") {
					dtend = append(dtend, line)
				}
			}
			switch {
			case tc.want == "" && len(dtend) > 0:
				t.Errorf("DTEND written: %q", dtend)
			case tc.want != "" && !slices.Equal(dtend, []string{tc.want}):
				t.Errorf("DTEND = %q, want %q", dtend, tc.want)
			}
		})
	}

	// An End that is set is written whatever the mode.
	for _, mode := range []EndMode{EndDefault, EndOmit, EndDerive, EndAsGiven} {
		got := render(t, Calendar{MissingEnd: mode, Events: []Event{
			{UID: "timed", Start: start, End: start.Add(time.Hour)},
			{UID: "allday", Start: start, End: start.AddDate(0, 0, 3), AllDay: true},
		}})
		mustContain(t, got, "DTEND:20261224T103000Z\r\n", "DTEND;VALUE=DATE:20261227\r\n")
	}
}

func TestVerbatimUIDs(t *testing.T) {
	const uid = " a\tb\u00a0c d;e,f\\g\r\n\x00h\x7fi\u0085j "
	got := render(t, Calendar{VerbatimUIDs: true, Events: []Event{{UID: uid, Start: testStamp}}})
	// Spaces, tab and no-break space stay, and so do the characters TEXT
	// would escape, as in the default; the control characters RFC 5545
	// forbids go, and a C1 control, which it allows, stays.
	mustContain(t, got, "UID: a\tb\u00a0c d;e,f\\ghi\u0085j \r\n")

	// By default the C1 control goes as well.
	got = render(t, Calendar{Events: []Event{{UID: uid, Start: testStamp}}})
	mustContain(t, got, "UID:ab\u00a0cd;e,f\\ghij\r\n")

	// Invalid UTF-8 is repaired as in TEXT values: one U+FFFD per run.
	got = render(t, Calendar{VerbatimUIDs: true, Events: []Event{{UID: "a\xff\xfeb", Start: testStamp}}})
	mustContain(t, got, "UID:a\uFFFDb\r\n")

	// A long UID is folded without splitting a character.
	long := strings.Repeat("€ä𝄞 ", 20) + "@example.com"
	got = render(t, Calendar{VerbatimUIDs: true, Events: []Event{{UID: long, Start: testStamp}}})
	checkPhysicalLines(t, got)
	if lines := eventLines(t, got); lines[0] != "UID:"+long {
		t.Errorf("unfolded UID line = %q, want %q", lines[0], "UID:"+long)
	}

	for _, blank := range []string{"", " \t", "\r\n\x00", "\u00a0\u2003"} {
		_, err := Calendar{ProdID: "-//x//y//EN", VerbatimUIDs: true, Events: []Event{{UID: blank}}}.Marshal()
		if !errors.Is(err, ErrMissingUID) {
			t.Errorf("UID %q: err = %v, want ErrMissingUID", blank, err)
		}
	}
}
