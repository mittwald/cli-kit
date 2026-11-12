package live

import (
	"bytes"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// swapRaiseHandler replaces the default-disposition re-raise so a test can
// observe it without ending the test process.
func swapRaiseHandler(t *testing.T, raise func(os.Signal)) {
	t.Helper()
	signalActionMu.Lock()
	previous := raiseHandler
	raiseHandler = raise
	signalActionMu.Unlock()
	t.Cleanup(func() {
		signalActionMu.Lock()
		raiseHandler = previous
		signalActionMu.Unlock()
	})
}

// swapStopHandler replaces the self-stop so a test can drive the stop path
// without stopping the test process.
func swapStopHandler(t *testing.T, stop func() bool) {
	t.Helper()
	signalActionMu.Lock()
	previous := stopHandler
	stopHandler = stop
	signalActionMu.Unlock()
	t.Cleanup(func() {
		signalActionMu.Lock()
		stopHandler = previous
		signalActionMu.Unlock()
	})
}

// resetRegistry forgets every cursor a test hid.
func resetRegistry(t *testing.T) {
	t.Helper()
	clearRegistry := func() {
		registryMu.Lock()
		clear(hidden)
		clear(suspended)
		clear(screens)
		restoreHooks = nil
		registryMu.Unlock()
	}
	clearRegistry()
	t.Cleanup(clearRegistry)
}

func TestCursorShowIsIdempotent(t *testing.T) {
	resetRegistry(t)
	var buf bytes.Buffer
	c := NewCursor(&buf)

	c.Hide()
	c.Hide()
	if got := buf.String(); got != HideCursorSequence {
		t.Fatalf("Hide wrote %q, want %q", got, HideCursorSequence)
	}
	if !c.Hidden() {
		t.Fatal("Hidden() = false after Hide")
	}

	buf.Reset()
	c.Show()
	if got := buf.String(); got != ShowCursorSequence {
		t.Fatalf("Show wrote %q, want %q", got, ShowCursorSequence)
	}

	buf.Reset()
	c.Show()
	c.changeNow(toShown)
	RestoreAll()
	if buf.Len() != 0 {
		t.Fatalf("showing a visible cursor wrote %q", buf.String())
	}
}

func TestHideCursorReturnsRestore(t *testing.T) {
	resetRegistry(t)
	var buf bytes.Buffer
	restore := HideCursor(&buf)
	restore()
	restore()
	if got, want := buf.String(), HideCursorSequence+ShowCursorSequence; got != want {
		t.Fatalf("wrote %q, want %q", got, want)
	}
}

func TestRestoreAllShowsEveryHiddenCursor(t *testing.T) {
	resetRegistry(t)
	var first, second bytes.Buffer
	NewCursor(&first).Hide()
	NewCursor(&second).Hide()
	RestoreAll()
	for name, buf := range map[string]*bytes.Buffer{"first": &first, "second": &second} {
		if got, want := buf.String(), HideCursorSequence+ShowCursorSequence; got != want {
			t.Errorf("%s cursor wrote %q, want %q", name, got, want)
		}
	}
}

// A frame in progress holds the lock; a signal path must not wait for it
// forever.
func TestShowNowDoesNotWaitForAStalledFrame(t *testing.T) {
	resetRegistry(t)
	var buf bytes.Buffer
	c := NewCursor(&buf)
	c.Hide()
	c.Lock()
	defer c.Unlock()
	buf.Reset()
	c.changeNow(toShown)
	if got := buf.String(); got != ShowCursorSequence {
		t.Fatalf("changeNow wrote %q, want %q", got, ShowCursorSequence)
	}
}

func TestRestoreOnPanicRestoresAndRepanics(t *testing.T) {
	resetRegistry(t)
	var buf bytes.Buffer
	c := NewCursor(&buf)
	c.Hide()
	buf.Reset()

	repanicked := func() (recovered any) {
		defer func() { recovered = recover() }()
		func() {
			defer c.RestoreOnPanic()
			panic("drawing goroutine died")
		}()
		return nil
	}()

	if repanicked != "drawing goroutine died" {
		t.Fatalf("panic was swallowed: %v", repanicked)
	}
	if got := buf.String(); got != ShowCursorSequence {
		t.Fatalf("panic cleanup wrote %q, want %q", got, ShowCursorSequence)
	}
}

func TestNilCursorIsSafe(t *testing.T) {
	var c *Cursor
	c.Hide()
	c.Show()
	if c.Hidden() {
		t.Fatal("a nil cursor reports itself hidden")
	}
	NewCursor(nil).Hide()
}

// A nil Cursor has nothing to show, but the panic it is deferred for still
// has to come through with its own value.
func TestRestoreOnPanicOfNilCursorRepanics(t *testing.T) {
	recovered := func() (recovered any) {
		defer func() { recovered = recover() }()
		var c *Cursor
		defer c.RestoreOnPanic()
		panic("original")
	}()
	if recovered != "original" {
		t.Fatalf("recovered %v, want the original panic value", recovered)
	}
}

// A terminal has one cursor: it stays hidden until the last Cursor that hid
// it shows it again, and only the first hide and the last show are written.
func TestCursorsOfOneTerminalCountTogether(t *testing.T) {
	resetRegistry(t)
	var buf bytes.Buffer
	application := NewCursor(&buf)
	bar := NewCursor(wrapped{&buf})

	application.Hide()
	bar.Hide()
	bar.Show()
	if got, want := buf.String(), HideCursorSequence; got != want {
		t.Fatalf("wrote %q, want %q: the application still hides the cursor", got, want)
	}
	if !application.Hidden() || bar.Hidden() {
		t.Fatalf("Hidden() = %v, %v, want true, false", application.Hidden(), bar.Hidden())
	}
	application.Show()
	if got, want := buf.String(), HideCursorSequence+ShowCursorSequence; got != want {
		t.Fatalf("wrote %q, want %q", got, want)
	}

	// The other way around: whoever hides first, the last show counts.
	buf.Reset()
	bar.Hide()
	application.Hide()
	bar.Show()
	RestoreAll()
	if got, want := buf.String(), HideCursorSequence+ShowCursorSequence; got != want {
		t.Fatalf("wrote %q, want %q", got, want)
	}
	registryMu.Lock()
	left := len(screens)
	registryMu.Unlock()
	if left != 0 {
		t.Fatalf("%d terminals still recorded with no cursor hidden", left)
	}
}

// wrapped is a writer around another, the way term.Links wraps one.
type wrapped struct{ w io.Writer }

func (w wrapped) Write(p []byte) (int, error) { return w.w.Write(p) }
func (w wrapped) Unwrap() io.Writer           { return w.w }

// stallingWriter takes what is written to it until it is stalled; from then
// on every write blocks until release is closed.
type stallingWriter struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	stalled atomic.Bool
	release chan struct{}
}

