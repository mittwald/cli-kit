package ics

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

// Formats of DATE-TIME and DATE values.
const (
	utcFormat  = "20060102T150405Z"
	dateFormat = "20060102"
)

// maxLineOctets is the longest a physical content line may be, CRLF left out.
const maxLineOctets = 75

// ErrMissingUID is returned when an event has no UID. RFC 5545 requires one,
// and clients use it to recognize an event they imported before.
var ErrMissingUID = errors.New("ics: event has no UID")

// ErrMissingProdID is returned when a calendar has no product identifier.
var ErrMissingProdID = errors.New("ics: calendar has no PRODID")

// Calendar is one VCALENDAR object.
type Calendar struct {
	// ProdID identifies the program that wrote the calendar, as a formal
	// public identifier such as "-//Example//Example CLI//EN". It is required.
	ProdID string
	// Name is the calendar's display name (X-WR-CALNAME). Optional.
	Name string
	// Method is the iTIP method; empty means "PUBLISH", the method for a
	// calendar that is imported or subscribed to.
	Method string
	// Scale is the calendar scale; empty means "GREGORIAN".
	Scale string
	// TimeZone is the zone all-day dates are read in. When set, it is also
	// written as X-WR-TIMEZONE, which some clients use as the calendar's zone.
	TimeZone *time.Location
	// Stamp is the DTSTAMP of every event that has none of its own. Zero
	// means the time of writing; set it for reproducible output.
	Stamp time.Time
	// Props are further calendar properties, such as X- properties, written
	// after the standard ones.
	Props []Property
	// Events are the calendar's events, written in order.
	Events []Event
}

// Event is one VEVENT component.
type Event struct {
	// UID identifies the event across exports and is required. Whitespace
	// and control characters are removed. Occurrences of the same series need
	// distinct UIDs, see [OccurrenceUID].
	UID string
	// Stamp is the event's DTSTAMP; zero falls back to [Calendar.Stamp].
	Stamp time.Time
	// Start is when the event begins.
	Start time.Time
	// End is when the event ends, exclusive. Zero leaves DTEND out, which
	// makes a timed event end at its start and an all-day event last a day.
	End time.Time
	// AllDay writes Start and End as dates instead of times.
	AllDay bool
	// Summary is the event's title. Empty leaves SUMMARY out.
	Summary string
	// Description is the event's longer text.
	Description string
	// Location is where the event takes place.
	Location string
	// Categories are written as one CATEGORIES property.
	Categories []string
	// Status is an RFC 5545 event status, such as "TENTATIVE" or
	// "CONFIRMED"; empty leaves STATUS out.
	Status string
	// Class is "PUBLIC", "PRIVATE" or "CONFIDENTIAL"; empty leaves CLASS out,
	// which clients read as PUBLIC.
	Class string
	// Transparent marks the event as not blocking time (TRANSP:TRANSPARENT),
	// as a holiday or an informational entry is. Otherwise it is OPAQUE.
	Transparent bool
	// LastModified is written as LAST-MODIFIED when set.
	LastModified time.Time
	// Props are further event properties, such as ORGANIZER, ATTENDEE or
	// X- properties, written after the standard ones.
	Props []Property
}

// Param is one property parameter, such as CN="Jane Doe".
type Param struct {
	Name  string
	Value string
}

// Property is one content line: a name, optional parameters, and a value.
// Value is written as given; build TEXT values with [TextProperty], which
// escapes them.
type Property struct {
	Name   string
	Params []Param
	Value  string
}

// TextProperty returns a property whose value is TEXT, escaped with
// [EscapeText]. Use it for X- properties and other free text.
func TextProperty(name, value string, params ...Param) Property {
	return Property{Name: name, Params: params, Value: EscapeText(value)}
}

// String returns the property as one unfolded content line without CRLF.
// Parameter values are quoted when they contain ":", ";" or ",", and caret
// encoded as RFC 6868 describes, so they cannot end the parameter early.
func (p Property) String() string {
	var b strings.Builder
	b.WriteString(strings.ToUpper(p.Name))
	for _, param := range p.Params {
		b.WriteByte(';')
		b.WriteString(strings.ToUpper(param.Name))
		b.WriteByte('=')
		b.WriteString(paramValue(param.Value))
	}
	b.WriteByte(':')
	b.WriteString(p.Value)
	return b.String()
}

