package term

import "strings"

// Wrap breaks the lines of s at spaces so that each fits into width cells,
// and starts every line after the first with indent, a hanging indent.
// Wrap(text, 40, "         ") gives
//
//	Dry run: would tag 8.4.25 and push it,
//	         then wait for its pipeline.
//
// A line that fits is left as it is, and so are the line breaks s already
// has; the lines after them are indented too. A line keeps its leading
// spaces, and a line Wrap starts has indent alone. A word wider than the
// room left for it gets a line of its own and stays whole. Widths are
// visible widths, as [VisibleWidth] measures them, so escape sequences take
// up none, and a tab takes up the cells to the next tab stop from where its
// word stands on the line; a tab is part of a word and no place to break. A
// width of 0 or less, what [Size] reports for anything but a terminal, breaks
// nothing and only indents.
func Wrap(s string, width int, indent string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if i > 0 {
			line = indent + line
		}
		lines[i] = wrapLine(line, width, indent)
	}
	return strings.Join(lines, "\n")
}

// wrapLine breaks one line, without a newline, as Wrap does.
func wrapLine(line string, width int, indent string) string {
	if width <= 0 || VisibleWidth(line) <= width {
		return line
	}
	rest := strings.TrimLeft(line, " ")
	lead := line[:len(line)-len(rest)]
	var b strings.Builder
	b.WriteString(lead)
	column := VisibleWidth(lead)
	// pending are spaces before an escape sequence standing alone, such as a
	// leading color or a trailing reset: they belong before the next visible
	// word, and only a visible word ends the first one's place on the line.
	pending := 0
	first := true
	for {
		spaces := len(rest) - len(strings.TrimLeft(rest, " "))
		rest = rest[spaces:]
		spaces += pending
		pending = 0
		if rest == "" {
			// Spaces at the end of a line that is broken anyway go.
			return b.String()
		}
		word := rest[:wordEnd(rest)]
		rest = rest[len(word):]
		switch {
		case WidthAt(word, 0) == 0:
			// Nothing to see: it stays where it is and cannot break the line.
			b.WriteString(word)
			pending = spaces
			continue
		case first:
			// Spaces after a leading escape sequence lead the line, as they
			// would without it.
			b.WriteString(strings.Repeat(" ", spaces))
			column += spaces
		case column+spaces+WidthAt(word, column+spaces) > width:
			b.WriteString("\n" + indent)
			column = VisibleWidth(indent)
		default:
			b.WriteString(strings.Repeat(" ", spaces))
			column += spaces
		}
		b.WriteString(word)
		// A word with a tab in it is as wide as the column it starts at
		// makes it.
		column += WidthAt(word, column)
		first = false
	}
}

// wordEnd returns where the word s starts with ends: at the first space
// outside an escape sequence, or at the end of s.
func wordEnd(s string) int {
	for i := 0; i < len(s); {
		if n := EscapeLen(s[i:]); n > 0 {
			i += n
			continue
		}
		if s[i] == ' ' {
			return i
		}
		i++
	}
	return len(s)
}
