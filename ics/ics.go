package ics

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"slices"
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

	// The fields below choose how events are encoded. Their zero values give
	// the package's defaults. Set them to suit a consumer, or to keep the
	// bytes of a feed that another writer produced until now: subscribed
	// clients compare a feed between polls, so a feed that changes form looks
	// as if every event had been edited.

	// EventPropertyOrder lists property names, such as "ORGANIZER", in the
	// order they are written in every event; names are matched regardless of
	// case. Properties it names come first, in its order. The others follow
	// in the default order: UID, DTSTAMP, DTSTART, DTEND, SUMMARY, LOCATION,
	// DESCRIPTION, CATEGORIES, STATUS, TRANSP, CLASS, LAST-MODIFIED and then
	// [Event.Props] as given. Properties that share a name, such as several
	// ATTENDEE, keep their order among themselves. RFC 5545 gives properties
	// no order, so use it to match an existing feed or to put the properties
	// a reader looks for first, such as ORGANIZER and ATTENDEE before STATUS.
	EventPropertyOrder []string
	// MissingEnd says how an event with a zero End is written. The zero
	// value, [EndDefault], leaves DTEND out of a timed event and writes the
	// day after the start for an all-day event.
	MissingEnd EndMode
	// VerbatimUIDs writes every [Event.UID] as given: only the control
	// characters RFC 5545 allows in no content line, the C0 controls but tab
	// and DEL, are removed, and invalid UTF-8 is replaced with U+FFFD. The
	// C1 controls, which RFC 5545 allows, stay, so a UID keeps the form a
	// feed published. By default, whitespace and the C1 controls are removed
	// too. Set it when the UIDs come from a scheme of the caller's own that
	// has to stay stable even where it lets whitespace or C1 controls
	// through, such as UIDs a feed has published before: clients key events
	// by UID, so a changed UID duplicates the event in every subscribed
	// client. A UID that is empty or only whitespace is still
	// [ErrMissingUID].
	VerbatimUIDs bool
}

// EndMode says how an event whose End is zero is written. See
// [Calendar.MissingEnd].
type EndMode int

const (
	// EndDefault leaves DTEND out of a timed event, which then ends at its
	// start, and writes the day after the start as the end of an all-day
	// event, which then lasts one day. Values other than the constants below
	// act as EndDefault.
	EndDefault EndMode = iota
	// EndOmit leaves DTEND out of every event without an End. RFC 5545 reads
	// an all-day event without DTEND as lasting one day, so this means the
	// same as EndDefault in one line less; use it where a consumer expects
	// no DTEND it was not given.
	EndOmit
	// EndDerive writes the end RFC 5545 implies: the start of a timed event,
	// the day after the start of an all-day event. Use it for consumers that
	// require DTEND.
	EndDerive
	// EndAsGiven writes the zero End like any other: 00010101T000000Z for a
	// timed event, the date the zero time falls on for an all-day event. No
	// client reads that as a sensible end; use it only to keep the bytes of
	// a feed whose earlier writer wrote the zero time.
	EndAsGiven
)

// Event is one VEVENT component.
type Event struct {
	// UID identifies the event across exports and is required. Whitespace
	// and control characters are removed, unless [Calendar.VerbatimUIDs] is
	// set. Occurrences of the same series need distinct UIDs, see
	// [OccurrenceUID].
	UID string
	// Stamp is the event's DTSTAMP; zero falls back to [Calendar.Stamp].
	Stamp time.Time
	// Start is when the event begins.
	Start time.Time
	// End is when the event ends, exclusive. By default, zero leaves DTEND
	// out, which makes a timed event end at its start, and makes an all-day
	// event last a day; [Calendar.MissingEnd] chooses otherwise.
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
	// X- properties, written after the standard ones unless
	// [Calendar.EventPropertyOrder] places them.
	Props []Property
}

