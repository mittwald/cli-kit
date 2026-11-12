//go:build unix

package pick

import (
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// An escape sequence split across two writes reaches grace through the real
// input, so an arrow key is not taken for Esc, and grace waits before giving
// up on a lone ESC.
func TestFileInputGraceJoinsSplitEscape(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	in := newFileInput(r)
	defer in.close()
	go func() {
		_, _ = w.Write([]byte{0x1b})
		time.Sleep(10 * time.Millisecond)
		_, _ = w.Write([]byte("[B"))
	}()
	first, err := in.read()
	if err != nil || string(first) != "\x1b" {
		t.Fatalf("read = %q, %v", first, err)
	}
	if got := string(in.grace()); got != "[B" {
		t.Errorf("grace = %q, want the rest of the sequence", got)
	}
	start := time.Now()
	if got := in.grace(); len(got) != 0 {
		t.Errorf("grace with nothing pending = %q", got)
	}
	if time.Since(start) < escapeGrace/2 {
		t.Error("grace returned before waiting")
	}
}

// A read that waits for a key is woken by wake, which is how rows a view
// loaded reach a list nobody is typing into, and ended by interrupt, which
// is what a signal does.
func TestFileInputReadCanBeWoken(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	in := newFileInput(r)
	defer in.close()
	if !in.wakes() {
		t.Fatal("wakes() = false on Unix")
	}

	go func() {
		time.Sleep(10 * time.Millisecond)
		in.wake()
		in.wake()
	}()
	if keys, err := in.read(); !errors.Is(err, errWoken) || len(keys) != 0 {
		t.Fatalf("read() = %q, %v; want errWoken", keys, err)
	}
	// Keys still arrive after a wake, and are not lost to one before it.
	_, _ = w.Write([]byte("x"))
	for {
		keys, err := in.read()
		if errors.Is(err, errWoken) {
			continue
		}
		if err != nil || string(keys) != "x" {
			t.Fatalf("read() = %q, %v; want the key typed", keys, err)
		}
		break
	}

	go func() {
		time.Sleep(10 * time.Millisecond)
		in.interrupt()
	}()
	if _, err := in.read(); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("read() error = %v, want ErrInterrupted", err)
	}
	if _, err := in.read(); !errors.Is(err, ErrInterrupted) {
		t.Errorf("read() after an interrupt = %v, want ErrInterrupted again", err)
	}
}

// A terminal that went away fails its reads with EIO rather than ending
// them: nobody is left to answer, which is an interrupt and not a failure.
func TestDriveTakesAHangUpForAnInterrupt(t *testing.T) {
	l := testList(t, 5, []string{"a"})
	failure := &os.PathError{Op: "read", Path: "/dev/stdin", Err: syscall.EIO}
	err := drive(l, &renderer{w: io.Discard}, fixedSize(80, 24), 0, &scriptedInput{err: failure})
	if !errors.Is(err, ErrInterrupted) {
		t.Errorf("drive() error = %v, want ErrInterrupted", err)
	}
	// Any other failure is still reported as what it is.
	failure = &os.PathError{Op: "read", Path: "/dev/stdin", Err: syscall.EBADF}
	err = drive(l, &renderer{w: io.Discard}, fixedSize(80, 24), 0, &scriptedInput{err: failure})
	if !errors.Is(err, syscall.EBADF) || errors.Is(err, ErrAborted) {
		t.Errorf("drive() error = %v, want the read failure", err)
	}
}

// watchRaises replaces raiseSignal for a test and returns the channel the
// signals passed on arrive in.
func watchRaises(t *testing.T) <-chan os.Signal {
	t.Helper()
	raised := make(chan os.Signal, 4)
	was := raiseSignal
	raiseSignal = func(sig os.Signal) { raised <- sig }
	t.Cleanup(func() { raiseSignal = was })
	return raised
}

func expectSignal(t *testing.T, from <-chan os.Signal, want os.Signal, what string) {
	t.Helper()
	select {
	case got := <-from:
		if got != want {
			t.Fatalf("%s: got %v, want %v", what, got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: nothing arrived", what)
	}
}

// A signal that ends the process puts the terminal back first and is then
// passed on, once.
func TestWatchSignalsRestoresThenPassesTheSignalOn(t *testing.T) {
	raised := watchRaises(t)
	restored, survived := make(chan struct{}, 4), make(chan struct{}, 4)
	stop := watchSignals(func() { restored <- struct{}{} }, func() { survived <- struct{}{} })
	defer stop()

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	expectSignal(t, raised, syscall.SIGTERM, "the signal passed on")
	if len(restored) != 1 || len(survived) != 0 {
		t.Fatalf("restore ran %d times and the prompt gave way %d times before the signal was passed on, want 1 and 0", len(restored), len(survived))
	}
	// The process is still there, so the prompt gives way to it.
	select {
	case <-survived:
	case <-time.After(5 * time.Second):
		t.Fatal("the prompt was never told the process lives on")
	}
	stop()
	stop()
	if len(raised) != 0 || len(restored) != 1 || len(survived) != 0 {
		t.Errorf("stopping passed %d more signals on, restored %d times and gave way %d more times", len(raised), len(restored), len(survived))
	}
}

// A prompt that ended leaves the signals as it found them, and a signal the
// process ignores is never watched.
func TestWatchSignalsLeavesNothingBehind(t *testing.T) {
	raised := watchRaises(t)
	own := make(chan os.Signal, 4)
	signal.Notify(own, syscall.SIGHUP)
	defer signal.Stop(own)

	unexpected := func() { t.Error("the prompt reacted to a signal that never reached it") }
	stop := watchSignals(unexpected, unexpected)
	stop()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	expectSignal(t, own, syscall.SIGHUP, "the application's own handler")
	if len(raised) != 0 {
		t.Errorf("a prompt that had ended passed %d signals on", len(raised))
	}

	signal.Ignore(syscall.SIGQUIT)
	defer signal.Reset(syscall.SIGQUIT)
	stop = watchSignals(unexpected, unexpected)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGQUIT); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	stop()
	if !signal.Ignored(syscall.SIGQUIT) {
		t.Error("watching made the process stop ignoring SIGQUIT")
	}
}

// A handler the application installed for itself is kept: it sees the signal
// as delivered and again as passed on, and the process lives.
func TestWatchSignalsKeepsTheApplicationsHandler(t *testing.T) {
	own := make(chan os.Signal, 4)
	signal.Notify(own, syscall.SIGTERM)
	defer signal.Stop(own)
	restored := make(chan struct{}, 1)
	stop := watchSignals(func() { restored <- struct{}{} }, func() {})
	defer stop()

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	expectSignal(t, own, syscall.SIGTERM, "the signal as delivered")
	expectSignal(t, own, syscall.SIGTERM, "the signal as passed on")
	select {
	case <-restored:
	case <-time.After(5 * time.Second):
		t.Fatal("the terminal was not put back")
	}
	if len(own) != 0 {
		t.Errorf("%d more signals reached the application's handler", len(own))
	}
}
