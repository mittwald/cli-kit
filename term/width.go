package term

import (
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

// Ellipsis marks where [Truncate] and [TruncateMiddle] cut text.
const Ellipsis = "…"

// StripANSI removes terminal escape sequences from s: CSI sequences such as
// colors and cursor movement (ESC [ … final byte), OSC sequences such as
// hyperlinks (ESC ] … BEL or ESC \), and the short escapes such as ESC 7
// and ESC ( B. Everything else, multi-byte runes included, is left
// untouched. It does not make text from elsewhere safe to show: control
// characters stay. [Sanitize] does that.
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
// marks none. For text spanning several lines it measures the widest line.
func VisibleWidth(s string) int {
	if strings.Contains(s, "\n") {
		widest := 0
		for line := range strings.SplitSeq(s, "\n") {
			widest = max(widest, VisibleWidth(line))
		}
		return widest
	}
	return runewidth.StringWidth(StripANSI(s))
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
// it. Text already that wide or wider is returned unchanged.
func PadLeft(s string, width int) string {
	if pad := width - VisibleWidth(s); pad > 0 {
		return strings.Repeat(" ", pad) + s
	}
	return s
}

// Truncate cuts s down to width visible cells, ending it with [Ellipsis] when
// anything was cut. Escape sequences are kept, and a reset is appended after
// a cut so a color never bleeds into what follows. A width of 0 or less,
// what [Size] reports for anything but a terminal, leaves s whole.
func Truncate(s string, width int) string {
	if width <= 0 || VisibleWidth(s) <= width {
		return s
	}
	ellipsisWidth := runewidth.StringWidth(Ellipsis)
	if width <= ellipsisWidth {
		return clip(s, width, "")
	}
	return clip(s, width-ellipsisWidth, Ellipsis)
}

// clip keeps the leading graphemes of s that fit into width cells, then
// appends tail. Escape sequences are copied through as they are met, and a
// color or hyperlink still open at the cut is closed after tail.
func clip(s string, width int, tail string) string {
	var b strings.Builder
	used := 0
	var open openSequences
	for i := 0; i < len(s); {
		if n := EscapeLen(s[i:]); n > 0 {
			seq := s[i : i+n]
			b.WriteString(seq)
			open.track(seq)
			i += n
			continue
		}
		next := len(s)
		if j := strings.IndexByte(s[i:], 0x1b); j >= 0 {
			next = i + j
		}
		graphemes := uniseg.NewGraphemes(s[i:next])
		for graphemes.Next() {
			cluster := graphemes.Str()
			w := runewidth.StringWidth(cluster)
			if used+w > width {
				return open.finish(&b, tail)
			}
			b.WriteString(cluster)
			used += w
		}
		i = next
	}
	return open.finish(&b, tail)
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

func (o *openSequences) finish(b *strings.Builder, tail string) string {
	b.WriteString(tail)
	if o.linked {
		b.WriteString("\x1b]8;;\x1b\\")
	}
	if o.styled {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// TruncateMiddle cuts s down to width visible cells by taking text out of its
// middle, so both ends stay readable: "feature/very-long-branch-name" cut to
// 21 cells becomes "feature/ve…ranch-name". Escape sequences are dropped. A
// width of 0 or less leaves s whole.
func TruncateMiddle(s string, width int) string {
	if width <= 0 || VisibleWidth(s) <= width {
		return s
	}
	plain := StripANSI(s)
	var clusters []string
	graphemes := uniseg.NewGraphemes(plain)
	for graphemes.Next() {
		clusters = append(clusters, graphemes.Str())
	}
	budget := width - runewidth.StringWidth(Ellipsis)
	if budget <= 0 {
		return clip(plain, width, "")
	}
	headBudget := (budget + 1) / 2
	var head strings.Builder
	used := 0
	for _, c := range clusters {
		w := runewidth.StringWidth(c)
		if used+w > headBudget {
			break
		}
		head.WriteString(c)
		used += w
	}
	// A wide rune that did not fit at the head leaves cells over; the tail
	// gets them, so the result uses the whole width where it can.
	tailBudget := budget - used
	tailUsed := 0
	start := len(clusters)
	for start > 0 {
		w := runewidth.StringWidth(clusters[start-1])
		if tailUsed+w > tailBudget {
			break
		}
		tailUsed += w
		start--
	}
	return head.String() + Ellipsis + strings.Join(clusters[start:], "")
}

// ScreenRows returns how many terminal rows lines take up on a terminal width
// cells wide. It follows the cursor the way a terminal does: a line wider
// than the terminal wraps onto further rows, and a wide character that would
// straddle the right edge starts the next row, leaving the last cell empty. A
// line exactly as wide as the terminal takes one row. A width of 0 or less
// counts one row per line. Redrawing a block in place depends on this count
// being exact.
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
		cells := runewidth.StringWidth(graphemes.Str())
		if column+cells > width {
			row++
			column = 0
		}
		column += cells
	}
	return row + 1
}
