package theme

import (
	"regexp"
	"strings"

	"github.com/mittwald/cli-kit/term"
)

// Rule paints text with Style: the parts Pattern matches, as
// [Style.PaintMatches] paints them, or the whole text when Pattern is nil.
type Rule struct {
	Pattern *regexp.Regexp
	Style   Style
}

// Rules paint text in turn, such as a column whose cells are painted in one
// color with the words that matter in another.
type Rules []Rule

// Paint applies the rules to text, in order. A rule with a pattern paints
// what it matches and keeps the colors around the match. A rule without
// one paints the whole text anew: it takes out the colors the text had,
// those of the rules before it too, but keeps its other escape sequences,
// such as the OSC 8 link of a cell. A rule with the zero Style paints
// nothing.
//
// Like [Style.Paint] it paints whatever it is given: ask term.Colored
// first. A text of several lines is painted as one: paint the lines one by
// one for a table cell, which is drawn line by line.
func (r Rules) Paint(text string) string {
	for _, rule := range r {
		if rule.Style == "" || text == "" {
			continue
		}
		if rule.Pattern == nil {
			text = rule.Style.Paint(stripColors(text))
			continue
		}
		text = rule.Style.PaintMatches(text, rule.Pattern)
	}
	return text
}

// stripColors removes the SGR sequences from text and keeps every other
// escape sequence.
func stripColors(text string) string {
	if !strings.Contains(text, "\x1b") {
		return text
	}
	var b strings.Builder
	for i := 0; i < len(text); {
		n := term.EscapeLen(text[i:])
		if n == 0 {
			b.WriteByte(text[i])
			i++
			continue
		}
		if _, ok := sgrParameters(text[i : i+n]); !ok {
			b.WriteString(text[i : i+n])
		}
		i += n
	}
	return b.String()
}

// HasColor reports whether text sets a color or a text attribute: whether
// it holds an SGR sequence other than the one that resets them all.
func HasColor(text string) bool {
	for i := strings.Index(text, "\x1b"); i >= 0 && i < len(text); {
		n := term.EscapeLen(text[i:])
		if n == 0 {
			i++
		} else {
			if params, ok := sgrParameters(text[i : i+n]); ok && strings.Trim(params, "0;") != "" {
				return true
			}
			i += n
		}
		next := strings.Index(text[i:], "\x1b")
		if next < 0 {
			return false
		}
		i += next
	}
	return false
}
