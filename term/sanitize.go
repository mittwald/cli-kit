package term

import (
	"strings"
	"unicode/utf8"
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
//   - The 8-bit forms of escape sequences, which start with a C1 control
//     instead of ESC, are removed with what belongs to them, so a clipboard
//     write such as U+009D 52;c;… BEL does not leave its payload behind as
//     text. A CSI (U+009B) goes with its parameters and final byte, an SGR
//     one too: most terminals that read UTF-8 do not take it for a sequence,
//     so its colors would not show there anyway. An OSC, DCS, SOS, PM or APC
//     string (U+009D, U+0090, U+0098, U+009E, U+009F) goes up to its
//     terminator, BEL, ESC \ or ST (U+009C), when that ends it on the same
//     line. Without a terminator on its line, or for a CSI that is not well
//     formed, only the C1 control itself goes: text decoded as Latin-1
//     although it was Windows-1252 has C1 controls where it meant letters
//     (U+009E for ž), and the rest of such a text must not be taken for the
//     payload of a sequence.
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
	lines := strings.Split(dropStrings(strings.ToValidUTF8(s, "�")), "\n")
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
	s = strings.TrimSuffix(dropStrings(strings.ToValidUTF8(s, "�")), "\n")
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

// Plain is [Sanitize] for text shown as plain text: the colors Sanitize keeps
// go too, so the result holds no escape sequence at all, and no control
// character but the newline. Use it for text from elsewhere that the program
// paints itself or not at all — a server's message, a name, a comment
// somebody typed — where the sender's colors could only mislead: text in
// the background color is hidden, and red or bold can pass the sender's
// words off as the program's own warning. Lines, tab stops, carriage
// returns and invalid UTF-8 are handled as Sanitize handles them.
func Plain(s string) string {
	return StripANSI(Sanitize(s))
}

// PlainLine is [Plain] for text shown within one line: a header value, a
// name, a file name, a label. Before anything else, every line break (LF,
// CR, VT, FF, NEL, U+2028 and U+2029) and every tab becomes a space, so the
// text can neither spill onto lines of its own nor, with a carriage return,
// draw one part of itself over another. The bidirectional embeddings,
// overrides and isolates (U+202A–U+202E and U+2066–U+2069) are dropped,
// since they reorder text on screen: an override turns the name
// "invoice\u202efdp.exe" into "invoiceexe.pdf". Last, every run of white
// space, a no-break space as much as a space, becomes one space, and white
// space at either end goes, so a value made of nothing but escape sequences,
// control characters and spaces comes back empty.
//
// Multi-line text keeps its bidirectional controls in [Sanitize] and Plain,
// since right-to-left text depends on them; a value shown on one line has no
// use for them.
func PlainLine(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.Map(func(r rune) rune {
		switch {
		case lineBreak(r) || r == '\t':
			return ' '
		case bidiControl(r):
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(Plain(s)), " ")
}

// lineBreak reports whether r breaks a line: LF, VT, FF, CR, NEL, or the
// Unicode line and paragraph separators.
func lineBreak(r rune) bool {
	return (r >= '\n' && r <= '\r') || r == '\u0085' || r == '\u2028' || r == '\u2029'
}

// bidiControl reports whether r is an explicit bidirectional formatting
// character: the embeddings and overrides U+202A to U+202E, and the isolates
// U+2066 to U+2069.
func bidiControl(r rune) bool {
	return (r >= '\u202a' && r <= '\u202e') || (r >= '\u2066' && r <= '\u2069')
}

// dropStrings removes the escape strings of s (OSC, DCS, SOS, PM and APC,
// such as a clipboard write), which run to their terminator across newlines,
// or to the end of s without one, as a terminal reads them. Splitting s into
// lines first would leave the rest of such a string behind as text.
func dropStrings(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && strings.IndexByte("]PX^_", s[i+1]) >= 0 {
			i += EscapeLen(s[i:])
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
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
		// column is the width of b up to segment, the text since the last
		// tab, which is measured only when the next tab needs it: a tab ends
		// any grapheme cluster, and measuring b again for every tab would make
		// a long line of them quadratic.
		column  int
		segment strings.Builder
		// unterminated is set once an 8-bit escape string found no
		// terminator up to the end of the line: none of the strings after
		// it can find one either, and looking again for each would make a
		// line of them quadratic.
		unterminated bool
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
			segment.Reset()
		case r == c1CSI:
			i += csiTail(line[i:])
		case c1String(r):
			if !unterminated {
				n := stringTail(line[i:])
				i += n
				unterminated = n == 0
			}
		case r == '\t':
			// Columns count grapheme clusters as a terminal draws them, so a
			// joined emoji such as 👩‍💻 is two cells, not one per rune.
			column += VisibleWidth(segment.String())
			segment.Reset()
			spaces := tabStop - column%tabStop
			b.WriteString(strings.Repeat(" ", spaces))
			column += spaces
		case control(r):
		default:
			b.WriteRune(r)
			segment.WriteRune(r)
		}
	}
	return overwritten + b.String()
}

// The C1 controls that start an 8-bit escape sequence: CSI, and the strings
// OSC, DCS, SOS, PM and APC, and ST, which ends a string.
const (
	c1CSI = '\u009b'
	c1OSC = '\u009d'
	c1DCS = '\u0090'
	c1SOS = '\u0098'
	c1PM  = '\u009e'
	c1APC = '\u009f'
	c1ST  = '\u009c'
)

// c1String reports whether r starts an 8-bit escape string: OSC, DCS, SOS,
// PM or APC.
func c1String(r rune) bool {
	return r == c1OSC || r == c1DCS || r == c1SOS || r == c1PM || r == c1APC
}

// csiTail returns the length of the rest of an 8-bit CSI sequence, what
// follows its U+009B in s: parameter bytes (0x30–0x3F), then intermediate
// bytes (0x20–0x2F), then one final byte (0x40–0x7E). It returns 0 when s
// does not start with such a tail, so only the CSI itself is dropped.
func csiTail(s string) int {
	i := 0
	for i < len(s) && s[i] >= 0x30 && s[i] <= 0x3f {
		i++
	}
	for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
		i++
	}
	if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7e {
		return i + 1
	}
	return 0
}

// stringTail returns the length of the rest of an 8-bit escape string, what
// follows its introducer in s, a line without its newline: the payload and
// the terminator, BEL, ESC \ or ST (U+009C). It returns 0 when no terminator
// follows, so only the introducer is dropped.
func stringTail(s string) int {
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == 0x07:
			return i + 1
		case s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\':
			return i + 2
		case strings.HasPrefix(s[i:], string(c1ST)):
			return i + len(string(c1ST))
		}
	}
	return 0
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
