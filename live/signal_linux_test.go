//go:build linux

package live

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestSignalSuspendsAndResumesChild drives the Ctrl-Z path in a real
// process: the child has to show the cursor, actually stop, hide the cursor
// again when continued, and still show it on the interrupt that follows.
// It reads the child's state from /proc, so it runs on Linux only.
func TestSignalSuspendsAndResumesChild(t *testing.T) {
	if os.Getenv(childEnv) == "suspend" {
		suspendSelfWithHiddenCursor()
		return
	}

	command := exec.Command(os.Args[0], "-test.run=^TestSignalSuspendsAndResumesChild$")
	command.Env = append(os.Environ(), childEnv+"=suspend")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })

	expectSequence(t, stdout, HideCursorSequence, "child did not hide the cursor")
	expectSequence(t, stdout, ShowCursorSequence, "SIGTSTP did not show the cursor")
	waitForProcessState(t, command.Process.Pid, 'T')

	if err := command.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatalf("continue child: %v", err)
	}
	expectSequence(t, stdout, HideCursorSequence, "continued child did not hide the cursor again")

	if err := command.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("interrupt child: %v", err)
	}
	expectSequence(t, stdout, ShowCursorSequence, "interrupt after continuing did not show the cursor")

	err = command.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("child exited with %v, want death by SIGINT", err)
	}
	if status, ok := exitErr.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != syscall.SIGINT {
		t.Fatalf("child exit status = %v, want signaled SIGINT", exitErr.ProcessState)
	}
}

func suspendSelfWithHiddenCursor() {
	defer InstallSignalRestore()()
	NewCursor(os.Stdout).Hide()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTSTP); err != nil {
		os.Exit(4)
	}
	time.Sleep(30 * time.Second) // the parent interrupts the child after continuing it
	os.Exit(3)
}

func expectSequence(t *testing.T, r io.Reader, want, message string) {
	t.Helper()
	got := make([]byte, len(want))
	if _, err := io.ReadFull(r, got); err != nil {
		t.Fatalf("%s: %v", message, err)
	}
	if string(got) != want {
		t.Fatalf("%s: read %q, want %q", message, got, want)
	}
}

// waitForProcessState blocks until the process reports the wanted /proc
// state letter ('T' for stopped).
func waitForProcessState(t *testing.T, pid int, want byte) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			t.Fatalf("read process state: %v", err)
		}
		// The state is the field after the parenthesized command name.
		if fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:])); len(fields) > 0 {
			if fields[0][0] == want {
				return
			}
			if !time.Now().Before(deadline) {
				t.Fatalf("process state = %q, want %q", fields[0], string(want))
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
}