// Param is one property parameter, such as CN="Jane Doe".
type Param struct {
	Name  string
	Value string
	// Quote writes Value as a quoted-string even when it needs none. By
	// default a value is quoted only when it contains ":", ";" or ",", which
	// is all RFC 5545 asks for. Set it for free-text parameters such as CN
	// to match a writer that always quotes them, or so a value keeps one form
	// whatever it contains.
	Quote bool
	// LineBreak, when not empty, replaces each CR and each LF in Value
	// before the value is encoded, so a CRLF pair is replaced twice. Empty
	// keeps line breaks and encodes each, a CRLF pair counted as one, as
	// RFC 6868's "^n". Set it, typically to " ", for consumers that do not
	// decode RFC 6868 and would show "^n" as it stands. The replacement is
	// encoded like the rest of the value.
	LineBreak string
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
// Parameter values are caret encoded as RFC 6868 describes and quoted when
// they contain ":", ";" or ",", so they cannot end the parameter early;
// [Param.Quote] and [Param.LineBreak] change that per parameter. Control
// characters in them are dropped, line breaks and tab aside, as
// [EscapeText] drops them.
func (p Property) String() string {
	var b strings.Builder
	b.WriteString(strings.ToUpper(p.Name))
	for _, param := range p.Params {
		b.WriteByte(';')
		b.WriteString(strings.ToUpper(param.Name))
		b.WriteByte('=')
		b.WriteString(param.encode())
	}
	b.WriteByte(':')
	b.WriteString(p.Value)
	return b.String()
}

var paramEscaper = strings.NewReplacer("^", "^^", "\r\n", "^n", "\n", "^n", "\r", "^n", `"`, "^'")

// encode returns the parameter's value as written (RFC 5545 section 3.2,
// RFC 6868).
func (p Param) encode() string {
	value := strings.ToValidUTF8(p.Value, "�")
	if p.LineBreak != "" {
		lineBreak := strings.ToValidUTF8(p.LineBreak, "�")
		value = strings.NewReplacer("\r", lineBreak, "\n", lineBreak).Replace(value)
	}
	value = paramEscaper.Replace(stripControls(value))
	if p.Quote || strings.ContainsAny(value, ":;,") {
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
// and commas are escaped, line breaks become \n, the other control
// characters but tab are dropped (the C0 controls, DEL and the C1 controls),
// and invalid UTF-8 is replaced with U+FFFD. RFC 5545 allows the C1
// controls, but no calendar shows them, and a terminal the file is printed
// on takes U+009B for ESC [ and U+009D for ESC ].
func EscapeText(s string) string {
	return textEscaper.Replace(stripControls(strings.ToValidUTF8(s, "�")))
}

// stripControls drops the control characters text has no use for, keeping
// tab and the line breaks that the callers encode themselves.
func stripControls(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || !textControl(r) {
			return r
		}
		return -1
	}, s)
}

// isControl reports whether r is a character RFC 5545 allows in no content
// line: a C0 control other than tab, or DEL.
func isControl(r rune) bool {
	return (r < 0x20 && r != '\t') || r == 0x7f
}

// textControl reports whether r is a control character text has no use for:
// one RFC 5545 allows in no content line, or a C1 control.
func textControl(r rune) bool {
	return isControl(r) || isC1(r)
}

// isC1 reports whether r is a C1 control, U+0080 to U+009F.
func isC1(r rune) bool {
	return r >= 0x80 && r <= 0x9f
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

// sanitizeUID removes what a UID must not contain: control characters (C0,
// DEL and C1) and the ASCII space.
func sanitizeUID(s string) string {
	return strings.Map(func(r rune) rune {
		if r <= 0x20 || r == 0x7f || isC1(r) {
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
		if _, ok := c.uid(event.UID); !ok {
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
		enc.line("BEGIN:VEVENT")
		for _, p := range c.eventProperties(event, stamp) {
			enc.line(p.String())
		}
		enc.line("END:VEVENT")
	}
	enc.line("END:VCALENDAR")
	return enc.n, enc.err
}

// uid returns the UID written for the event UID raw, and false when that
// leaves no UID.
func (c Calendar) uid(raw string) (string, bool) {
	if c.VerbatimUIDs {
		uid := strings.Map(func(r rune) rune {
			if isControl(r) {
				return -1
			}
			return r
		}, strings.ToValidUTF8(raw, "�"))
		return uid, strings.TrimSpace(uid) != ""
	}
	uid := sanitizeUID(raw)
	return uid, uid != ""
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

// dateValue marks a DTSTART or DTEND value as a DATE.
var dateValue = Param{Name: "VALUE", Value: "DATE"}

// eventProperties returns the content lines of ev in the order they are
// written.
func (c Calendar) eventProperties(ev Event, calendarStamp time.Time) []Property {
	stamp := ev.Stamp
	if stamp.IsZero() {
		stamp = calendarStamp
	}
	props := make([]Property, 0, 12+len(ev.Props))
	add := func(name, value string, params ...Param) {
		props = append(props, Property{Name: name, Params: params, Value: value})
	}

	uid, _ := c.uid(ev.UID)
	add("UID", uid)
	add("DTSTAMP", stamp.UTC().Format(utcFormat))
	// A zero End is written as given with EndAsGiven, and otherwise left out
	// or derived from the start.
	writeEnd := !ev.End.IsZero() || c.MissingEnd == EndAsGiven
	if ev.AllDay {
		start := dateIn(ev.Start, c.TimeZone)
		add("DTSTART", start.Format(dateFormat), dateValue)
		switch {
		case writeEnd:
			add("DTEND", dateIn(ev.End, c.TimeZone).Format(dateFormat), dateValue)
		case c.MissingEnd != EndOmit:
			add("DTEND", start.AddDate(0, 0, 1).Format(dateFormat), dateValue)
		}
	} else {
		add("DTSTART", ev.Start.UTC().Format(utcFormat))
		switch {
		case writeEnd:
			add("DTEND", ev.End.UTC().Format(utcFormat))
		case c.MissingEnd == EndDerive:
			add("DTEND", ev.Start.UTC().Format(utcFormat))
		}
	}
	if ev.Summary != "" {
		add("SUMMARY", EscapeText(ev.Summary))
	}
	if ev.Location != "" {
		add("LOCATION", EscapeText(ev.Location))
	}
	if ev.Description != "" {
		add("DESCRIPTION", EscapeText(ev.Description))
	}
	if len(ev.Categories) > 0 {
		categories := make([]string, 0, len(ev.Categories))
		for _, category := range ev.Categories {
			if category != "" {
				categories = append(categories, EscapeText(category))
			}
		}
		if len(categories) > 0 {
			add("CATEGORIES", strings.Join(categories, ","))
		}
	}
	if ev.Status != "" {
		add("STATUS", strings.ToUpper(strings.TrimSpace(ev.Status)))
	}
	if ev.Transparent {
		add("TRANSP", "TRANSPARENT")
	} else {
		add("TRANSP", "OPAQUE")
	}
	if ev.Class != "" {
		add("CLASS", strings.ToUpper(strings.TrimSpace(ev.Class)))
	}
	if !ev.LastModified.IsZero() {
		add("LAST-MODIFIED", ev.LastModified.UTC().Format(utcFormat))
	}
	props = append(props, ev.Props...)
	orderProperties(props, c.EventPropertyOrder)
	return props
}

// orderProperties sorts props by the position of their names in order,
// keeping the order of properties with the same position. Properties whose
// names order does not list go after those it does.
func orderProperties(props []Property, order []string) {
	if len(order) == 0 {
		return
	}
	rank := make(map[string]int, len(order))
	for i, name := range order {
		name = strings.ToUpper(name)
		if _, seen := rank[name]; !seen {
			rank[name] = i
		}
	}
	position := func(p Property) int {
		if i, ok := rank[strings.ToUpper(p.Name)]; ok {
			return i
		}
		return len(order)
	}
	slices.SortStableFunc(props, func(a, b Property) int {
		return cmp.Compare(position(a), position(b))
	})
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
