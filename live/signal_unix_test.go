//go:build unix

package live

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestInstallSignalRestoreHandlesRealSignal drives the installed handler
// with an actual signal: SIGHUP would end the process without it.
func TestInstallSignalRestoreHandlesRealSignal(t *testing.T) {
	resetRegistry(t)
	raised := make(chan os.Signal, 1)
	swapRaiseHandler(t, func(sig os.Signal) { raised <- sig })

	uninstall := InstallSignalRestore()
	defer uninstall()

	var buf bytes.Buffer
	c := NewCursor(&buf)
	c.Hide()

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("kill: %v", err)
	}
	select {
	case sig := <-raised:
		if sig != syscall.SIGHUP {
			t.Fatalf("re-raised %v, want SIGHUP", sig)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("signal handler did not run")
	}
	if c.Hidden() {
		t.Fatal("cursor still hidden after SIGHUP")
	}
	if got := buf.String(); got != HideCursorSequence+ShowCursorSequence {
		t.Fatalf("wrote %q, want hide then show", got)
	}
}

const childEnv = "CLI_KIT_LIVE_SIGNAL_CHILD"

// TestSignalPreservesExitStatus runs the test binary again as a child that
// hides the cursor and interrupts itself. The real re-raise has to show the
// cursor and still let the signal end the process.
func TestSignalPreservesExitStatus(t *testing.T) {
	if os.Getenv(childEnv) == "interrupt" {
		interruptSelfWithHiddenCursor()
		return
	}

	command := exec.Command(os.Args[0], "-test.run=^TestSignalPreservesExitStatus$")
	command.Env = append(os.Environ(), childEnv+"=interrupt")
	output, err := command.Output()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("child exited with %v, want death by SIGINT (output %q)", err, output)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGINT {
		t.Fatalf("child exit status = %v, want signaled SIGINT", exitErr.ProcessState)
	}
	if !strings.Contains(string(output), ShowCursorSequence) {
		t.Fatalf("interrupted child did not show the cursor: %q", output)
	}
}

func interruptSelfWithHiddenCursor() {
	defer InstallSignalRestore()()
	NewCursor(os.Stdout).Hide()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGINT); err != nil {
		os.Exit(4)
	}
	time.Sleep(5 * time.Second) // the signal has to land first
	os.Exit(3)
}
