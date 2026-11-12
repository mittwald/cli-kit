package pick

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"weak"

	xterm "golang.org/x/term"

	"github.com/mittwald/cli-kit/live"
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

// errWoken is what a read returns when it was woken rather than given keys:
// something off the key loop has a result for it.
var errWoken = errors.New("pick: read woken")

// input is a prompt's key source. It is a seam, so the key loop is testable
// without a terminal.
type input interface {
	// read blocks until keys were typed and returns them. Where wakes
	// reports true it also returns, with errWoken and no keys, once wake was
	// called.
	read() ([]byte, error)
	// grace is the short second read that decides a pending ESC: whatever
	// arrives within escapeGrace, or nothing.
	grace() []byte
	// unread hands back keys that were read but not used, for the next
	// prompt that reads from the same source.
	unread(keys []byte)
	// wakes reports whether wake can end a read that is already blocked.
	wakes() bool
	// wake makes a blocked read, or else the next one, return errWoken. It
	// is the one method that may be called off the key loop.
	wake()
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
// that fetches its rows the first time it is shown. nextLoad hands out that
// work, or nil when there is none to start: a function for the key loop to
// run off its own goroutine, which returns the function that takes the
// result into the block, back on the key loop. The block is only ever
// touched from the key loop that way, and stays responsive while the work
// blocks.
type loaderState interface {
	nextLoad() func() func()
}

// session is a terminal in raw mode with a block drawn on it.
type session struct {
	in, out *os.File
	screen  *renderer
	source  *fileInput
	restore *xterm.State
	color   bool
	theme   theme.Theme
	// unwatch ends the session's own watch for signals and unhook takes it
	// out of the functions package live calls on its ways out of the
	// process. released makes sure the terminal is put back once, whoever
	// gets to it first: the end of the prompt, the watch, or live.
	unwatch  func()
	unhook   func()
	released sync.Once
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
		source:  newFileInput(in),
		restore: previous,
		color:   term.Colored(out),
		theme:   theme.Active(),
	}
	// A signal that ends the process finds the terminal put back. Should
	// the process outlive it, the key loop is left with ErrInterrupted.
	s.unwatch = watchSignals(s.release, s.source.interrupt)
	// An application that installed live's signal handler is ended by that
	// handler, which waits for nobody but the functions registered with it.
	s.unhook = live.OnRestore(s.restored)
	if hide {
		if _, err := io.WriteString(out, hideCursor); err != nil {
			s.close()
			return nil, err
		}
	}
	return s, nil
}

func (s *session) size() (int, int) { return terminalSize(s.out) }

// release erases the block while the terminal is still raw, so the carriage
// returns land, and only then restores the mode and the cursor. Nothing is
// drawn after it.
func (s *session) release() {
	s.released.Do(func() {
		_ = s.screen.end()
		_, _ = io.WriteString(s.out, showCursor)
		_ = xterm.Restore(int(s.in.Fd()), s.restore)
	})
}

// restored is what package live calls where it takes the process to be
// about to end: on a terminating signal its handler caught, in
// live.RestoreAll, and on a panic passed through Cursor.RestoreOnPanic. The
// terminal is put back before the call returns, which is all live waits
// for. A process that is still there a moment later lives on, and the key
// loop gives way to it with ErrInterrupted, as it does after a signal the
// session saw itself: a prompt erased from the screen has no answer to wait
// for.
func (s *session) restored() {
	s.release()
	time.AfterFunc(restoreGrace, s.source.interrupt)
}

// close puts the terminal back and lets go of what the session held.
func (s *session) close() {
	s.unwatch()
	s.unhook()
	s.release()
	s.source.close()
}

func (s *session) drive(state screenState, maxVisible int) error {
	return drive(state, s.screen, s.size, maxVisible, s.source)
}

