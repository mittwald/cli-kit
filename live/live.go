package live

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/rivo/uniseg"

	"github.com/mittwald/cli-kit/term"
)

// Live is a block of lines that a terminal shows as it is now: each
// [Live.Draw] replaces the lines the previous draw showed instead of adding
// to the scrollback.
//
// Nothing else may write to the terminal between two draws, because the
// block is found again by counting rows up from the cursor. For the same
// reason a block has to fit on the screen: the cursor cannot move above the
// top of the screen, so rows that scrolled off cannot be redrawn.
//
// The rows are counted at the width the terminal has when the block is
// drawn over, which is right for a terminal that wraps its lines again when
// its window changes width, as most do. A terminal that cuts its lines off
// instead, such as xterm, GNU screen or the Linux console, keeps a line on
// the one row it had; the next draw after such a window was made narrower
// moves up too far and erases the rows above the block that it takes for
// the block's.
//
// A Live is not safe for concurrent use.
type Live struct {
	w io.Writer
	// size reports the terminal's width and height, 0 where unknown.
	size func() (width, height int)
	// drawn is what the last draw left on screen to be drawn over, or nil
	// when the next draw has to go below it instead. Its rows are counted
	// when it is drawn over, at the width the terminal has then: a terminal
	// made narrower in between has wrapped every wider line onto more rows.
	drawn []string
	last  string
}

// New returns a Live that draws to w, which has to be a terminal.
func New(w io.Writer) *Live {
	return &Live{w: w, size: func() (int, int) { return term.Size(w) }}
}

// Width returns the width of the terminal in cells, or 0 when it is not
// known.
func (l *Live) Width() int {
	width, _ := l.size()
	return width
}

// Fits reports whether lines can be drawn and later redrawn in place. Lines
// get one row less than the terminal has: the cursor ends up on the row
// below them. A line with newlines in it counts as the lines [Live.Draw]
// makes of it.
func (l *Live) Fits(lines []string) bool {
	width, height := l.size()
	return height <= 0 || term.ScreenRows(splitLines(lines), width) < height
}

// Draw replaces what the previous draw showed with lines, and leaves the
// cursor at the start of the row below them, so output written after the
// last draw follows it like any other output. Drawing the lines already on
// screen writes nothing, and drawing no lines at all is [Live.Clear].
//
// A line is drawn as the terminal shows it: one wider than the terminal
// wraps onto further rows, a tab moves to the next of the tab stops every 8
// columns, and a line with newlines in it is drawn as the lines between
// them, a carriage return before a newline left out. The redraw counts the
// rows on that basis, so lines must hold no other control character and no
// escape sequence but colors and hyperlinks; term.Sanitize makes text from
// elsewhere fit to draw.
//
// Lines that do not fit lose their first lines until they do, and a single
// remaining line that still wraps past the bottom of the screen is cut short.
// A block cut down this way can still be redrawn in place; a block running
// off the screen would pile up in the scrollback. Callers that know which
// lines matter least should keep within [Live.Fits] themselves. On a
// terminal one row high nothing fits, and each draw goes below the previous
// one.
func (l *Live) Draw(lines []string) error {
	lines = splitLines(lines)
	if len(lines) == 0 {
		return l.Clear()
	}
	width, height := l.size()
	lines, fits := fitScreen(lines, width, height)
	frame := strings.Join(lines, "\n") + "\n"
	if frame == l.last {
		return nil
	}
	var b strings.Builder
	b.WriteString(l.moveUp(width))
	b.WriteString("\x1b[J")
	b.WriteString(frame)
	if _, err := io.WriteString(l.w, b.String()); err != nil {
		return err
	}
	l.last = frame
	l.drawn = nil
	if fits {
		// A copy: the caller may reuse its slice for the next frame, while
		// the screen still shows these lines.
		l.drawn = slices.Clone(lines)
	}
	return nil
}

// splitLines returns lines with every line that holds newlines replaced by
// the lines between them, the way a terminal shows it. Lines without a
// newline are returned as they are.
func splitLines(lines []string) []string {
	if !slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, "\n") }) {
		return lines
	}
	split := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		parts := strings.Split(line, "\n")
		for i, part := range parts {
			if i < len(parts)-1 {
				part = strings.TrimSuffix(part, "\r")
			}
			split = append(split, part)
		}
	}
	return split
}

