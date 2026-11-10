package live

import (
	"io"
	"sync"
	"time"
)

const (
	// HideCursorSequence hides the terminal cursor (DECTCEM reset).
	HideCursorSequence = "\x1b[?25l"
	// ShowCursorSequence shows the terminal cursor (DECTCEM set).
	ShowCursorSequence = "\x1b[?25h"

	// lockTimeout bounds how long a signal handler or a panicking goroutine
	// waits for a frame in progress before it writes the show sequence
	// without the lock. Showing the cursor, and re-raising the signal behind
	// it, must never be blocked by a stalled terminal write.
	lockTimeout    = 100 * time.Millisecond
	lockRetryDelay = 2 * time.Millisecond
)

// Cursor tracks whether a program hid the cursor of the terminal it writes
// to, so that every way out of the program can show it again: a deferred
// call, a recovered panic, or a signal handler installed by
// [InstallSignalRestore].
//
// A Cursor also serializes output to its terminal. Code that draws frames
// holds [Cursor.Lock] while it writes, so a show sequence written from a
// signal handler never lands in the middle of a frame.
//
// A hidden Cursor is recorded in a process-wide registry until it is shown
// again; that registry is what the signal handler and [RestoreAll] use.
// Hide, Show and the lock are safe for concurrent use.
type Cursor struct {
	w   io.Writer
	out sync.Mutex
}

// The registry is process-wide because the terminal cursor and signal
// dispositions are: hidden holds every cursor hidden and not shown again,
// suspended every cursor a stop signal showed and its continue signal has to
// hide again. Both are guarded by registryMu, which is never held while
// writing, so a signal handler can always read them.
var (
	registryMu sync.Mutex
	hidden     = map[*Cursor]struct{}{}
	suspended  = map[*Cursor]struct{}{}
)

// NewCursor returns a Cursor for the terminal w writes to. It does not write
// anything.
func NewCursor(w io.Writer) *Cursor { return &Cursor{w: w} }

// HideCursor hides the cursor of the terminal w writes to and returns the
// function that shows it again. The function is idempotent, so it can be
// deferred and also called earlier.
func HideCursor(w io.Writer) (restore func()) {
	c := NewCursor(w)
	c.Hide()
	return c.Show
}

// Lock serializes a write to the terminal with the cursor's own writes.
// Hold it while writing a frame; do not call Hide or Show while holding it.
func (c *Cursor) Lock() { c.out.Lock() }

// Unlock releases the lock taken by [Cursor.Lock].
func (c *Cursor) Unlock() { c.out.Unlock() }

// Hide hides the cursor and records it, so a signal handler can show it
// again. Hiding a hidden cursor writes nothing.
func (c *Cursor) Hide() {
	if c == nil || c.w == nil {
		return
	}
	c.out.Lock()
	defer c.out.Unlock()
	c.hideLocked()
}

// Show makes the cursor visible again if [Cursor.Hide] hid it. It is
// idempotent, so it can be deferred, called at an output boundary, and
// called again from a signal handler without writing a stray sequence.
func (c *Cursor) Show() {
	if c == nil || c.w == nil {
		return
	}
	c.out.Lock()
	defer c.out.Unlock()
	c.showLocked()
}

// Hidden reports whether c currently keeps the cursor hidden.
func (c *Cursor) Hidden() bool {
	registryMu.Lock()
	defer registryMu.Unlock()
	_, ok := hidden[c]
	return ok
}

// RestoreOnPanic shows the cursor and re-panics. A panic in a goroutine runs
// only that goroutine's deferred calls before the process dies, so every
// goroutine that draws while the cursor is hidden should start with
//
//	defer cursor.RestoreOnPanic()
//
// It has to be called directly by a deferred statement.
func (c *Cursor) RestoreOnPanic() {
	if recovered := recover(); recovered != nil {
		c.showNow()
		panic(recovered)
	}
}

// RestoreAll shows every cursor this process hid and has not shown again. It
// waits no longer than a moment for a frame in progress, so it is safe to
// call on any exit path.
func RestoreAll() {
	for _, c := range snapshot(hidden) {
		c.showNow()
	}
}

func (c *Cursor) hideLocked() {
	registryMu.Lock()
	delete(suspended, c)
	if _, ok := hidden[c]; ok {
		registryMu.Unlock()
		return
	}
	hidden[c] = struct{}{}
	registryMu.Unlock()
	_, _ = io.WriteString(c.w, HideCursorSequence)
}

func (c *Cursor) showLocked() {
	registryMu.Lock()
	delete(suspended, c)
	if _, ok := hidden[c]; !ok {
		registryMu.Unlock()
		return
	}
	delete(hidden, c)
	registryMu.Unlock()
	_, _ = io.WriteString(c.w, ShowCursorSequence)
}

// showNow is Show for paths that must not block: it gives up on the output
// lock after lockTimeout and writes the show sequence anyway. A frame that
// interleaves with it on the way out is better than a cursor that stays
// hidden, or a signal that is delayed.
func (c *Cursor) showNow() {
	if c.tryLock(lockTimeout) {
		defer c.out.Unlock()
	}
	c.showLocked()
}

func (c *Cursor) tryLock(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if c.out.TryLock() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(lockRetryDelay)
	}
}

// suspendAll shows every hidden cursor before the process is stopped, and
// marks it to be hidden again once the process continues.
func suspendAll() {
	for _, c := range snapshot(hidden) {
		c.showNow()
		registryMu.Lock()
		suspended[c] = struct{}{}
		registryMu.Unlock()
	}
}

// resumeAll hides every cursor suspendAll showed again.
func resumeAll() {
	cursors := snapshot(suspended)
	for _, c := range cursors {
		c.Hide()
	}
}

// forgetSuspended drops the marks suspendAll set, for a stop that failed:
// the process kept running, so its cursors stay visible.
func forgetSuspended() {
	registryMu.Lock()
	defer registryMu.Unlock()
	clear(suspended)
}

func snapshot(set map[*Cursor]struct{}) []*Cursor {
	registryMu.Lock()
	defer registryMu.Unlock()
	cursors := make([]*Cursor, 0, len(set))
	for c := range set {
		cursors = append(cursors, c)
	}
	return cursors
}
