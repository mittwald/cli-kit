package live

import (
	"io"
	"reflect"
	"slices"
	"sync"
	"time"
)

const (
	// HideCursorSequence hides the terminal cursor (DECTCEM reset).
	HideCursorSequence = "\x1b[?25l"
	// ShowCursorSequence shows the terminal cursor (DECTCEM set).
	ShowCursorSequence = "\x1b[?25h"

	// lockTimeout bounds every wait on the way out of the process: for a
	// frame in progress, for the show sequence to be written, and for the
	// functions registered with OnRestore. A signal handler or a panicking
	// goroutine goes on without what it waited for once the time is up.
	// Showing the cursor, and re-raising the signal behind it, must never be
	// blocked by a stalled terminal write.
	lockTimeout    = 100 * time.Millisecond
	lockRetryDelay = 2 * time.Millisecond
)

// Cursor tracks whether a program hid the cursor of the terminal it writes
// to, so that every way out of the program can show it again: a deferred
// call, a recovered panic, or a signal handler installed by
// [InstallSignalRestore].
//
// A terminal has one cursor, however many Cursor values write to it, so the
// Cursors of one terminal count together: the terminal's cursor is hidden by
// the first of them that hides it and shown again when the last of them
// shows it. A progress bar that finishes therefore does not show a cursor
// the application around it still hides. Two Cursors share a terminal when
// they were created for the same writer, or for writers that wrap it with an
// Unwrap() io.Writer method; on Unix also when their writers are files open
// on the same terminal device, such as os.Stdout and os.Stderr of an
// interactive program, and on Windows when both are consoles. A terminal
// opened through /dev/tty counts as one of its own.
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
	// screen identifies the terminal w writes to. Cursors with the same
	// screen count their hides together.
	screen any
}

// screen is what the registry knows about one terminal while a Cursor hides
// its cursor or changes that.
type screen struct {
	// sequence is held while a hide or show sequence is decided on and
	// written, so the sequences of two Cursors reach the terminal in the
	// order the registry recorded them.
	sequence sync.Mutex
	// hides counts the Cursors that keep the terminal's cursor hidden, and
	// users the calls that are working on the screen. Both are guarded by
	// registryMu.
	hides, users int
}

// The registry is process-wide because the terminal cursor and signal
// dispositions are: hidden holds every cursor hidden and not shown again,
// suspended every cursor a stop signal showed and its continue signal has to
// hide again, screens the terminals either of them belongs to, and
// restoreHooks the functions registered with OnRestore. All are guarded by
// registryMu, which is never held while writing, so a signal handler can
// always read them.
var (
	registryMu   sync.Mutex
	hidden       = map[*Cursor]struct{}{}
	suspended    = map[*Cursor]struct{}{}
	screens      = map[any]*screen{}
	restoreHooks []restoreHook
	lastHook     int
)

type restoreHook struct {
	id      int
	restore func()
}

// NewCursor returns a Cursor for the terminal w writes to. It does not write
// anything.
func NewCursor(w io.Writer) *Cursor {
	c := &Cursor{w: w}
	if c.screen = screenOf(w); c.screen == nil {
		c.screen = c
	}
	return c
}

// screenOf returns what identifies the terminal w writes to: its device
// where the platform tells, otherwise the writer behind every Unwrap. It
// returns nil for a writer that cannot be told apart from others, which then
// counts as a terminal of its own.
func screenOf(w io.Writer) any {
	for w != nil {
		wrapper, ok := w.(interface{ Unwrap() io.Writer })
		if !ok {
			break
		}
		inner := wrapper.Unwrap()
		if inner == nil {
			break
		}
		w = inner
	}
	if w == nil {
		return nil
	}
	if id, ok := terminalID(w); ok {
		return id
	}
	// Only a pointer is safe to compare with any other writer.
	if reflect.ValueOf(w).Kind() == reflect.Pointer {
		return w
	}
	return nil
}

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
// again. Hiding a hidden cursor writes nothing, and so does hiding the
// cursor of a terminal that another Cursor already keeps hidden.
func (c *Cursor) Hide() {
	if c == nil || c.w == nil {
		return
	}
	c.out.Lock()
	defer c.out.Unlock()
	c.change(toHidden)
}

