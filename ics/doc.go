// Package ics writes iCalendar (RFC 5545) files: a VCALENDAR holding VEVENT
// components, as calendar clients import or subscribe to them.
//
// The package handles the encoding rules that are easy to get wrong: TEXT
// values are escaped, control characters are dropped, invalid UTF-8 is
// repaired, content lines are folded at 75 octets without splitting a
// multi-byte character, and every line ends in CRLF.
//
// # Times
//
// Timed events are written in UTC ("DTSTART:20261110T080000Z"). UTC needs no
// VTIMEZONE component, and every client shows the instant in the viewer's
// own zone, so nothing is lost for one-off events. Recurrence rules, which
// would need a local zone to stay correct across daylight-saving changes, are
// not supported: write every occurrence as an event of its own.
//
// All-day events are written as dates ("DTSTART;VALUE=DATE:20261224"). The
// date is read in [Calendar.TimeZone] when set, or else in the location the
// time value carries. As in RFC 5545, the end of an all-day event is
// exclusive: a one-day event on 24 December ends on 25 December. By default,
// a zero End makes an all-day event last one day.
//
// # Encoding choices
//
// RFC 5545 leaves some choices to the writer. The defaults suit a new feed;
// a program that moves an existing feed to this package can set the fields
// below so the feed keeps its bytes, since subscribed clients see changed
// bytes as edited events and a changed UID as a new event:
//
//   - [Calendar.EventPropertyOrder] writes event properties in a given
//     order, such as ORGANIZER and ATTENDEE before STATUS.
//   - [Calendar.MissingEnd] leaves DTEND out, derives it or writes the zero
//     time for an event without an End.
//   - [Calendar.VerbatimUIDs] writes UIDs as given instead of removing
//     whitespace from them.
//   - [Param.Quote] quotes a parameter value even when it needs no quotes.
//   - [Param.LineBreak] replaces line breaks in a parameter value instead of
//     encoding them as RFC 6868 "^n".
package ics
