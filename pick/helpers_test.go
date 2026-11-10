package pick

import (
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/mittwald/cli-kit/theme"
)

// th is the theme every test renders with.
var th = theme.Default

// seq is the escape sequence that switches style on.
func seq(style theme.Style) string { return style.Sequence() }

func testList(t *testing.T, rows int, cells ...[]string) *list {
	t.Helper()
	items := make([]Item, 0, len(cells))
	for _, row := range cells {
		items = append(items, Item{Cells: row})
	}
	return newList(Options{Title: "Which one?", Items: items, MaxVisible: rows}, 0, false)
}

func numbered(t *testing.T, rows, count int) *list {
	t.Helper()
	cells := make([][]string, 0, count)
	for i := range count {
		cells = append(cells, []string{"1.4." + strconv.Itoa(i)})
	}
	return testList(t, rows, cells...)
}

func typeText(l screenState, text string) {
	for _, r := range text {
		l.apply(key{kind: keyRune, rune: r})
	}
}

func stripSGR(s string) string {
	var b strings.Builder
	for {
		start := strings.Index(s, "\x1b[")
		if start < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:start])
		end := strings.IndexByte(s[start:], 'm')
		if end < 0 {
			return b.String()
		}
		s = s[start+end+1:]
	}
}

// scriptedInput replays chunks of bytes as if they were typed, which is how
// the key loop is driven without a terminal.
type scriptedInput struct {
	chunks [][]byte
	err    error
	// noGrace makes every pending ESC resolve as the Esc key, the way a
	// terminal behaves when nothing follows it.
	noGrace bool
}

func (i *scriptedInput) read() ([]byte, error) {
	if len(i.chunks) == 0 {
		if i.err != nil {
			return nil, i.err
		}
		return nil, io.EOF
	}
	chunk := i.chunks[0]
	i.chunks = i.chunks[1:]
	return chunk, nil
}

// grace hands over the next chunk, the way a terminal that split an escape
// sequence across two writes behaves. With nothing left it returns nothing,
// and a pending ESC then resolves as the Esc key.
func (i *scriptedInput) grace() []byte {
	if i.noGrace || len(i.chunks) == 0 {
		return nil
	}
	chunk := i.chunks[0]
	i.chunks = i.chunks[1:]
	return chunk
}

func fixedSize(width, height int) func() (int, int) {
	return func() (int, int) { return width, height }
}

// renderRow draws the row at position n cells wide.
func (l *list) renderRow(position, n int) string {
	return l.renderRowWith(position, n, l.fittedWidths(n))
}