// Show ends what [Cursor.Hide] began: the cursor is visible again unless
// another Cursor of the same terminal still keeps it hidden. It is
// idempotent, so it can be deferred, called at an output boundary, and
// called again from a signal handler without writing a stray sequence.
func (c *Cursor) Show() {
	if c == nil || c.w == nil {
		return
	}
	c.out.Lock()
	defer c.out.Unlock()
	c.change(toShown)
}

// Hidden reports whether c currently keeps the cursor hidden.
func (c *Cursor) Hidden() bool {
	registryMu.Lock()
	defer registryMu.Unlock()
	_, ok := hidden[c]
	return ok
}

// RestoreOnPanic shows every cursor this process hid, as [RestoreAll] does,
// calls the functions registered with [OnRestore], and panics again with the
// value it recovered. A panic in a goroutine runs only that goroutine's
// deferred calls before the process dies, so every goroutine that draws
// while the cursor is hidden should start with
//
//	defer cursor.RestoreOnPanic()
//
// Showing c alone would not do: a terminal that another Cursor hid as well
// stays hidden until that one shows it, and in a dying process it never
// gets to. A program that recovers the panic further up and goes on finds
// all its cursors shown.
//
// It has to be called directly by a deferred statement. A nil Cursor passes
// the panic on all the same.
func (c *Cursor) RestoreOnPanic() {
	if recovered := recover(); recovered != nil {
		wait := startRestoreHooks()
		changeAll(snapshot(hidden), toShown)
		wait()
		panic(recovered)
	}
}

// RestoreAll shows every cursor this process hid and has not shown again,
// and calls the functions registered with [OnRestore]. It waits no longer
// than a moment for a frame in progress, for a terminal that does not take
// the show sequence, or for a registered function, so it is safe to call on
// any exit path: a stalled terminal cannot keep it from returning.
//
// Call it on the way out only. A function a prompt registered puts the
// terminal back into its mode while the prompt may still be waiting, so an
// answer that was to stay hidden would be echoed from then on.
func RestoreAll() {
	wait := startRestoreHooks()
	changeAll(snapshot(hidden), toShown)
	wait()
}

// OnRestore registers restore to be called where the process is about to
// end without reaching its deferred calls: by [RestoreAll], by the signal
// handler of [InstallSignalRestore] before it raises a terminating signal
// again, and by [Cursor.RestoreOnPanic] before it panics again. It is meant
// for what a program changed about its terminal besides the cursor, such as
// its input mode. The returned function takes restore out again; it is safe
// to call more than once.
//
// restore is called on a goroutine of its own, at the same time as the other
// registered functions and possibly while the code it cleans up after is
// still running, and it is called every time one of those paths is taken,
// so it has to be safe to call more than once and from any goroutine. It
// gets a moment, the 100 milliseconds a stalled terminal gets everywhere on
// these paths, shared with all other registered functions: after that the
// process goes on, and dies, without waiting for it.
//
// restore is not called when the process is suspended with SIGTSTP, since
// nothing would undo it when the process continues. A program that has to
// follow a suspension handles SIGTSTP and SIGCONT itself.
func OnRestore(restore func()) (remove func()) {
	if restore == nil {
		return func() {}
	}
	registryMu.Lock()
	lastHook++
	id := lastHook
	restoreHooks = append(restoreHooks, restoreHook{id: id, restore: restore})
	registryMu.Unlock()
	return func() {
		registryMu.Lock()
		defer registryMu.Unlock()
		for i, hook := range restoreHooks {
			if hook.id == id {
				// A new slice: a call in progress keeps the one it copied.
				restoreHooks = append(restoreHooks[:i:i], restoreHooks[i+1:]...)
				return
			}
		}
	}
}

// startRestoreHooks calls the registered functions, each on a goroutine of
// its own, and returns the function that waits for them: until all have
// returned, but no longer than lockTimeout after they were started.
func startRestoreHooks() (wait func()) {
	registryMu.Lock()
	hooks := restoreHooks
	registryMu.Unlock()
	if len(hooks) == 0 {
		return func() {}
	}
	var running sync.WaitGroup
	for _, hook := range hooks {
		running.Go(hook.restore)
	}
	done := make(chan struct{})
	go func() {
		running.Wait()
		close(done)
	}()
	timer := time.NewTimer(lockTimeout)
	return func() {
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
		}
	}
}

// transition is a change to what the registry records about a Cursor.
type transition int

const (
	// toHidden hides the cursor, whatever it was before.
	toHidden transition = iota
	// toShown shows the cursor, whatever it was before.
	toShown
	// toSuspended shows a hidden cursor for the time the process is
	// stopped, and marks it to be hidden again.
	toSuspended
	// toResumed hides a cursor again that is still marked as suspended.
	toResumed
)

