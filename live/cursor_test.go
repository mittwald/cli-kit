package live

import (
	"bytes"
	"os"
	"testing"
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
	c.showNow()
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
	c.showNow()
	if got := buf.String(); got != ShowCursorSequence {
		t.Fatalf("showNow wrote %q, want %q", got, ShowCursorSequence)
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
	NewCursor(nil).Hide()
}

func TestSignalRestoresThenReRaises(t *testing.T) {
	resetRegistry(t)
	var raised []os.Signal
	swapRaiseHandler(t, func(sig os.Signal) { raised = append(raised, sig) })

	var buf bytes.Buffer
	c := NewCursor(&buf)
	c.Hide()
	buf.Reset()

	handleSignal(terminateSignals[0])

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
	swapStopHandler(t, func() bool { stopped = true; return true }) // stands in for stop and continue
	swapRaiseHandler(t, func(sig os.Signal) { t.Fatalf("stop signal was re-raised: %v", sig) })

	var buf bytes.Buffer
	c := NewCursor(&buf)
	c.Hide()
	buf.Reset()

	handleSignal(stopSignals[0])

	if !stopped {
		t.Fatal("stop signal did not stop the process")
	}
	// Shown for the stop, hidden again once the process continues.
	if got, want := buf.String(), ShowCursorSequence+HideCursorSequence; got != want {
		t.Fatalf("stop signal wrote %q, want %q", got, want)
	}
	if !c.Hidden() {
		t.Fatal("cursor was not hidden again after continuing")
	}

	// The SIGCONT that follows arrives on the channel too and must not
	// repeat it.
	buf.Reset()
	handleSignal(resumeSignals[0])
	if buf.Len() != 0 {
		t.Fatalf("duplicate resume wrote %q", buf.String())
	}
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

	handleSignal(stopSignals[0])

	// The process kept running, so the cursor stays visible instead of being
	// hidden again behind a stop that never happened.
	if got := buf.String(); got != ShowCursorSequence {
		t.Fatalf("failed stop wrote %q, want %q", got, ShowCursorSequence)
	}
	if c.Hidden() {
		t.Fatal("cursor hidden again after a failed stop")
	}
	// A later SIGCONT must not hide it either.
	handleSignal(resumeSignals[0])
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
	handleSignal(resumeSignals[0])
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

func TestInstallSignalRestoreUninstallIsIdempotent(t *testing.T) {
	uninstall := InstallSignalRestore()
	uninstall()
	uninstall()
	if signalInstalled.Load() {
		t.Fatal("handler still recorded as installed")
	}
}

func TestInstallSignalRestoreInstallsOnce(t *testing.T) {
	first := InstallSignalRestore()
	t.Cleanup(first)

	// Signal dispositions are process-wide: the second install must not start
	// a second handler, and uninstalling it must not remove the first.
	second := InstallSignalRestore()
	second()
	if !signalInstalled.Load() {
		t.Fatal("uninstalling a redundant install removed the active handler")
	}
}