// fitScreen returns the last of lines that a terminal width cells wide and
// height rows high shows above the cursor's row, and whether they fit there.
// Each line is measured once, from the last one up, and none above the first
// that does not fit. When not even the last line fits, it is cut short to
// the rows there are.
func fitScreen(lines []string, width, height int) (shown []string, fits bool) {
	if height <= 0 {
		return lines, true
	}
	rows, first := 0, len(lines)
	for first > 0 {
		next := term.ScreenRows(lines[first-1:first], width)
		if rows+next >= height {
			break
		}
		rows += next
		first--
	}
	if first < len(lines) {
		return lines[first:], true
	}
	last := lines[len(lines)-1]
	if width > 0 && height > 1 {
		last = truncateRows(last, width, height-1)
	}
	return []string{last}, term.ScreenRows([]string{last}, width) < height
}

// Clear erases the block the last draw showed and leaves the cursor where
// the block started, so the next output takes its place. A block that did
// not fit on the screen cannot be taken back and is left as it is.
func (l *Live) Clear() error {
	width, _ := l.size()
	up := l.moveUp(width)
	l.last, l.drawn = "", nil
	if up == "" {
		return nil
	}
	_, err := io.WriteString(l.w, up+"\x1b[J")
	return err
}

// moveUp returns the sequence that moves the cursor from below the drawn
// block to its first row, or "" when nothing is there to draw over.
func (l *Live) moveUp(width int) string {
	rows := term.ScreenRows(l.drawn, width)
	if rows == 0 {
		return ""
	}
	return fmt.Sprintf("\r\x1b[%dA", rows)
}

// cursor follows where text lands on a terminal width cells wide. Row and
// column count from 0; column is the number of cells used on the row.
type cursor struct{ row, column, width int }

// place puts a grapheme cluster after what is already there, counting its
// cells the way term.ScreenRows does. A character that would straddle the
// right edge starts the next row instead, the way a terminal wraps a wide
// character. A tab moves to the next tab stop of its row and stops at the
// row's last column instead of wrapping.
func (c *cursor) place(cluster string) {
	cells := term.WidthAt(cluster, c.column)
	if cluster == "\t" {
		c.column = max(c.column, min(c.column+cells, c.width-1))
		return
	}
	if c.column+cells > c.width {
		c.row++
		c.column = 0
	}
	c.column += cells
}

// truncateRows cuts text down to what rows rows of a terminal width cells
// wide hold, ending it with an ellipsis. Escape sequences before the cut are
// kept whole, and a color or hyperlink they leave open is closed, so the cut
// does not bleed into what follows.
func truncateRows(text string, width, rows int) string {
	if term.ScreenRows([]string{text}, width) <= rows {
		return text
	}
	var b strings.Builder
	at := cursor{width: width}
	full := false
	styled, linked := false, false
	emit := func(plain string) {
		graphemes := uniseg.NewGraphemes(plain)
		for graphemes.Next() {
			cluster := graphemes.Str()
			next := at
			next.place(cluster)
			// The ellipsis has to fit after the character too.
			probe := next
			probe.place(term.Ellipsis)
			if probe.row >= rows {
				full = true
				return
			}
			b.WriteString(cluster)
			at = next
		}
	}
	for i := 0; i < len(text) && !full; {
		if n := term.EscapeLen(text[i:]); n > 0 {
			sequence := text[i : i+n]
			b.WriteString(sequence)
			switch {
			case strings.HasPrefix(sequence, "\x1b]8;"):
				linked = !strings.HasPrefix(sequence, "\x1b]8;;\x1b") && !strings.HasPrefix(sequence, "\x1b]8;;\a")
			case strings.HasPrefix(sequence, "\x1b[") && strings.HasSuffix(sequence, "m"):
				styled = sequence != "\x1b[0m" && sequence != "\x1b[m"
			}
			i += n
			continue
		}
		next := len(text)
		if j := strings.IndexByte(text[i:], 0x1b); j >= 0 {
			next = i + j
		}
		emit(text[i:next])
		i = next
	}
	if rows > 0 {
		b.WriteString(term.Ellipsis)
	}
	if linked {
		b.WriteString("\x1b]8;;\x1b\\")
	}
	if styled {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}
