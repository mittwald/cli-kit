package pick

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	xterm "golang.org/x/term"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// action is what a keypress asked of a prompt.
type action int

const (
	actionNone action = iota
	actionSelect
	actionBack
	actionInterrupt
)

// input is a prompt's key source: one blocking read, plus the short second
// read that decides a pending ESC. It is a seam, so the key loop is testable
// without a terminal.
type input interface {
	read() ([]byte, error)
	grace() []byte
}

// screenState is what the key loop drives: a block that reacts to keys, draws
// itself and refits when the terminal changes size. The list, the text field
// and the switches are the three.
type screenState interface {
	apply(k key) action
	render(width int) []string
	resize(maxVisible, height int)
}

// cursorState is a block that leaves the terminal's own cursor visible and
// says which column of its last line the cursor belongs in, 1-based.
type cursorState interface {
	cursorColumn(width int) int
}

// loaderState is a block with work to do once it is on screen, such as a view
// that fetches its rows the first time it is shown. The key loop runs the
// work between two draws, so the block already says it is loading when the
// work blocks.
type loaderState interface {
	loadPending() bool
	runPendingLoad()
}

// session is a terminal in raw mode with a block drawn on it.
type session struct {
	in, out *os.File
	screen  *renderer
	source  input
	restore *xterm.State
	color   bool
	theme   theme.Theme
}

// openTerminal switches in to raw mode and, when hide is set, hides the
// cursor for a block that draws its own. Callers close the session, which
// puts both back as they were found.
func openTerminal(in, out *os.File, hide bool) (*session, error) {
	if !term.Interactive(in, out) {
		return nil, ErrNotATerminal
	}
	previous, err := xterm.MakeRaw(int(in.Fd()))
	if err != nil {
		return nil, fmt.Errorf("pick: switching the terminal to raw mode: %w", err)
	}
	s := &session{
		in: in, out: out,
		screen: &renderer{w: out, width: func() int {
			width, _ := terminalSize(out)
			return width
		}},
		source:  &fileInput{file: in},
		restore: previous,
		color:   term.Colored(out),
		theme:   theme.Active(),
	}
	if hide {
		if _, err := io.WriteString(out, hideCursor); err != nil {
			s.close()
			return nil, err
		}
	}
	return s, nil
}

func (s *session) size() (int, int) { return terminalSize(s.out) }

// close erases the block while the terminal is still raw, so the carriage
// returns land, and only then restores the mode and the cursor.
func (s *session) close() {
	_ = s.screen.clear()
	_, _ = io.WriteString(s.out, showCursor)
	_ = xterm.Restore(int(s.in.Fd()), s.restore)
}

func (s *session) drive(state screenState, maxVisible int) error {
	return drive(state, s.screen, s.size, maxVisible, s.source)
}

// drive draws the block and feeds it keys until one ends it.
func drive(state screenState, screen *renderer, size func() (int, int), maxVisible int, source input) error {
	var pending []byte
	for {
		width, height := size()
		state.resize(maxVisible, height)
		if err := screen.draw(state.render(width)); err != nil {
			return err
		}
		if loader, ok := state.(loaderState); ok && loader.loadPending() {
			loader.runPendingLoad()
			continue
		}
		if cursor, ok := state.(cursorState); ok {
			if err := screen.placeCursor(cursor.cursorColumn(width)); err != nil {
				return err
			}
		}
		chunk, readErr := source.read()
		pending = append(pending, chunk...)
		for len(pending) > 0 {
			k, consumed := decode(pending, true)
			if consumed == 0 {
				// An incomplete sequence: a split escape sequence or the Esc
				// key itself. Give the continuation a moment to arrive, then
				// decide; without this, Esc would look dead until the next
				// keypress.
				if readErr == nil {
					if extra := source.grace(); len(extra) > 0 {
						pending = append(pending, extra...)
						continue
					}
				}
				if k, consumed = decode(pending, false); consumed == 0 {
					break
				}
			}
			pending = pending[consumed:]
			switch state.apply(k) {
			case actionSelect:
				return nil
			case actionBack:
				return ErrBack
			case actionInterrupt:
				return ErrInterrupted
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				// Nobody is left to press a key, so there is nothing to go
				// back to either.
				return ErrInterrupted
			}
			return fmt.Errorf("pick: reading the terminal: %w", readErr)
		}
	}
}

