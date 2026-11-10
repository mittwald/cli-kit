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

func ExampleFold() {
	for _, line := range ics.Fold("DESCRIPTION:" + strings.Repeat("abcdefghij", 8)) {
		fmt.Printf("%2d %q\n", len(line), line[:min(len(line), 16)])
	}
	// Output:
	// 75 "DESCRIPTION:abcd"
	// 18 " defghijabcdefgh"
}
