package term

import (
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

// Ellipsis marks where [Truncate] and [TruncateMiddle] cut text.
const Ellipsis = "…"

// tabStop is how far apart the tab stops are that a tab advances to, a
// terminal's default.
const tabStop = 8

// widths measures text the same way on every machine. The characters whose
// width Unicode leaves ambiguous (box-drawing lines, the ellipsis, bullets,
// arrows and many other symbols) count one cell, which is how terminals draw
// them unless they are set up otherwise. go-runewidth's default counts them
// two cells wide under a Chinese, Japanese or Korean locale and under
// RUNEWIDTH_EASTASIAN=1, so a table drew crooked and a block redrawn in place
// climbed too far there.
var widths = &runewidth.Condition{EastAsianWidth: false, StrictEmojiNeutral: true}

// cells returns how many cells s takes up, which holds neither an escape
// sequence nor a tab.
func cells(s string) int { return widths.StringWidth(s) }

// StripANSI removes terminal escape sequences from s: CSI sequences such as
// colors and cursor movement (ESC [ … final byte), OSC sequences such as
// hyperlinks (ESC ] … BEL or ESC \), and the short escapes such as ESC 7
// and ESC ( B. Everything else, multi-byte runes included, is left
// untouched. It does not make text from elsewhere safe to show: control
// characters stay. [Sanitize] and [Plain] do that.
func StripANSI(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if n := EscapeLen(s[i:]); n > 0 {
			i += n
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// EscapeLen returns the length in bytes of the escape sequence s starts
// with, or 0 when s does not start with one. It recognizes CSI sequences
// (ESC [ … final byte), OSC, DCS, SOS, PM and APC strings (ended by BEL or
// ESC \), and the short escapes: ESC, any intermediate bytes (0x20–0x2F),
// and a final byte (0x30–0x7E), such as ESC 7 or ESC ( B. An ESC followed by
// anything else, such as a control character or the first byte of a
// multi-byte rune, is a sequence of one byte, so the rune stays whole. An
// unterminated sequence runs to the end of s, so a cut-off sequence is never
// counted as visible text. Code that walks styled text uses it to copy escape
// sequences through whole.
func EscapeLen(s string) int {
	if len(s) == 0 || s[0] != 0x1b {
		return 0
	}
	if len(s) == 1 {
		return 1
	}
	switch s[1] {
	case '[': // CSI: parameters and intermediates, then one final byte.
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
		return len(s)
	case ']', 'P', 'X', '^', '_': // OSC, DCS, SOS, PM, APC: ended by BEL or ST.
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return len(s)
	}
	// A short escape: intermediate bytes, then one final byte.
	i := 1
	for i < len(s) && s[i] >= 0x20 && s[i] <= 0x2f {
		i++
	}
	switch {
	case i == len(s):
		return len(s)
	case s[i] >= 0x30 && s[i] <= 0x7e:
		return i + 1
	}
	return i
}

// VisibleWidth returns how many terminal cells s takes up once drawn, escape
// sequences left out. Wide runes (CJK, most emoji) count two cells, combining
// marks none, and a tab counts the cells up to the next tab stop, every 8
// columns from the start of its line. For text spanning several lines it
// measures the widest line.
//
// A character whose width Unicode leaves ambiguous, such as a box-drawing
// line, the ellipsis or a bullet, counts one cell, as terminals draw it by
// default. The width of text does not depend on the locale: LANG, LC_ALL and
// RUNEWIDTH_EASTASIAN change nothing about it.
func VisibleWidth(s string) int {
	if strings.Contains(s, "\n") {
		widest := 0
		for line := range strings.SplitSeq(s, "\n") {
			widest = max(widest, WidthAt(line, 0))
		}
		return widest
	}
	return WidthAt(s, 0)
}

// WidthAt returns how many terminal cells s, text within one line, takes up
// when it starts at column, the number of cells before it on its line. It
// measures the way [VisibleWidth] does; the two differ only for text with a
// tab in it, whose width depends on where the tab stands: "\tx" is 9 cells
// wide at column 0 and 5 at column 4. Code that lays text out cell by cell
// measures each piece at the column it is drawn at. A column below 0 is 0.
func WidthAt(s string, column int) int {
	column = max(column, 0)
	start := column
	s = StripANSI(s)
	for {
		text, rest, found := strings.Cut(s, "\t")
		column += cells(text)
		if !found {
			return column - start
		}
		column += tabStop - column%tabStop
		s = rest
	}
}

// expandTabs replaces every tab of s with the spaces up to its tab stop, so
// the text takes up the same cells wherever on a line it is drawn. Escape
// sequences are copied through and take up no cells.
func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	column := 0
	for i := 0; i < len(s); {
		if n := EscapeLen(s[i:]); n > 0 {
			b.WriteString(s[i : i+n])
			i += n
			continue
		}
		next := len(s)
		if j := strings.IndexAny(s[i:], "\t\n\x1b"); j >= 0 {
			next = i + j
		}
		b.WriteString(s[i:next])
		column += cells(s[i:next])
		i = next
		if i == len(s) {
			break
		}
		switch s[i] {
		case '\t':
			spaces := tabStop - column%tabStop
			b.WriteString(strings.Repeat(" ", spaces))
			column += spaces
			i++
		case '\n':
			b.WriteByte('\n')
			column = 0
			i++
		}
	}
	return b.String()
}

// PadRight pads s with spaces to width visible cells. Text already that wide
// or wider is returned unchanged.
func PadRight(s string, width int) string {
	if pad := width - VisibleWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// PadLeft pads s with leading spaces to width visible cells, right-aligning
// it. Text already that wide or wider is returned unchanged. The spaces move
// s along its line and its tab stops with it, so the tabs of text that is
// padded become the spaces they stood for.
func PadLeft(s string, width int) string {
	if pad := width - VisibleWidth(s); pad > 0 {
		return strings.Repeat(" ", pad) + expandTabs(s)
	}
	return s
}

// Truncate cuts s down to width visible cells, ending it with [Ellipsis] when
// anything was cut. Text of several lines is cut line by line: every line
// wider than width ends with an Ellipsis of its own, and the lines that fit
// stay as they are. Escape sequences up to a cut are kept, and a color or
// hyperlink still open there, even one an earlier line opened, is closed
// after the cut so that it never bleeds into what follows. A width of 0 or
// less, what [Size] reports for anything but a terminal, leaves s whole.
func Truncate(s string, width int) string {
	if width <= 0 || VisibleWidth(s) <= width {
		return s
	}
	tail, room := Ellipsis, width-cells(Ellipsis)
	if room <= 0 {
		tail, room = "", width
	}
	var (
		b    strings.Builder
		open openSequences
	)
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		if WidthAt(line, 0) <= width {
			b.WriteString(line)
			open.trackAll(line)
			continue
		}
		clip(&b, &open, line, room, tail)
	}
	return b.String()
}

// clip writes to b the leading graphemes of line, text without a newline,
// that fit into width cells, then tail. Escape sequences are copied through
// as they are met and tracked in open, and a color or hyperlink still open at
// the cut is closed after tail. A line that fits into width is written whole,
// without tail.
func clip(b *strings.Builder, open *openSequences, line string, width int, tail string) {
	used := 0
	for i := 0; i < len(line); {
		if n := EscapeLen(line[i:]); n > 0 {
			seq := line[i : i+n]
			b.WriteString(seq)
			open.track(seq)
			i += n
			continue
		}
		next := len(line)
		if j := strings.IndexByte(line[i:], 0x1b); j >= 0 {
			next = i + j
		}
		graphemes := uniseg.NewGraphemes(line[i:next])
		for graphemes.Next() {
			cluster := graphemes.Str()
			w := clusterWidth(cluster, used)
			if used+w > width {
				b.WriteString(tail)
				open.end(b)
				return
			}
			b.WriteString(cluster)
			used += w
		}
		i = next
	}
}

// clusterWidth returns how many cells cluster, one grapheme cluster, takes up
// at column: a tab the cells up to the next tab stop.
func clusterWidth(cluster string, column int) int {
	if cluster == "\t" {
		return tabStop - column%tabStop
	}
	return cells(cluster)
}

// openSequences tracks whether a color or an OSC 8 hyperlink is still open
// at a point in styled text.
type openSequences struct{ styled, linked bool }

func (o *openSequences) track(seq string) {
	switch {
	case strings.HasPrefix(seq, "\x1b]8;"):
		// An OSC 8 sequence with an empty URI closes the link.
		o.linked = !strings.HasPrefix(seq, "\x1b]8;;\x1b") && !strings.HasPrefix(seq, "\x1b]8;;\a")
	case strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m"):
		o.styled = seq != "\x1b[0m" && seq != "\x1b[m"
	}
}

// trackAll tracks every escape sequence in s.
func (o *openSequences) trackAll(s string) {
	for {
		i := strings.IndexByte(s, 0x1b)
		if i < 0 {
			return
		}
		n := EscapeLen(s[i:])
		o.track(s[i : i+n])
		s = s[i+n:]
	}
}

// end closes the hyperlink and ends the color that are open, and forgets
// them.
func (o *openSequences) end(b *strings.Builder) {
	if o.linked {
		b.WriteString("\x1b]8;;\x1b\\")
	}
	if o.styled {
		b.WriteString("\x1b[0m")
	}
	*o = openSequences{}
}

// TruncateMiddle cuts s down to width visible cells by taking text out of its
// middle, so both ends stay readable: "feature/very-long-branch-name" cut to
// 21 cells becomes "feature/ve…ranch-name". Text of several lines is cut line
// by line: every line wider than width loses its own middle, and the lines
// that fit keep their text. Where anything is cut, the escape sequences of s
// are dropped, and the tabs of a line that is cut become the spaces up to
// their tab stops. A width of 0 or less leaves s whole.
func TruncateMiddle(s string, width int) string {
	if width <= 0 || VisibleWidth(s) <= width {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		line = StripANSI(line)
		if WidthAt(line, 0) > width {
			line = cutMiddle(expandTabs(line), width)
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// cutMiddle is TruncateMiddle for one line that is wider than width and
// holds neither an escape sequence nor a tab.
func cutMiddle(line string, width int) string {
	var clusters []string
	graphemes := uniseg.NewGraphemes(line)
	for graphemes.Next() {
		clusters = append(clusters, graphemes.Str())
	}
	budget := width - cells(Ellipsis)
	if budget <= 0 {
		// No room for an ellipsis beside any text: the head alone.
		head, _ := leading(clusters, width)
		return head
	}
	head, used := leading(clusters, (budget+1)/2)
	// A wide rune that did not fit at the head leaves cells over; the tail
	// gets them, so the result uses the whole width where it can.
	tailBudget := budget - used
	tailUsed := 0
	start := len(clusters)
	for start > 0 {
		w := cells(clusters[start-1])
		if tailUsed+w > tailBudget {
			break
		}
		tailUsed += w
		start--
	}
	return head + Ellipsis + strings.Join(clusters[start:], "")
}

// leading returns the first of clusters that fit into width cells, and how
// many cells they take up.
func leading(clusters []string, width int) (string, int) {
	var b strings.Builder
	used := 0
	for _, c := range clusters {
		w := cells(c)
		if used+w > width {
			break
		}
		b.WriteString(c)
		used += w
	}
	return b.String(), used
}

// ScreenRows returns how many terminal rows lines take up on a terminal width
// cells wide. It follows the cursor the way a terminal does: a line wider
// than the terminal wraps onto further rows, and a wide character that would
// straddle the right edge starts the next row, leaving the last cell empty. A
// line exactly as wide as the terminal takes one row. A tab moves to the next
// tab stop of its row, every 8 columns, and stops at the row's last column
// instead of wrapping. A width of 0 or less counts one row per line. Redrawing
// a block in place depends on this count being exact, so lines hold no
// control character but the tab: no newline, no carriage return.
func ScreenRows(lines []string, width int) int {
	rows := 0
	for _, line := range lines {
		rows += lineRows(line, width)
	}
	return rows
}

func lineRows(line string, width int) int {
	if width <= 0 {
		return 1
	}
	row, column := 0, 0
	graphemes := uniseg.NewGraphemes(StripANSI(line))
	for graphemes.Next() {
		cluster := graphemes.Str()
		if cluster == "\t" {
			column = max(column, min(column+tabStop-column%tabStop, width-1))
			continue
		}
		n := cells(cluster)
		if column+n > width {
			row++
			column = 0
		}
		column += n
	}
	return row + 1
}
