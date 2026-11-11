package ics_test

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mittwald/cli-kit/ics"
)

func ExampleCalendar_WriteTo() {
	stamp := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
	cal := ics.Calendar{
		ProdID: "-//Example//Example CLI//EN",
		Name:   "Team",
		Stamp:  stamp,
		Events: []ics.Event{
			{
				UID:         ics.HashUID("example.com", "holiday", "2026-12-24"),
				Start:       time.Date(2026, 12, 24, 0, 0, 0, 0, time.UTC),
				AllDay:      true,
				Summary:     "Christmas Eve",
				Transparent: true,
			},
			{
				UID:     "standup-20261110@example.com",
				Start:   time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC),
				End:     time.Date(2026, 11, 10, 9, 15, 0, 0, time.UTC),
				Summary: "Standup, daily",
				Props:   []ics.Property{ics.TextProperty("X-EXAMPLE-ROOM", "Blue")},
			},
		},
	}
	var out strings.Builder
	if _, err := cal.WriteTo(&out); err != nil {
		panic(err)
	}
	// Print with LF line ends; the file itself uses CRLF.
	_, _ = os.Stdout.WriteString(strings.ReplaceAll(out.String(), "\r\n", "\n"))
	// Output:
	// BEGIN:VCALENDAR
	// VERSION:2.0
	// PRODID:-//Example//Example CLI//EN
	// CALSCALE:GREGORIAN
	// METHOD:PUBLISH
	// X-WR-CALNAME:Team
	// BEGIN:VEVENT
	// UID:b327d0058e593094f8d9821ffa3d7219@example.com
	// DTSTAMP:20261110T080000Z
	// DTSTART;VALUE=DATE:20261224
	// DTEND;VALUE=DATE:20261225
	// SUMMARY:Christmas Eve
	// TRANSP:TRANSPARENT
	// END:VEVENT
	// BEGIN:VEVENT
	// UID:standup-20261110@example.com
	// DTSTAMP:20261110T080000Z
	// DTSTART:20261110T090000Z
	// DTEND:20261110T091500Z
	// SUMMARY:Standup\, daily
	// TRANSP:OPAQUE
	// X-EXAMPLE-ROOM:Blue
	// END:VEVENT
	// END:VCALENDAR
}

// A feed that lists the participants before the status, quotes every name
// and writes a line break in a name as a space, for clients that do not
// decode RFC 6868.
func ExampleCalendar_WriteTo_participants() {
	name := func(value string) ics.Param {
		return ics.Param{Name: "CN", Value: value, Quote: true, LineBreak: " "}
	}
	cal := ics.Calendar{
		ProdID: "-//Example//Example CLI//EN",
		Stamp:  time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC),
		EventPropertyOrder: []string{
			"UID", "DTSTAMP", "DTSTART", "DTEND", "SUMMARY", "ORGANIZER", "ATTENDEE",
		},
		Events: []ics.Event{{
			UID:     "planning-20261110@example.com",
			Start:   time.Date(2026, 11, 10, 13, 0, 0, 0, time.UTC),
			End:     time.Date(2026, 11, 10, 14, 0, 0, 0, time.UTC),
			Summary: "Planning",
			Status:  "confirmed",
			Props: []ics.Property{
				{
					Name:   "ATTENDEE",
					Params: []ics.Param{name("Ann Other"), {Name: "PARTSTAT", Value: "ACCEPTED"}},
					Value:  "mailto:ann@example.com",
				},
				{
					Name:   "ORGANIZER",
					Params: []ics.Param{name("Jane Doe\nTeam Lead")},
					Value:  "mailto:jane@example.com",
				},
			},
		}},
	}
	var out strings.Builder
	if _, err := cal.WriteTo(&out); err != nil {
		panic(err)
	}
	_, _ = os.Stdout.WriteString(strings.ReplaceAll(out.String(), "\r\n", "\n"))
	// Output:
	// BEGIN:VCALENDAR
	// VERSION:2.0
	// PRODID:-//Example//Example CLI//EN
	// CALSCALE:GREGORIAN
	// METHOD:PUBLISH
	// BEGIN:VEVENT
	// UID:planning-20261110@example.com
	// DTSTAMP:20261110T080000Z
	// DTSTART:20261110T130000Z
	// DTEND:20261110T140000Z
	// SUMMARY:Planning
	// ORGANIZER;CN="Jane Doe Team Lead":mailto:jane@example.com
	// ATTENDEE;CN="Ann Other";PARTSTAT=ACCEPTED:mailto:ann@example.com
	// STATUS:CONFIRMED
	// TRANSP:OPAQUE
	// END:VEVENT
	// END:VCALENDAR
}

func ExampleFold() {
	for _, line := range ics.Fold("DESCRIPTION:" + strings.Repeat("abcdefghij", 8)) {
		fmt.Printf("%2d %q\n", len(line), line[:min(len(line), 16)])
	}
	// Output:
	// 75 "DESCRIPTION:abcd"
	// 18 " defghijabcdefgh"
}