func (s *stallingWriter) Write(p []byte) (int, error) {
	if s.stalled.Load() {
		<-s.release
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// A terminal that takes no more output, after Ctrl-S or behind a connection
// that hangs, must not keep a signal from ending the process: the show
// sequence gets a moment and no more.
func TestStalledTerminalDoesNotHoldUpTheSignal(t *testing.T) {
	resetRegistry(t)
	raised := make(chan os.Signal, 1)
	swapRaiseHandler(t, func(sig os.Signal) { raised <- sig })

	terminal := &stallingWriter{release: make(chan struct{})}
	c := NewCursor(terminal)
	c.Hide()
	c.Lock() // a frame is being written and makes no progress
	terminal.stalled.Store(true)

	go new(handler).handle(terminateSignals[0])
	select {
	case <-raised:
	case <-time.After(10 * lockTimeout):
		t.Error("the signal was not raised again while the terminal was stalled")
	}

	// The same without a frame in the way: the write itself is what hangs.
	second := &stallingWriter{release: make(chan struct{})}
	NewCursor(second).Hide()
	second.stalled.Store(true)
	restored := make(chan struct{})
	go func() {
		RestoreAll()
		close(restored)
	}()
	select {
	case <-restored:
	case <-time.After(10 * lockTimeout):
		t.Error("RestoreAll did not return while the terminal was stalled")
	}

	close(second.release)
	c.Unlock()
	close(terminal.release)
}

func TestOnRestore(t *testing.T) {
	resetRegistry(t)
	var calls atomic.Int32
	remove := OnRestore(func() { calls.Add(1) })
	OnRestore(nil)() // nothing to call, nothing to remove

	RestoreAll()
	RestoreAll()
	if got := calls.Load(); got != 2 {
		t.Fatalf("restore called %d times by two RestoreAll, want 2", got)
	}

	// Before a terminating signal is raised again.
	raisedAfter := int32(-1)
	swapRaiseHandler(t, func(os.Signal) { raisedAfter = calls.Load() })
	new(handler).handle(terminateSignals[0])
	if raisedAfter != 3 {
		t.Fatalf("signal raised again after %d calls of restore, want 3", raisedAfter)
	}

	// Before a panic goes on.
	func() {
		defer func() { _ = recover() }()
		defer NewCursor(io.Discard).RestoreOnPanic()
		panic("drawing goroutine died")
	}()
	if got := calls.Load(); got != 4 {
		t.Fatalf("restore called %d times after a panic, want 4", got)
	}

	// Not for a stop: nothing would undo it on continue.
	if len(stopSignals) > 0 {
		swapStopHandler(t, func() bool { return true })
		h := new(handler)
		h.handle(stopSignals[0])
		h.continued()
		if got := calls.Load(); got != 4 {
			t.Fatalf("restore called %d times after a stop, want 4", got)
		}
	}

	remove()
	remove()
	RestoreAll()
	if got := calls.Load(); got != 4 {
		t.Fatalf("removed restore was called: %d calls, want 4", got)
	}
}

// Removing one function leaves the others, whatever the order.
func TestOnRestoreRemovesOnlyItsOwn(t *testing.T) {
	resetRegistry(t)
	var first, second, third atomic.Int32
	removeFirst := OnRestore(func() { first.Add(1) })
	removeSecond := OnRestore(func() { second.Add(1) })
	OnRestore(func() { third.Add(1) })
	removeSecond()
	RestoreAll()
	removeFirst()
	removeSecond()
	RestoreAll()
	if first.Load() != 1 || second.Load() != 0 || third.Load() != 2 {
		t.Fatalf("calls = %d, %d, %d, want 1, 0, 2", first.Load(), second.Load(), third.Load())
	}
}

// A registered function that does not return, because the terminal it
// restores is stalled, gets a moment like the show sequence does.
func TestOnRestoreDoesNotHoldUpTheSignal(t *testing.T) {
	resetRegistry(t)
	release := make(chan struct{})
	defer close(release)
	var quick atomic.Bool
	OnRestore(func() { <-release })
	OnRestore(func() { quick.Store(true) })

	raised := make(chan time.Time, 1)
	swapRaiseHandler(t, func(os.Signal) { raised <- time.Now() })
	start := time.Now()
	go new(handler).handle(terminateSignals[0])
	select {
	case at := <-raised:
		if waited := at.Sub(start); waited < lockTimeout/2 || waited > 5*lockTimeout {
			t.Errorf("signal raised again after %v, want about %v", waited, lockTimeout)
		}
	case <-time.After(10 * lockTimeout):
		t.Fatal("a blocked restore function kept the signal from being raised again")
	}
	if !quick.Load() {
		t.Error("the blocked function kept the other one from running")
	}
}

func TestSignalRestoresThenReRaises(t *testing.T) {
	resetRegistry(t)
	var raised []os.Signal
	swapRaiseHandler(t, func(sig os.Signal) { raised = append(raised, sig) })

	var buf bytes.Buffer
	c := NewCursor(&buf)
	c.Hide()
	buf.Reset()

	new(handler).handle(terminateSignals[0])

	if got := buf.String(); got != ShowCursorSequence {
		t.Fatalf("terminating signal wrote %q, want %q", got, ShowCursorSequence)
	}
	if len(raised) != 1 || raised[0] != terminateSignals[0] {
		t.Fatalf("re-raised %v, want [%v]", raised, terminateSignals[0])
	}
	if c.Hidden() {
		t.Fatal("cursor still recorded as hidden after restore")
	}
}

func TestSignalSuspendAndResume(t *testing.T) {
	if len(stopSignals) == 0 {
		t.Skip("platform has no job-control stop signals")
	}
	resetRegistry(t)
	stopped := false
	swapStopHandler(t, func() bool { stopped = true; return true }) // stands in for the stop signal
	swapRaiseHandler(t, func(sig os.Signal) { t.Fatalf("stop signal was re-raised: %v", sig) })

	var buf bytes.Buffer
	c := NewCursor(&buf)
	c.Hide()
	buf.Reset()

	h := new(handler)
	h.handle(stopSignals[0])

	if !stopped {
		t.Fatal("stop signal did not stop the process")
	}
	// Shown for the stop, and not hidden again before the process continues:
	// the stop signal is only on its way when the handler has sent it.
	if got, want := buf.String(), ShowCursorSequence; got != want {
		t.Fatalf("stop signal wrote %q, want %q", got, want)
	}
	if c.Hidden() {
		t.Fatal("cursor hidden again before the process continued")
	}
	if h.overdue == nil {
		t.Fatal("handler does not wait for the stop to turn out")
	}

	buf.Reset()
	h.handle(resumeSignals[0])
	if got, want := buf.String(), HideCursorSequence; got != want {
		t.Fatalf("continue signal wrote %q, want %q", got, want)
	}
	if !c.Hidden() {
		t.Fatal("cursor was not hidden again after continuing")
	}
	if h.overdue != nil {
		t.Fatal("handler still waits for a stop that is over")
	}

	// A second continue signal must not repeat it.
	buf.Reset()
	h.handle(resumeSignals[0])
	if buf.Len() != 0 {
		t.Fatalf("duplicate resume wrote %q", buf.String())
	}
}

// In an orphaned process group the kernel drops the stop signal: no shell is
// there to continue the process. No continue signal follows either, so the
// handler hides the cursors again once the time a stop takes has passed.
func TestSignalStopThatNeverHappensHidesAgain(t *testing.T) {
	if len(stopSignals) == 0 {
		t.Skip("platform has no job-control stop signals")
	}
	resetRegistry(t)
	swapStopHandler(t, func() bool { return true })

	var buf bytes.Buffer
	c := NewCursor(&buf)
	c.Hide()
	buf.Reset()

	h := new(handler)
	h.handle(stopSignals[0])
	select {
	case <-h.overdue:
		h.continued()
	case <-time.After(10 * stopGrace):
		t.Fatal("handler never gave up on the stop")
	}
	if got, want := buf.String(), ShowCursorSequence+HideCursorSequence; got != want {
		t.Fatalf("wrote %q, want %q", got, want)
	}
	if !c.Hidden() {
		t.Fatal("cursor stayed visible although the process was never stopped")
	}
}

// A cursor the program shows while the handler is showing it for a stop is
// not hidden again on resume, whichever of the two comes first.
func TestSuspendDoesNotUndoAShow(t *testing.T) {
	resetRegistry(t)
	for range 500 {
		c := NewCursor(new(lockedBuffer))
		c.Hide()
		var both sync.WaitGroup
		both.Go(suspendAll)
		both.Go(c.Show)
		both.Wait()
		resumeAll()
		if c.Hidden() {
			t.Fatal("resume hid a cursor the program had shown")
		}
	}
}

// lockedBuffer is a buffer two goroutines may write to.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func TestSignalFailedSuspendLeavesCursorVisible(t *testing.T) {
	if len(stopSignals) == 0 {
		t.Skip("platform has no job-control stop signals")
	}
	resetRegistry(t)
	swapStopHandler(t, func() bool { return false }) // the stop failed

	var buf bytes.Buffer
	c := NewCursor(&buf)
	c.Hide()
	buf.Reset()

	h := new(handler)
	h.handle(stopSignals[0])

	// The process kept running, so the cursor stays visible instead of being
	// hidden again behind a stop that never happened.
	if got := buf.String(); got != ShowCursorSequence {
		t.Fatalf("failed stop wrote %q, want %q", got, ShowCursorSequence)
	}
	if c.Hidden() {
		t.Fatal("cursor hidden again after a failed stop")
	}
	// A later SIGCONT must not hide it either.
	h.handle(resumeSignals[0])
	if got := buf.String(); got != ShowCursorSequence {
		t.Fatalf("resume after a failed stop wrote %q", got)
	}
}

func TestSignalResumeAfterUncatchableStop(t *testing.T) {
	if len(resumeSignals) == 0 {
		t.Skip("platform has no continue signal")
	}
	resetRegistry(t)
	var buf bytes.Buffer
	c := NewCursor(&buf)
	c.Hide()
	buf.Reset()

	// SIGSTOP cannot be caught, so nothing was shown: the cursor is still
	// hidden and continuing must leave it that way without writing.
	new(handler).handle(resumeSignals[0])
	if buf.Len() != 0 {
		t.Fatalf("resume without stop wrote %q", buf.String())
	}
	if !c.Hidden() {
		t.Fatal("resume cleared the hidden state")
	}
}

// A cursor shown by the program while it was marked as suspended is not
// hidden again on resume.
func TestShowClearsSuspendedMark(t *testing.T) {
	resetRegistry(t)
	var buf bytes.Buffer
	c := NewCursor(&buf)
	c.Hide()
	suspendAll()
	c.Show()
	buf.Reset()
	resumeAll()
	if buf.Len() != 0 {
		t.Fatalf("resume hid a cursor the program showed: %q", buf.String())
	}
}

// installed reports whether the process-wide handler is installed.
func installed() bool {
	installMu.Lock()
	defer installMu.Unlock()
	return installs > 0
}

func TestInstallSignalRestoreUninstallIsIdempotent(t *testing.T) {
	uninstall := InstallSignalRestore()
	uninstall()
	uninstall()
	if installed() {
		t.Fatal("handler still recorded as installed")
	}
}

// Signal dispositions are process-wide, so installs are counted: the handler
// stays until the last caller that asked for it has uninstalled it,
// whichever order they do it in.
func TestInstallSignalRestoreIsCounted(t *testing.T) {
	first := InstallSignalRestore()
	second := InstallSignalRestore()

	first()
	first() // calling it twice takes one install back, not two
	if !installed() {
		t.Fatal("uninstalling the first install removed the handler the second still needs")
	}
	second()
	if installed() {
		t.Fatal("handler still installed after every install was taken back")
	}

	third := InstallSignalRestore()
	if !installed() {
		t.Fatal("handler not installed again")
	}
	third()
}
