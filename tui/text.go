package tui

import (
	"strings"

	"github.com/rivo/uniseg"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// Span is a piece of text in one style.
type Span struct {
	Text  string
	Style theme.Style
}

// Line is a line of text made of spans.
type Line []Span

// Text returns a line of s in one style.
func Text(s string, style theme.Style) Line { return Line{{Text: s, Style: style}} }

// Width is how many cells the line takes up.
func (l Line) Width() int {
	n := 0
	for _, s := range l {
		n += textWidth(s.Text)
	}
	return n
}

// String is the text of the line without its styles.
func (l Line) String() string {
	var b strings.Builder
	for _, s := range l {
		b.WriteString(s.Text)
	}
	return b.String()
}

// Badge returns s as a badge: padded with a space on either side, for a
// style with a background, such as a label or a priority.
func Badge(s string, style theme.Style) Span {
	return Span{Text: " " + term.PlainLine(s) + " ", Style: style}
}

// oneLine makes s safe to show on one line without folding its spaces, as
// term.PlainLine does: escape sequences go, line breaks and tabs become
// spaces. Padding a caller put in, such as of a column, stays.
func oneLine(s string) string {
	s = term.StripANSI(term.Sanitize(s))
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t' || r == '\v' || r == '\f' || r == 0x85 || r == 0x2028 || r == 0x2029:
			return ' '
		case bidiControl(r):
			return -1
		}
		return r
	}, s)
}

// grapheme is one grapheme cluster and the cells it takes up.
type grapheme struct {
	text  string
	width int
}

// graphemes splits s into its grapheme clusters, leaving out the clusters
// that would not draw what they are: control characters and the characters
// that reorder text. A tab becomes a space.
func graphemes(s string) []grapheme {
	var out []grapheme
	state := -1
	for s != "" {
		var cluster string
		cluster, s, _, state = uniseg.FirstGraphemeClusterInString(s, state)
		if cluster == "\t" {
			out = append(out, grapheme{text: " ", width: 1})
			continue
		}
		if !drawable(cluster) {
			continue
		}
		out = append(out, grapheme{text: cluster, width: term.VisibleWidth(cluster)})
	}
	return out
}

// drawable reports whether cluster draws itself rather than moving the
// cursor, changing the terminal's state or reordering text.
func drawable(cluster string) bool {
	for _, r := range cluster {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) || bidiControl(r) {
			return false
		}
	}
	return true
}

// textWidth is how many cells s takes up as a canvas draws it.
func textWidth(s string) int {
	n := 0
	for _, g := range graphemes(s) {
		n += g.width
	}
	return n
}

// truncate cuts s to width cells, ending it in an ellipsis when it was cut.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if textWidth(s) <= width {
		return s
	}
	var b strings.Builder
	used := 0
	for _, g := range graphemes(s) {
		if used+g.width > width-1 {
			break
		}
		b.WriteString(g.text)
		used += g.width
	}
	b.WriteString(term.Ellipsis)
	return b.String()
}

// TruncateLine cuts a line to width cells, ending it in an ellipsis in the
// style of the span that was cut.
func TruncateLine(l Line, width int) Line {
	if l.Width() <= width {
		return l
	}
	if width <= 0 {
		return nil
	}
	// The line does not fit, so a cell is kept for the ellipsis wherever
	// the cut falls, also between two spans.
	room := width - 1
	var out Line
	used := 0
	for _, s := range l {
		var b strings.Builder
		cut := false
		for _, g := range graphemes(s.Text) {
			if used+g.width > room {
				cut = true
				break
			}
			b.WriteString(g.text)
			used += g.width
		}
		if cut || used == room {
			out = append(out, Span{Text: b.String() + term.Ellipsis, Style: s.Style})
			return out
		}
		out = append(out, Span{Text: b.String(), Style: s.Style})
	}
	return out
}

// WrapLine breaks a line into lines of at most width cells, at spaces where
// it can and inside a word where the word is wider than a line. Styles carry
// over to the lines they continue on. A width below 1 is taken as 1.
func WrapLine(l Line, width int) []Line {
	width = max(width, 1)
	type cell struct {
		g     grapheme
		style theme.Style
	}
	var cells []cell
	for _, s := range l {
		for _, g := range graphemes(s.Text) {
			cells = append(cells, cell{g, s.Style})
		}
	}
	if len(cells) == 0 {
		return []Line{nil}
	}
	var lines []Line
	start := 0
	for start < len(cells) {
		used, end, lastSpace := 0, start, -1
		for end < len(cells) && used+cells[end].g.width <= width {
			if cells[end].g.text == " " {
				lastSpace = end
			}
			used += cells[end].g.width
			end++
		}
		next := end
		if end < len(cells) {
			switch {
			case cells[end].g.text == " ":
				next = end + 1
			case lastSpace > start:
				end, next = lastSpace, lastSpace+1
			case end == start:
				// A cluster wider than the line: it goes on a line of its
				// own rather than holding the wrapping up.
				end, next = start+1, start+1
			}
		}
		var line Line
		for i := start; i < end; i++ {
			c := cells[i]
			if n := len(line); n > 0 && line[n-1].Style == c.style {
				line[n-1].Text += c.g.text
				continue
			}
			line = append(line, Span{Text: c.g.text, Style: c.style})
		}
		lines = append(lines, line)
		start = next
	}
	return lines
}

// WrapText breaks text, which may hold line breaks, into lines of at most
// width cells, in one style. The text is made safe to show first with
// term.Plain, so text from elsewhere can be passed as it is.
func WrapText(text string, width int, style theme.Style) []Line {
	var out []Line
	for line := range strings.SplitSeq(term.Plain(text), "\n") {
		out = append(out, WrapLine(Text(line, style), width)...)
	}
	return out
}