// record applies to to c in the registry and returns the sequence the
// terminal needs for it: none while another Cursor keeps its cursor hidden,
// and none when c was already there. Checking and changing happen in one
// critical section, so a Show that comes in between is never undone.
func (c *Cursor) record(s *screen, to transition) string {
	registryMu.Lock()
	defer registryMu.Unlock()
	_, isHidden := hidden[c]
	_, isSuspended := suspended[c]
	hide := false
	switch to {
	case toHidden:
		delete(suspended, c)
		hide = !isHidden
	case toResumed:
		delete(suspended, c)
		hide = isSuspended && !isHidden
	case toShown:
		delete(suspended, c)
		if !isHidden {
			return ""
		}
	case toSuspended:
		if !isHidden {
			return ""
		}
		suspended[c] = struct{}{}
	}
	if hide {
		hidden[c] = struct{}{}
		s.hides++
		if s.hides == 1 {
			return HideCursorSequence
		}
		return ""
	}
	if to == toHidden || to == toResumed {
		return ""
	}
	delete(hidden, c)
	s.hides--
	if s.hides == 0 {
		return ShowCursorSequence
	}
	return ""
}

// enter returns the screen of c's terminal for one change to it; leave ends
// the change. A screen nobody hides and nobody works on is forgotten.
func (c *Cursor) enter() *screen {
	registryMu.Lock()
	defer registryMu.Unlock()
	s := screens[c.screen]
	if s == nil {
		s = &screen{}
		screens[c.screen] = s
	}
	s.users++
	return s
}

func (c *Cursor) leave(s *screen) {
	registryMu.Lock()
	defer registryMu.Unlock()
	s.users--
	if s.users == 0 && s.hides == 0 {
		delete(screens, c.screen)
	}
}

// change applies to to c and writes what the terminal needs for it, with
// c.out held.
func (c *Cursor) change(to transition) {
	s := c.enter()
	defer c.leave(s)
	s.sequence.Lock()
	defer s.sequence.Unlock()
	if sequence := c.record(s, to); sequence != "" {
		_, _ = io.WriteString(c.w, sequence)
	}
}

// changeNow is change for paths that must not block: a signal handler, a
// panicking goroutine, RestoreAll. It gives up on each lock after
// lockTimeout and goes on without it, and it gives the terminal no longer
// than that to take the sequence. A frame that interleaves with the sequence
// on the way out is better than a cursor that stays hidden, and a sequence
// that is still on its way to a stalled terminal is better than a signal
// that never ends the process.
func (c *Cursor) changeNow(to transition) {
	if c == nil || c.w == nil {
		return
	}
	if tryLock(&c.out, lockTimeout) {
		defer c.out.Unlock()
	}
	s := c.enter()
	defer c.leave(s)
	if tryLock(&s.sequence, lockTimeout) {
		defer s.sequence.Unlock()
	}
	if sequence := c.record(s, to); sequence != "" {
		writeBounded(c.w, sequence)
	}
}

// changeAll applies to to every one of cursors without blocking, all at the
// same time, so that several cursors on a stalled terminal take no longer
// than one.
func changeAll(cursors []*Cursor, to transition) {
	if len(cursors) == 1 {
		cursors[0].changeNow(to)
		return
	}
	var changes sync.WaitGroup
	for _, c := range cursors {
		changes.Go(func() { c.changeNow(to) })
	}
	changes.Wait()
}

// writeBounded writes sequence to w and waits for the write no longer than
// lockTimeout. A write that takes longer is left to finish on its own.
func writeBounded(w io.Writer, sequence string) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.WriteString(w, sequence)
	}()
	timer := time.NewTimer(lockTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

func tryLock(mu *sync.Mutex, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if mu.TryLock() {
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
	changeAll(snapshot(hidden), toSuspended)
}

// resumeAll hides every cursor again that suspendAll showed and the program
// has not shown itself since, where the process may draw on the terminal: a
// process continued in the background, as a shell's bg does, leaves the
// cursor to whoever is in the foreground. Its cursors stay marked, for the
// continue signal that brings it to the foreground.
func resumeAll() {
	cursors := snapshot(suspended)
	cursors = slices.DeleteFunc(cursors, func(c *Cursor) bool { return !foreground(c.w) })
	changeAll(cursors, toResumed)
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
