package pick

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

const (
	// columnGap separates the aligned cells of a row.
	columnGap = "  "
	// cursorMarker stands in front of the row under the cursor.
	cursorMarker = "❯ "
	// uncheckedBox and checkedBox are the boxes of a list that takes several
	// rows, and of a checklist.
	uncheckedBox = "[ ]"
	checkedBox   = "[x]"
	// separator joins the parts of a status line.
	separator = " · "
)

// width is how many terminal cells s occupies once drawn.
func width(s string) int { return term.VisibleWidth(s) }

// pad fills s with spaces to n cells.
func pad(s string, n int) string { return term.PadRight(s, n) }

// plainLine makes text a caller handed in safe to draw within one line. It is
// term.PlainLine without the folding of white space: escape sequences and
// control characters go, a line break or a tab becomes a space, and the
// characters that reorder text on screen are dropped, but every space stays
// where it is. The space a prompt ends in and the spaces inside the value of
// a text field are the caller's, and term.PlainLine would trim the one and
// fold the others.
//
// Every string a prompt draws passes through here once, where it enters, so
// the widths the layout counts are the widths the terminal draws.
func plainLine(s string) string {
	if printable(s) {
		return s
	}
	s = strings.Map(func(r rune) rune {
		switch {
		case lineBreak(r) || r == '\t':
			return ' '
		case bidiControl(r):
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, "\ufffd"))
	// Without a line break left in it, what term.Plain returns is one line.
	return term.Plain(s)
}

// plainLines is plainLine for every string of a slice, in a slice of its
// own, so the caller's is never written to.
func plainLines(lines []string) []string {
	if lines == nil {
		return nil
	}
	plain := make([]string, len(lines))
	for i, line := range lines {
		plain[i] = plainLine(line)
	}
	return plain
}

// printable reports whether s is drawn as it is written: valid UTF-8 made of
// nothing but graphic characters and spaces. It is the way past plainLine for
// the text that needs nothing done to it, which is nearly all of it.
func printable(s string) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c >= 0x7f {
			return printableRunes(s)
		}
	}
	return true
}

func printableRunes(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) && !unicode.Is(unicode.Zs, r) {
			return false
		}
	}
	return true
}

// lineBreak reports whether r breaks a line: LF, VT, FF, CR, NEL, or the
// Unicode line and paragraph separators. It is the set term.PlainLine turns
// into spaces.
func lineBreak(r rune) bool {
	return (r >= '\n' && r <= '\r') || r == '\u0085' || r == '\u2028' || r == '\u2029'
}

