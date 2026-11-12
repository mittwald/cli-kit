package term

import (
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// tabStop is how far apart the tab stops are that [Sanitize] expands tabs
// to, a terminal's default.
const tabStop = 8

// Sanitize makes text from elsewhere — a CI job's log, a remote command's
// output, a message somebody wrote — safe to write to a terminal. Escape
// sequences in such text can do more than color it: move the cursor over
// what was written before, retitle the window, set the clipboard, disguise a
// link, or leave the terminal in a mode it does not get out of. Sanitize
// keeps what colors text and takes out everything else:
//
//   - SGR sequences are kept, with only the attributes that change how text
//     looks and leave it readable: bold, dim, italic, underline, blink,
//     reverse, strikethrough, overline, foreground and background colors
//     (16-color, 256-color and 24-bit) and underline colors (256-color and
//     24-bit). Concealed text, fonts, frames and the like are dropped, and a
//     sequence left with nothing is removed.
//   - Every other escape sequence is removed, and so is every control
//     character but the newline: the C0 and C1 controls and DEL.
//   - A tab becomes the spaces up to the next tab stop, every 8 columns.
//   - A carriage return ends what came before it on its line, the way a
//     progress meter that redraws its line leaves only its last state on
//     screen: only the text after the line's last one is kept, in the colors
//     the text before it set. Carriage returns ending a line, as in CRLF,
//     are dropped.
//   - Bytes that are not valid UTF-8 become U+FFFD.
//
// Newlines are kept, so a color carries from one line into the next as the
// text set it. [SanitizeLines] makes every line stand on its own instead.
func Sanitize(s string) string {
	if safe(s) {
		return s
	}
	lines := strings.Split(strings.ToValidUTF8(s, "�"), "\n")
	for i, line := range lines {
		lines[i] = sanitizeLine(line)
	}
	return strings.Join(lines, "\n")
}

// SanitizeLines is [Sanitize] split into lines that each stand on their own:
// a line starts with the colors and attributes the lines before it left set,
// and a line that leaves any set ends with a reset. Every line can then be shown
// apart from the rest, cut short or left out — the last lines of a log under
// a status board, say — without taking its colors into what follows it. A
// newline at the end of s ends its last line rather than starting another,
// and an empty s has no lines.
func SanitizeLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(strings.ToValidUTF8(s, "�"), "\n")
	lines := strings.Split(s, "\n")
	var carried rendition
	for i, line := range lines {
		line = sanitizeLine(line)
		shown := carried.sequence() + line
		carried.applyText(line)
		if carried.sequence() != "" {
			shown += sgrReset
		}
		lines[i] = shown
	}
	return lines
}

// sanitizeLine is Sanitize for one line, without its newline.
func sanitizeLine(line string) string {
	line = strings.TrimRight(line, "\r")
	var (
		// b holds the text since the last carriage return.
		b strings.Builder
		// set is what the line's SGR sequences have changed so far, and
		// overwritten what they had changed by the last carriage return.
		set         = rendition{relative: true}
		overwritten string
		column      int
	)
	for i := 0; i < len(line); {
		if n := EscapeLen(line[i:]); n > 0 {
			seq := line[i : i+n]
			i += n
			params, ok := sgrParams(seq)
			if !ok {
				continue
			}
			kept, dropped := parseSGR(params)
			if len(kept) == 0 {
				continue
			}
			if dropped {
				texts := make([]string, len(kept))
				for j, param := range kept {
					texts[j] = param.text
				}
				seq = "\x1b[" + strings.Join(texts, ";") + "m"
			}
			b.WriteString(seq)
			set.apply(kept)
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		i += size
		switch {
		case r == '\r':
			b.Reset()
			overwritten = set.sequence()
			column = 0
		case r == '\t':
			spaces := tabStop - column%tabStop
			b.WriteString(strings.Repeat(" ", spaces))
			column += spaces
		case control(r):
		default:
			b.WriteRune(r)
			column += runewidth.RuneWidth(r)
		}
	}
	return overwritten + b.String()
}

// control reports whether r is a control character: C0, DEL or C1.
func control(r rune) bool {
	return r < 0x20 || (r >= 0x7f && r <= 0x9f)
}

// safe reports whether s has nothing Sanitize would change: no control
// character but the newline, and valid UTF-8 throughout.
func safe(s string) bool {
	for _, r := range s {
		if r == utf8.RuneError || (control(r) && r != '\n') {
			return false
		}
	}
	return true
}
