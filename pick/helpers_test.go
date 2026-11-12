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
	// woken makes the input one a read can be woken on, as a terminal on
	// Unix is; newWakeableInput sets it. A nil chunk then stands for a read
	// that finds nothing typed: it calls blocked, if set, and waits to be
	// woken.
	woken   chan struct{}
	blocked func()
}

// newWakeableInput is a scripted input whose reads can be woken.
func newWakeableInput(chunks ...[]byte) *scriptedInput {
	return &scriptedInput{chunks: chunks, noGrace: true, woken: make(chan struct{}, 16)}
}

func (i *scriptedInput) read() ([]byte, error) {
	select {
	case <-i.woken:
		return nil, errWoken
	default:
	}
	if len(i.chunks) == 0 {
		if i.err != nil {
			return nil, i.err
		}
		return nil, io.EOF
	}
	chunk := i.chunks[0]
	i.chunks = i.chunks[1:]
	if chunk == nil && i.woken != nil {
		if i.blocked != nil {
			i.blocked()
		}
		<-i.woken
		return nil, errWoken
	}
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

// unread puts keys back in front of what is left to read, where the next
// prompt driven with this input finds them.
func (i *scriptedInput) unread(keys []byte) {
	if len(keys) > 0 {
		i.chunks = append([][]byte{append([]byte(nil), keys...)}, i.chunks...)
	}
}

func (i *scriptedInput) wakes() bool { return i.woken != nil }

func (i *scriptedInput) wake() {
	if i.woken != nil {
		i.woken <- struct{}{}
	}
}

func fixedSize(width, height int) func() (int, int) {
	return func() (int, int) { return width, height }
}

// renderRow draws the row at position n cells wide.
func (l *list) renderRow(position, n int) string {
	return l.renderRowWith(position, n, l.fittedWidths(n))
}

// loadPending reports whether the view the list stands on has rows to fetch
// that nobody is fetching yet.
func (l *list) loadPending() bool { return l.needsLoad() }

// runPendingLoad fetches the current view's rows and waits for them, the way
// the key loop does where it cannot be woken while it waits for a key.
func (l *list) runPendingLoad() {
	if work := l.nextLoad(); work != nil {
		work()()
	}
}