// bidiControl reports whether r is an explicit bidirectional formatting
// character: the embeddings and overrides U+202A to U+202E, and the isolates
// U+2066 to U+2069. It is the set term.PlainLine drops.
func bidiControl(r rune) bool {
	return (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069')
}

// typable reports whether r can be part of what is typed into a prompt: any
// character plainLine would leave alone. A control character pasted into a
// filter or a text field is dropped rather than drawn.
func typable(r rune) bool {
	return !unicode.IsControl(r) && !lineBreak(r) && !bidiControl(r)
}

// cut shortens s to n cells, marking the cut with an ellipsis. Unlike
// term.Truncate, a width of 0 or less leaves nothing: a prompt line has to fit
// the terminal, and a line that wrapped would desynchronize the in-place
// redraw from the screen.
func cut(s string, n int) string {
	switch {
	case n <= 0:
		return ""
	case width(s) <= n:
		return s
	case n == 1:
		return term.Ellipsis
	}
	return term.Truncate(s, n)
}

// cutMiddle shortens s to n cells by taking text out of its middle.
func cutMiddle(s string, n int) string {
	switch {
	case n <= 0:
		return ""
	case width(s) <= n:
		return s
	case n == 1:
		return term.Ellipsis
	}
	return term.TruncateMiddle(s, n)
}

// painter paints the pieces of a block in the theme's styles, or leaves them
// plain when color is off.
type painter struct {
	color bool
	theme theme.Theme
}

// paint wraps text in style. Blank text carries no color: no row has a
// background to fill.
func (p painter) paint(style theme.Style, text string) string {
	if !p.color || style == "" || strings.TrimSpace(text) == "" {
		return text
	}
	return style.Paint(text)
}

// styledText is a piece of a line and the style it is drawn in, "" for the
// terminal's own.
type styledText struct {
	text  string
	style theme.Style
}

// choicesPattern matches the answers a question spells out, such as [y/N]:
// two or more words between brackets, separated by slashes.
var choicesPattern = regexp.MustCompile(`\[[^\[\]/\s]+(?:/[^\[\]/\s]+)+\]`)

// promptParts cuts a question into the styles its pieces are drawn in: the
// subject where the question holds %s for it, the answers it spells out
// receding, and the rest plain.
func promptParts(prompt, subject string, th theme.Theme) []styledText {
	before, after, found := strings.Cut(prompt, "%s")
	if !found {
		return choiceParts(prompt, "", th)
	}
	parts := append(choiceParts(before, "", th), styledText{subject, th.Subject})
	return append(parts, choiceParts(after, "", th)...)
}

// titleParts is promptParts for the title above a list or a set of switches.
// A title without a subject is drawn in the theme's title style as a whole;
// one with a subject leaves the weight to the subject.
func titleParts(title, subject string, th theme.Theme) []styledText {
	if strings.Contains(title, "%s") {
		return promptParts(title, subject, th)
	}
	return choiceParts(title, th.Title, th)
}

func choiceParts(text string, style theme.Style, th theme.Theme) []styledText {
	var parts []styledText
	last := 0
	for _, match := range choicesPattern.FindAllStringIndex(text, -1) {
		parts = append(parts, styledText{text[last:match[0]], style}, styledText{text[match[0]:match[1]], th.Qualifier})
		last = match[1]
	}
	return append(parts, styledText{text[last:], style})
}

// partsText is the parts as plain text, the way they read once drawn.
func partsText(parts []styledText) string {
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(part.text)
	}
	return b.String()
}

// cutParts shortens the parts to n cells in the order they read, dropping
// the ones the cut leaves nothing of.
func cutParts(parts []styledText, n int) []styledText {
	fitted := make([]styledText, 0, len(parts))
	left := n
	for _, part := range parts {
		if left <= 0 {
			break
		}
		text := cut(part.text, left)
		left -= width(text)
		fitted = append(fitted, styledText{text, part.style})
	}
	return fitted
}

// drawParts writes the parts in their styles, cut to n cells in the order
// they read.
func drawParts(parts []styledText, n int, p painter) string {
	var b strings.Builder
	for _, part := range cutParts(parts, n) {
		b.WriteString(p.paint(part.style, part.text))
	}
	return b.String()
}

// span is a stretch of a cell painted as one, and whether it is bracketed.
type span struct {
	text  string
	paren bool
}

// parenSpans cuts a cell into what is inside parentheses and what is not, in
// reading order. The parentheses belong to what they enclose, nested ones do
// not start a second span, and one that is never closed takes the rest of the
// cell with it.
func parenSpans(cell string) []span {
	var spans []span
	depth, start := 0, 0
	for i, r := range cell {
		switch {
		case r == '(' && depth == 0:
			if i > start {
				spans = append(spans, span{text: cell[start:i]})
			}
			start, depth = i, 1
		case r == '(':
			depth++
		case r == ')' && depth == 1:
			spans = append(spans, span{text: cell[start : i+1], paren: true})
			start, depth = i+1, 0
		case r == ')' && depth > 1:
			depth--
		}
	}
	if start < len(cell) {
		spans = append(spans, span{text: cell[start:], paren: depth > 0})
	}
	return spans
}

// fitHint returns long when it fits n cells, else short cut to n cells: a
// truncated key list is worse than a shorter complete one.
func fitHint(long, short string, n int) string {
	if width(long) <= n {
		return long
	}
	return cut(short, n)
}

func firstNonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
