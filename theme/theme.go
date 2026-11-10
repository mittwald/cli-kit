package theme

import (
	"strconv"
	"strings"
	"sync/atomic"
)

// Style is a set of SGR parameters, such as "1" (bold) or "38;5;141" (a
// 256-color foreground). The zero Style paints nothing.
type Style string

// Reset ends every SGR attribute.
const Reset = "\x1b[0m"

// Common attributes.
const (
	Bold      Style = "1"
	Dim       Style = "2"
	Italic    Style = "3"
	Underline Style = "4"
	Reverse   Style = "7"
)

// Paint wraps text in s and a reset. It returns text unchanged when s is
// empty or text is empty, so it is safe to call unconditionally.
func (s Style) Paint(text string) string {
	if s == "" || text == "" {
		return text
	}
	return "\x1b[" + string(s) + "m" + text + Reset
}

// PaintIf paints text only when on is true, usually the result of
// term.Colored for the writer the text goes to.
func (s Style) PaintIf(on bool, text string) string {
	if !on {
		return text
	}
	return s.Paint(text)
}

// PaintLines paints every line of text on its own, so a terminal that
// redraws a single line keeps its color and a newline never carries a
// color into the next line.
func (s Style) PaintLines(text string) string {
	if s == "" || text == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = s.Paint(line)
	}
	return strings.Join(lines, "\n")
}

// Sequence returns the escape sequence that switches s on, or "" for the
// zero Style.
func (s Style) Sequence() string {
	if s == "" {
		return ""
	}
	return "\x1b[" + string(s) + "m"
}

// Join combines styles into one, skipping empty ones: Join(Bold, Fg256(141)).
func Join(styles ...Style) Style {
	parts := make([]string, 0, len(styles))
	for _, s := range styles {
		if s != "" {
			parts = append(parts, string(s))
		}
	}
	return Style(strings.Join(parts, ";"))
}

// Fg256 is a foreground color from the 256-color palette.
func Fg256(n uint8) Style { return Style("38;5;" + strconv.Itoa(int(n))) }

// Bg256 is a background color from the 256-color palette.
func Bg256(n uint8) Style { return Style("48;5;" + strconv.Itoa(int(n))) }

// RGB is a 24-bit foreground color.
func RGB(r, g, b uint8) Style {
	return Style("38;2;" + strconv.Itoa(int(r)) + ";" + strconv.Itoa(int(g)) + ";" + strconv.Itoa(int(b)))
}

// BgRGB is a 24-bit background color.
func BgRGB(r, g, b uint8) Style {
	return Style("48;2;" + strconv.Itoa(int(r)) + ";" + strconv.Itoa(int(g)) + ";" + strconv.Itoa(int(b)))
}

// Theme assigns a style to each role text plays on screen.
type Theme struct {
	// Title is a heading, or the question a prompt asks.
	Title Style
	// Subject is the thing a title is about, drawn inside it.
	Subject Style
	// Header is a table's header row.
	Header Style
	// Border is a table's rules and frame.
	Border Style
	// Muted recedes text: notes, key hints, detail lines, disabled rows.
	Muted Style
	// Qualifier is secondary text beside a value, quieter than the value but
	// louder than Muted.
	Qualifier Style
	// Value is an answer or a typed value.
	Value Style
	// Detail is the full value of a shortened item, shown beside a title.
	Detail Style
	// Selected is the row or item under the cursor.
	Selected Style
	// SelectedQualifier is Qualifier on the selected row.
	SelectedQualifier Style
	// Action is what choosing a row does.
	Action Style
	// SelectedAction is Action on the selected row.
	SelectedAction Style
	// Accent marks something worth noticing, such as a marker column.
	Accent Style
	// Flag marks something worth stopping at.
	Flag Style
	// Info, Success, Warning and Error color messages by severity.
	Info    Style
	Success Style
	Warning Style
	Error   Style
}

// Default is the theme the packages use unless [Use] installs another. It
// sticks to the 256-color palette, which every common terminal supports,
// and to mid-luminance tones that read on dark and light backgrounds.
var Default = Theme{
	Title:             Bold,
	Subject:           Bold,
	Header:            Join(Bold, Fg256(252)),
	Border:            Fg256(240),
	Muted:             Fg256(244),
	Qualifier:         Fg256(250),
	Value:             Fg256(141),
	Detail:            Fg256(117),
	Selected:          Join(Bold, Fg256(141)),
	SelectedQualifier: Fg256(146),
	Action:            Fg256(73),
	SelectedAction:    Join(Bold, Fg256(79)),
	Accent:            Fg256(214),
	Flag:              Fg256(203),
	Info:              Fg256(117),
	Success:           Fg256(114),
	Warning:           Fg256(221),
	Error:             Fg256(203),
}

// Plain paints nothing. Installing it keeps escape sequences out of output
// even where term.Colored would allow them.
var Plain = Theme{}

var active atomic.Pointer[Theme]

// Use installs t as the active theme for the whole process. Call it once at
// start-up, before any output is drawn.
func Use(t Theme) { active.Store(&t) }

// Active returns the theme [Use] installed, or [Default].
func Active() Theme {
	if t := active.Load(); t != nil {
		return *t
	}
	return Default
}