var paramEscaper = strings.NewReplacer("^", "^^", "\r\n", "^n", "\n", "^n", "\r", "^n", `"`, "^'")

// paramValue encodes a parameter value (RFC 5545 section 3.2, RFC 6868).
func paramValue(value string) string {
	value = paramEscaper.Replace(stripControls(strings.ToValidUTF8(value, "�")))
	if strings.ContainsAny(value, ":;,") {
		return `"` + value + `"`
	}
	return value
}

var textEscaper = strings.NewReplacer(
	`\`, `\\`,
	"\r\n", `\n`,
	"\r", `\n`,
	"\n", `\n`,
	";", `\;`,
	",", `\,`,
)

// EscapeText encodes s as an RFC 5545 TEXT value: backslashes, semicolons
// and commas are escaped, line breaks become \n, other control characters
// are dropped, and invalid UTF-8 is replaced with U+FFFD.
func EscapeText(s string) string {
	return textEscaper.Replace(stripControls(strings.ToValidUTF8(s, "�")))
}

// stripControls drops C0 control characters and DEL, keeping tab and the
// line breaks that the callers encode themselves.
func stripControls(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// Fold splits one content line into physical lines of at most 75 octets,
// as RFC 5545 section 3.1 requires: every line after the first starts with
// the space that marks it a continuation. No multi-byte character is split,
// and invalid UTF-8 is replaced with U+FFFD first, so every physical line is
// valid UTF-8. The lines carry no CRLF.
func Fold(line string) []string {
	line = strings.ToValidUTF8(line, "�")
	lines := make([]string, 0, 1+len(line)/maxLineOctets)
	prefix := ""
	limit := maxLineOctets
	for len(line) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(line[cut]) {
			cut--
		}
		if cut == 0 { // Unreachable for valid UTF-8; guards against a stall.
			cut = limit
		}
		lines = append(lines, prefix+line[:cut])
		line = line[cut:]
		prefix = " "
		limit = maxLineOctets - 1
	}
	return append(lines, prefix+line)
}

// OccurrenceUID derives a distinct UID for one occurrence of a series whose
// occurrences share uid. RFC 5545 forbids two events with the same UID unless
// a RECURRENCE-ID tells them apart; appending the start keeps each occurrence
// its own event.
func OccurrenceUID(uid string, start time.Time) string {
	return sanitizeUID(uid) + "-" + start.UTC().Format(utcFormat)
}

// HashUID derives a stable UID from the fields that identify an event, for
// sources that have no identifier of their own: the same parts always give
// the same UID, so a client re-importing the calendar updates the events it
// knows instead of duplicating them. domain should name the program, such as
// "example.com". The parts are hashed, so they do not leak into the file.
func HashUID(domain string, parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		// The length prefix keeps ("ab", "c") and ("a", "bc") apart.
		fmt.Fprintf(h, "%d:%s|", len(part), part)
	}
	uid := hex.EncodeToString(h.Sum(nil)[:16])
	if domain != "" {
		uid += "@" + domain
	}
	return uid
}

// sanitizeUID removes what a UID must not contain: control characters and
// whitespace.
func sanitizeUID(s string) string {
	return strings.Map(func(r rune) rune {
		if r <= 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, ""))
}

// Marshal returns the calendar encoded as an iCalendar file.
func (c Calendar) Marshal() ([]byte, error) {
	var b bytes.Buffer
	if _, err := c.WriteTo(&b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// WriteTo writes the calendar to w as an iCalendar file. It implements
// io.WriterTo. Nothing is written when the calendar or one of its events is
// incomplete.
func (c Calendar) WriteTo(w io.Writer) (int64, error) {
	if strings.TrimSpace(c.ProdID) == "" {
		return 0, ErrMissingProdID
	}
	for i, event := range c.Events {
		if sanitizeUID(event.UID) == "" {
			return 0, fmt.Errorf("event %d (%q): %w", i, event.Summary, ErrMissingUID)
		}
	}

	stamp := c.Stamp
	if stamp.IsZero() {
		stamp = time.Now()
	}
	enc := &encoder{w: w}
	enc.line("BEGIN:VCALENDAR")
	enc.line("VERSION:2.0")
	enc.line("PRODID:" + EscapeText(c.ProdID))
	enc.line("CALSCALE:" + valueOr(c.Scale, "GREGORIAN"))
	enc.line("METHOD:" + valueOr(c.Method, "PUBLISH"))
	if c.Name != "" {
		enc.line("X-WR-CALNAME:" + EscapeText(c.Name))
	}
	if c.TimeZone != nil {
		enc.line("X-WR-TIMEZONE:" + EscapeText(c.TimeZone.String()))
	}
	for _, p := range c.Props {
		enc.line(p.String())
	}
	for _, event := range c.Events {
		enc.event(event, stamp, c.TimeZone)
	}
	enc.line("END:VCALENDAR")
	return enc.n, enc.err
}

func valueOr(value, fallback string) string {
	if v := strings.ToUpper(strings.TrimSpace(value)); v != "" {
		return v
	}
	return fallback
}

// encoder writes folded, CRLF-terminated content lines and remembers the
// first error, so the component code reads as a list of lines.
type encoder struct {
	w   io.Writer
	n   int64
	err error
}

func (e *encoder) line(line string) {
	if e.err != nil {
		return
	}
	for _, physical := range Fold(line) {
		n, err := io.WriteString(e.w, physical+"\r\n")
		e.n += int64(n)
		if err != nil {
			e.err = err
			return
		}
	}
}

func (e *encoder) event(ev Event, calendarStamp time.Time, zone *time.Location) {
	stamp := ev.Stamp
	if stamp.IsZero() {
		stamp = calendarStamp
	}
	e.line("BEGIN:VEVENT")
	e.line("UID:" + sanitizeUID(ev.UID))
	e.line("DTSTAMP:" + stamp.UTC().Format(utcFormat))
	if ev.AllDay {
		start := dateIn(ev.Start, zone)
		end := start.AddDate(0, 0, 1)
		if !ev.End.IsZero() {
			end = dateIn(ev.End, zone)
		}
		e.line("DTSTART;VALUE=DATE:" + start.Format(dateFormat))
		e.line("DTEND;VALUE=DATE:" + end.Format(dateFormat))
	} else {
		e.line("DTSTART:" + ev.Start.UTC().Format(utcFormat))
		if !ev.End.IsZero() {
			e.line("DTEND:" + ev.End.UTC().Format(utcFormat))
		}
	}
	if ev.Summary != "" {
		e.line("SUMMARY:" + EscapeText(ev.Summary))
	}
	if ev.Location != "" {
		e.line("LOCATION:" + EscapeText(ev.Location))
	}
	if ev.Description != "" {
		e.line("DESCRIPTION:" + EscapeText(ev.Description))
	}
	if len(ev.Categories) > 0 {
		categories := make([]string, 0, len(ev.Categories))
		for _, category := range ev.Categories {
			if category != "" {
				categories = append(categories, EscapeText(category))
			}
		}
		if len(categories) > 0 {
			e.line("CATEGORIES:" + strings.Join(categories, ","))
		}
	}
	if ev.Status != "" {
		e.line("STATUS:" + strings.ToUpper(strings.TrimSpace(ev.Status)))
	}
	if ev.Transparent {
		e.line("TRANSP:TRANSPARENT")
	} else {
		e.line("TRANSP:OPAQUE")
	}
	if ev.Class != "" {
		e.line("CLASS:" + strings.ToUpper(strings.TrimSpace(ev.Class)))
	}
	if !ev.LastModified.IsZero() {
		e.line("LAST-MODIFIED:" + ev.LastModified.UTC().Format(utcFormat))
	}
	for _, p := range ev.Props {
		e.line(p.String())
	}
	e.line("END:VEVENT")
}

// dateIn returns the calendar date t falls on in zone, or in t's own
// location when zone is nil, as midnight UTC of that date.
func dateIn(t time.Time, zone *time.Location) time.Time {
	if zone != nil {
		t = t.In(zone)
	}
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