// drive draws the block and feeds it keys until one ends it.
func drive(state screenState, screen *renderer, size func() (int, int), maxVisible int, source input) error {
	var pending []byte
	var loads loadQueue
	for {
		loads.finish()
		width, height := size()
		state.resize(maxVisible, height)
		if err := screen.draw(state.render(width)); err != nil {
			return err
		}
		if loader, ok := state.(loaderState); ok {
			if work := loader.nextLoad(); work != nil {
				if !source.wakes() {
					// A read cannot be woken here, so the work is done
					// between two draws: the block already says it is
					// loading while the work blocks.
					work()()
					continue
				}
				loads.start(work, source.wake)
			}
		}
		if cursor, ok := state.(cursorState); ok {
			if err := screen.placeCursor(cursor.cursorColumn(width)); err != nil {
				return err
			}
		}
		chunk, readErr := source.read()
		if errors.Is(readErr, errWoken) {
			continue
		}
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
				// What was typed past the answer is for the next prompt.
				source.unread(pending)
				return nil
			case actionBack:
				source.unread(pending)
				return ErrBack
			case actionInterrupt:
				return ErrInterrupted
			}
		}
		if readErr != nil {
			if inputGone(readErr) {
				// Nobody is left to press a key, so there is nothing to go
				// back to either.
				return ErrInterrupted
			}
			return fmt.Errorf("pick: reading the terminal: %w", readErr)
		}
	}
}

// inputGone reports whether a read failed because there is no input any
// more: it ended, it was closed, the terminal behind it went away, or a
// signal ended the prompt.
func inputGone(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) || errors.Is(err, ErrInterrupted) || hungUp(err)
}

// loadQueue runs a block's work off the key loop and keeps what it returns
// until the key loop takes it.
type loadQueue struct {
	mu       sync.Mutex
	finished []func()
}

// start runs work on a goroutine of its own and calls wake once its result
// waits in the queue. A panic in the work is kept as well and raised again
// on the key loop, where the deferred cleanup of the prompt puts the
// terminal back before the program ends.
func (q *loadQueue) start(work func() func(), wake func()) {
	go func() {
		var result func()
		defer func() {
			if failure := recover(); failure != nil {
				result = func() { panic(failure) }
			}
			q.mu.Lock()
			q.finished = append(q.finished, result)
			q.mu.Unlock()
			wake()
		}()
		result = work()
	}()
}

// finish hands the results that arrived to the block. It runs on the key
// loop.
func (q *loadQueue) finish() {
	q.mu.Lock()
	finished := q.finished
	q.finished = nil
	q.mu.Unlock()
	for _, result := range finished {
		result()
	}
}

// restoreGrace is how long a process that was about to end is given to do
// so before the prompt returns to a program that, it seems, lives on.
const restoreGrace = 50 * time.Millisecond

// escapeGrace is how long the continuation of a split escape sequence may
// take before a pending ESC counts as the Esc key: long enough for a link
// that fragments a sequence, short enough that Esc still feels instant.
const escapeGrace = 50 * time.Millisecond

// fileInput reads keys from a terminal already switched to raw mode.
type fileInput struct {
	file *os.File
	buf  [64]byte
	// ahead are the keys the prompt before this one read but did not use.
	ahead []byte
	// wakeRead and wakeWrite are the two ends of the pipe a read waits on
	// next to the terminal, nil where a read cannot be woken. wakeFd is the
	// descriptor of the reading end.
	wakeRead, wakeWrite *os.File
	wakeFd              int
	// gone is set once a signal ended the prompt.
	gone atomic.Bool
}

func newFileInput(file *os.File) *fileInput {
	i := &fileInput{file: file, ahead: takeTypeahead(file), wakeFd: -1}
	if !wakeable || !awaitable(file.Fd()) {
		return i
	}
	// Without a pipe the prompt still works; it just cannot be woken.
	r, w, err := os.Pipe()
	if err != nil {
		return i
	}
	if !awaitable(r.Fd()) {
		// The pipe got a descriptor that cannot be waited on, as select(2)
		// on macOS has it for one past FD_SETSIZE. A read that claimed it
		// could be woken would block on the terminal all the same, and rows
		// loaded meanwhile, or a signal, would wait for the next key.
		_ = r.Close()
		_ = w.Close()
		return i
	}
	i.wakeRead, i.wakeWrite = r, w
	i.wakeFd = int(r.Fd())
	return i
}

// awaitable reports whether awaitInput can wait on the descriptor fd.
func awaitable(fd uintptr) bool {
	return maxAwaitFd >= 0 && fd <= uintptr(maxAwaitFd)
}

func (i *fileInput) read() ([]byte, error) {
	if i.gone.Load() {
		return nil, ErrInterrupted
	}
	if len(i.ahead) > 0 {
		keys := i.ahead
		i.ahead = nil
		return keys, nil
	}
	if i.wakes() {
		if _, woken := awaitInput(i.file, i.wakeFd, -1); woken {
			// Whatever number of wakes is in the pipe, one read answers them.
			var drain [64]byte
			_, _ = i.wakeRead.Read(drain[:])
			if i.gone.Load() {
				return nil, ErrInterrupted
			}
			return nil, errWoken
		}
	}
	n, err := i.file.Read(i.buf[:])
	return i.buf[:n], err
}

