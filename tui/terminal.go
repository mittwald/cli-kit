package tui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	xterm "golang.org/x/term"

	"github.com/mittwald/cli-kit/internal/ttyin"
	"github.com/mittwald/cli-kit/live"
	"github.com/mittwald/cli-kit/term"
)

// The sequences that take the terminal over and give it back.
const (
	enterScreen = "\x1b[?1049h\x1b[?25l\x1b[?2004h"
	leaveScreen = "\x1b[?2004l\x1b[0m\x1b[?25h\x1b[?1049l"
	mouseOn     = "\x1b[?1000h\x1b[?1002h\x1b[?1006h"
	mouseOff    = "\x1b[?1006l\x1b[?1002l\x1b[?1000l"
)

// terminal is a real terminal in raw mode on the alternate screen.
type terminal struct {
	in, out *os.File
	reader  *ttyin.Reader
	state   *xterm.State
	mouse   bool
	color   bool

	keys   chan []byte
	resize chan struct{}
	// stop ends the reading goroutine; done is closed once it ended.
	stop     chan struct{}
	done     chan struct{}
	unwatch  func()
	unhook   func()
	restored sync.Once
	mu       sync.Mutex
}

// openTerminal switches the terminal to raw mode and to the alternate
// screen.
func openTerminal(in, out *os.File, mouse bool) (*terminal, error) {
	if !term.Interactive(in, out) || !term.Controls(out) {
		return nil, ErrNotATerminal
	}
	state, err := xterm.MakeRaw(int(in.Fd()))
	if err != nil {
		return nil, fmt.Errorf("tui: switching the terminal to raw mode: %w", err)
	}
	t := &terminal{
		in: in, out: out, state: state, mouse: mouse,
		color:  term.Colored(out),
		reader: ttyin.NewReader(in),
		keys:   make(chan []byte),
		resize: make(chan struct{}, 1),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	t.unwatch = watchSignals(t.restore, t.signalResize)
	t.unhook = live.OnRestore(t.restore)
	// The reader starts first, so close, which waits for it, can end it.
	go t.read()
	if err := t.enter(); err != nil {
		t.close()
		return nil, err
	}
	return t, nil
}

func (t *terminal) enter() error {
	seq := enterScreen
	if t.mouse {
		seq += mouseOn
	}
	return t.write(seq)
}

func (t *terminal) leave() string {
	seq := leaveScreen
	if t.mouse {
		seq = mouseOff + seq
	}
	return seq
}

// read hands what the terminal sends to the loop until stop.
func (t *terminal) read() {
	defer close(t.done)
	defer close(t.keys)
	buf := make([]byte, 4096)
	for {
		n, err := t.reader.Read(buf)
		select {
		case <-t.stop:
			return
		default:
		}
		if errors.Is(err, ttyin.ErrWoken) {
			continue
		}
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			select {
			case t.keys <- data:
			case <-t.stop:
				return
			}
		}
		if err != nil && !errors.Is(err, io.ErrNoProgress) {
			return
		}
	}
}

func (t *terminal) size() (int, int) {
	w, h := term.Size(t.out)
	if w <= 0 || h <= 0 {
		return 80, 24
	}
	return w, h
}

func (t *terminal) write(s string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := io.WriteString(t.out, s)
	return err
}

func (t *terminal) input() <-chan []byte     { return t.keys }
func (t *terminal) resized() <-chan struct{} { return t.resize }
func (t *terminal) colored() bool            { return t.color }

// signalResize tells the loop the terminal changed size.
func (t *terminal) signalResize() {
	select {
	case t.resize <- struct{}{}:
	default:
	}
}

// restoreTimeout is how long restore waits for the terminal to take what
// puts the screen back before it restores the mode without it: a terminal
// that takes no output must not keep the shell in raw mode.
const restoreTimeout = 50 * time.Millisecond

// restore gives the terminal back, once: the main screen, the cursor and
// the mode it was found in.
func (t *terminal) restore() {
	t.restored.Do(func() {
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = t.write(t.leave())
		}()
		timer := time.NewTimer(restoreTimeout)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
		}
		_ = xterm.Restore(int(t.in.Fd()), t.state)
	})
}

// suspend gives the terminal back to the shell and stops the process, and
// takes the terminal again once it continues.
func (t *terminal) suspend() {
	if !canSuspend {
		return
	}
	_ = t.write(t.leave())
	_ = xterm.Restore(int(t.in.Fd()), t.state)
	stopProcess()
	if state, err := xterm.MakeRaw(int(t.in.Fd())); err == nil {
		t.state = state
	}
	_ = t.enter()
}

// close gives the terminal back and ends the reading goroutine. Where a
// read cannot be woken the goroutine stays blocked until the next key,
// which it then drops rather than takes from the program after.
func (t *terminal) close() {
	t.restore()
	t.unwatch()
	t.unhook()
	close(t.stop)
	if t.reader.Wakes() {
		t.reader.Wake()
		<-t.done
		t.reader.Close()
	}
}