// escapeGrace is how long the continuation of a split escape sequence may
// take before a pending ESC counts as the Esc key: long enough for a link
// that fragments a sequence, short enough that Esc still feels instant.
const escapeGrace = 50 * time.Millisecond

// fileInput reads keys from a terminal already switched to raw mode.
type fileInput struct {
	file *os.File
	buf  [64]byte
}

func (i *fileInput) read() ([]byte, error) {
	n, err := i.file.Read(i.buf[:])
	return i.buf[:n], err
}

// grace waits escapeGrace for more input and returns whatever arrived, or
// nothing on a timeout. It polls the descriptor rather than putting a read
// deadline on the file: a terminal inherited as stdin is not in Go's runtime
// poller, so SetReadDeadline fails on it with os.ErrNoDeadline.
func (i *fileInput) grace() []byte {
	if !readable(i.file, escapeGrace) {
		return nil
	}
	n, err := i.file.Read(i.buf[:])
	if n == 0 || err != nil {
		return nil
	}
	return i.buf[:n]
}

const (
	hideCursor = "\x1b[?25l"
	showCursor = "\x1b[?25h"
)

// terminalSize is the size of out, or 80×24 where it cannot be read.
func terminalSize(out *os.File) (int, int) {
	width, height := term.Size(out)
	if width <= 0 || height <= 0 {
		return 80, 24
	}
	return width, height
}

// renderer draws successive versions of a block in place: it moves back to
// the block's first line, rewrites every line, and erases whatever the
// previous version left below it.
//
// The way back is counted in screen rows at the width the terminal has when
// it is taken: a terminal made narrower while the block is up wraps what was
// drawn onto more rows, and counting lines would leave the top of the block
// behind.
type renderer struct {
	w io.Writer
	// width is the terminal's current width, 0 or nil where unknown: every
	// line then counts as one row.
	width func() int
	// lines is what the last draw left on the screen.
	lines []string
	// column is how many cells of the last line lie before the cursor, -1
	// where draw left it after the last character.
	column int
}

func (r *renderer) draw(lines []string) error {
	var b strings.Builder
	b.WriteString(r.moveToStart())
	for i, line := range lines {
		if i > 0 {
			b.WriteString("\r\n")
		}
		b.WriteString("\x1b[2K")
		b.WriteString(line)
	}
	b.WriteString("\x1b[J")
	r.lines, r.column = lines, -1
	_, err := io.WriteString(r.w, b.String())
	return err
}

// placeCursor puts the terminal's cursor in a 1-based column of the line just
// drawn.
func (r *renderer) placeCursor(column int) error {
	if column < 1 {
		column = 1
	}
	r.column = column - 1
	_, err := fmt.Fprintf(r.w, "\r\x1b[%dG", column)
	return err
}

// clear erases the block, leaving the cursor where the block started.
func (r *renderer) clear() error {
	if len(r.lines) == 0 {
		return nil
	}
	out := r.moveToStart() + "\x1b[J"
	r.lines = nil
	_, err := io.WriteString(r.w, out)
	return err
}

func (r *renderer) moveToStart() string {
	if len(r.lines) == 0 {
		return ""
	}
	if up := r.rowsAbove(); up > 0 {
		return fmt.Sprintf("\r\x1b[%dA", up)
	}
	return "\r"
}

// rowsAbove is how many rows of the block lie above the one the cursor is
// on, at the width the terminal has now.
func (r *renderer) rowsAbove() int {
	width := 0
	if r.width != nil {
		width = r.width()
	}
	last := len(r.lines) - 1
	rows := term.ScreenRows(r.lines[:last], width)
	switch {
	case r.column < 0:
		rows += term.ScreenRows(r.lines[last:], width) - 1
	case width > 0:
		rows += r.column / width
	}
	return rows
}