// grace waits escapeGrace for more input and returns whatever arrived, or
// nothing on a timeout. It polls the descriptor rather than putting a read
// deadline on the file: a terminal inherited as stdin is not in Go's runtime
// poller, so SetReadDeadline fails on it with os.ErrNoDeadline.
func (i *fileInput) grace() []byte {
	if ready, _ := awaitInput(i.file, -1, escapeGrace); !ready {
		return nil
	}
	n, err := i.file.Read(i.buf[:])
	if n == 0 || err != nil {
		return nil
	}
	return i.buf[:n]
}

func (i *fileInput) unread(keys []byte) { keepTypeahead(i.file, keys) }

func (i *fileInput) wakes() bool { return i.wakeRead != nil }

// wake is safe to call from any goroutine and after close: writing to a
// closed file fails without touching a descriptor.
func (i *fileInput) wake() {
	if i.wakeWrite != nil {
		_, _ = i.wakeWrite.Write([]byte{0})
	}
}

// interrupt makes every read from now on report ErrInterrupted, the one
// that is blocked included.
func (i *fileInput) interrupt() {
	i.gone.Store(true)
	i.wake()
}

// close lets go of the pipe. It is called by the goroutine that reads, once
// it is done reading.
func (i *fileInput) close() {
	if i.wakeRead != nil {
		_ = i.wakeRead.Close()
		_ = i.wakeWrite.Close()
	}
}

// typeahead holds the keys one prompt read past its answer, for the next
// prompt on the same input: an answer pasted or typed ahead for the question
// after this one arrives in the same read as the Enter before it. The input
// is held weakly, so keys nobody comes back for do not keep a file open.
var typeahead struct {
	sync.Mutex
	file weak.Pointer[os.File]
	keys []byte
}

func keepTypeahead(file *os.File, keys []byte) {
	typeahead.Lock()
	defer typeahead.Unlock()
	if len(keys) == 0 {
		if typeahead.file.Value() == file {
			typeahead.keys = nil
		}
		return
	}
	typeahead.file = weak.Make(file)
	typeahead.keys = append([]byte(nil), keys...)
}

func takeTypeahead(file *os.File) []byte {
	typeahead.Lock()
	defer typeahead.Unlock()
	if file == nil || typeahead.file.Value() != file {
		return nil
	}
	keys := typeahead.keys
	typeahead.keys = nil
	return keys
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
// The block starts in the first column of the line the cursor is on, and
// that line is erased with every other line of the block: what was written
// on it before the prompt opened is gone, and with it the question where a
// block would have to start for its lines to be counted right.
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
	// mu guards what follows: a signal puts the terminal back from a
	// goroutine of its own while the key loop may be drawing.
	mu sync.Mutex
	// lines is what the last draw left on the screen.
	lines []string
	// drawn says the block is on screen, so the next draw starts by moving
	// back to its first line.
	drawn bool
	// column is how many cells of the last line lie before the cursor, -1
	// where draw left it after the last character.
	column int
	// ended says the block was erased for good; nothing is drawn after.
	ended bool
}

func (r *renderer) draw(lines []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ended {
		return nil
	}
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
	r.lines, r.drawn, r.column = lines, true, -1
	_, err := io.WriteString(r.w, b.String())
	return err
}

// placeCursor puts the terminal's cursor in a 1-based column of the line just
// drawn.
func (r *renderer) placeCursor(column int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ended {
		return nil
	}
	if column < 1 {
		column = 1
	}
	r.column = column - 1
	_, err := fmt.Fprintf(r.w, "\r\x1b[%dG", column)
	return err
}

// clear erases the block, leaving the cursor where the block started.
func (r *renderer) clear() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.erase()
}

// end is clear for good: every draw after it does nothing.
func (r *renderer) end() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ended = true
	return r.erase()
}

func (r *renderer) erase() error {
	if !r.drawn {
		return nil
	}
	out := r.moveToStart() + "\x1b[J"
	r.lines, r.drawn = nil, false
	_, err := io.WriteString(r.w, out)
	return err
}

// moveToStart is the way to the first column of the block's first line: of
// the line the cursor is on before anything was drawn.
func (r *renderer) moveToStart() string {
	if !r.drawn || len(r.lines) == 0 {
		return "\r"
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
