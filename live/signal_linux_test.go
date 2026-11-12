//go:build linux

package live

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestSignalSuspendsAndResumesChild drives the Ctrl-Z path in a real
// process: the child has to show the cursor, actually stop with the cursor
// still shown, hide it again when continued, and still show it on the
// interrupt that follows. It reads the child's state from /proc, so it runs
// on Linux only.
func TestSignalSuspendsAndResumesChild(t *testing.T) {
	if os.Getenv(childEnv) == "suspend" {
		suspendSelfWithHiddenCursor()
		return
	}

	needStopSignal(t)
	suspendAndResumeChild(t)
}

// TestSignalDoesNotStopWhereStopSignalsAreIgnored starts the child with
// SIGTSTP and SIGTTIN ignored, as a shell starts the commands of a command
// substitution. Such a process is not to be stopped from the keyboard: the
// cursor shows for a moment and is hidden again, and the child keeps
// running.
func TestSignalDoesNotStopWhereStopSignalsAreIgnored(t *testing.T) {
	switch os.Getenv(childEnv) {
	case "ignore":
		// Ignored signals stay ignored across exec. The test process must
		// not ignore them itself: os/signal cannot undo that for it.
		signal.Ignore(syscall.SIGTSTP, syscall.SIGTTIN)
		if err := os.Setenv(childEnv, "ignored"); err != nil {
			os.Exit(5)
		}
		if err := syscall.Exec(os.Args[0], os.Args, os.Environ()); err != nil { //nolint:gosec // the test runs itself again
			os.Exit(5)
		}
	case "ignored":
		waitWithHiddenCursor()
		return
	}

	command := exec.Command(os.Args[0], "-test.run=^TestSignalDoesNotStopWhereStopSignalsAreIgnored$")
	command.Env = append(os.Environ(), childEnv+"=ignore")
	output := startChild(t, command)

	output.expect(t, HideCursorSequence, "child did not hide the cursor")
	if err := command.Process.Signal(syscall.SIGTSTP); err != nil {
		t.Fatalf("stop child: %v", err)
	}
	output.expect(t, ShowCursorSequence, "SIGTSTP did not show the cursor")
	output.expect(t, HideCursorSequence, "child that was never stopped did not hide the cursor again")
	if state := processState(t, command.Process.Pid); state == 'T' {
		t.Fatal("child started with its stop signals ignored was stopped")
	}

	if err := command.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("interrupt child: %v", err)
	}
	output.expect(t, ShowCursorSequence, "interrupt did not show the cursor")
	expectDeathBySignal(t, command, syscall.SIGINT)
}

// needStopSignal skips a test of the Ctrl-Z path where the test itself was
// started with the terminal's stop signals ignored, as the commands of a
// shell's command substitution are: its children inherit that, and are
// never stopped. The kernel's record is read, since signal.Ignored only
// knows the signals a Go program ignored itself.
func needStopSignal(t *testing.T) {
	t.Helper()
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatalf("read signal dispositions: %v", err)
	}
	for line := range strings.SplitSeq(string(status), "\n") {
		value, ok := strings.CutPrefix(line, "SigIgn:")
		if !ok {
			continue
		}
		ignored, err := strconv.ParseUint(strings.TrimSpace(value), 16, 64)
		if err != nil {
			t.Fatalf("parse %q: %v", line, err)
		}
		for _, sig := range []syscall.Signal{syscall.SIGTSTP, syscall.SIGTTIN} {
			if ignored&(1<<(uint(sig)-1)) != 0 {
				t.Skipf("%v is ignored in this environment", sig)
			}
		}
	}
}

// suspendAndResumeChild runs the child of TestSignalSuspendsAndResumesChild
// through a stop, a continue and an interrupt.
func suspendAndResumeChild(t *testing.T) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestSignalSuspendsAndResumesChild$")
	command.Env = append(os.Environ(), childEnv+"=suspend")
	// A process group of its own, with this test as the parent outside it
	// that can continue it. In the group it would inherit, the child is
	// never stopped where that group is orphaned, as under setsid or in a
	// container without a shell around the tests.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	output := startChild(t, command)

	output.expect(t, HideCursorSequence, "child did not hide the cursor")
	output.expect(t, ShowCursorSequence, "SIGTSTP did not show the cursor")
	waitForProcessState(t, command.Process.Pid, 'T')
	// The stop signal reaches the process a moment after the handler sent
	// it. A handler that hid the cursor again right away left the stopped
	// process with a hidden cursor.
	output.expectNothing(t, "child hid the cursor again before it was continued")

	if err := command.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatalf("continue child: %v", err)
	}
	output.expect(t, HideCursorSequence, "continued child did not hide the cursor again")

	if err := command.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("interrupt child: %v", err)
	}
	output.expect(t, ShowCursorSequence, "interrupt after continuing did not show the cursor")
	expectDeathBySignal(t, command, syscall.SIGINT)
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

// TestSignalDoesNotStopAnOrphanedProcessGroup runs the child in a session of
// its own, the way ssh or docker exec run a command without a shell around
// it. The kernel never stops such a process for SIGTSTP, since nothing
// could continue it, and the handler must not either: the cursor shows for
// a moment and is hidden again, and the process keeps running.
func TestSignalDoesNotStopAnOrphanedProcessGroup(t *testing.T) {
	if os.Getenv(childEnv) == "orphan" {
		waitWithHiddenCursor()
		return
	}

	needStopSignal(t)
	command := exec.Command(os.Args[0], "-test.run=^TestSignalDoesNotStopAnOrphanedProcessGroup$")
	command.Env = append(os.Environ(), childEnv+"=orphan")
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	output := startChild(t, command)

	output.expect(t, HideCursorSequence, "child did not hide the cursor")
	if err := command.Process.Signal(syscall.SIGTSTP); err != nil {
		t.Fatalf("stop child: %v", err)
	}
	output.expect(t, ShowCursorSequence, "SIGTSTP did not show the cursor")
	output.expect(t, HideCursorSequence, "child that was never stopped did not hide the cursor again")
	if state := processState(t, command.Process.Pid); state == 'T' {
		t.Fatal("child in an orphaned process group was stopped, with nothing to continue it")
	}

	if err := command.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("interrupt child: %v", err)
	}
	output.expect(t, ShowCursorSequence, "interrupt did not show the cursor")
	expectDeathBySignal(t, command, syscall.SIGINT)
}

func waitWithHiddenCursor() {
	defer InstallSignalRestore()()
	NewCursor(os.Stdout).Hide()
	time.Sleep(30 * time.Second) // the parent interrupts the child
	os.Exit(3)
}

// TestSignalLeavesCursorShownInTheBackground gives the child a terminal of
// its own and puts another process group into its foreground, which is
// where a shell's bg leaves a job. Continuing must not hide the cursor
// there: it belongs to whoever is in the foreground.
func TestSignalLeavesCursorShownInTheBackground(t *testing.T) {
	if os.Getenv(childEnv) == "background" {
		waitInTheBackgroundWithHiddenCursor()
		return
	}

	needStopSignal(t)
	master, slave := openTerminal(t)
	command := exec.Command(os.Args[0], "-test.run=^TestSignalLeavesCursorShownInTheBackground$")
	command.Env = append(os.Environ(), childEnv+"=background")
	command.Stdin, command.Stdout = slave, slave
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := command.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })
	_ = slave.Close()
	output := readChild(master)

	output.expect(t, HideCursorSequence, "child did not hide the cursor")
	if err := command.Process.Signal(syscall.SIGTSTP); err != nil {
		t.Fatalf("stop child: %v", err)
	}
	output.expect(t, ShowCursorSequence, "SIGTSTP did not show the cursor")
	if err := command.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatalf("continue child: %v", err)
	}
	time.Sleep(2 * stopGrace)
	output.expectNothing(t, "child in the background hid the cursor of a terminal it does not have")
}

func waitInTheBackgroundWithHiddenCursor() {
	// Another process group takes the foreground of the terminal.
	other := exec.Command("sleep", "30")
	other.SysProcAttr = &syscall.SysProcAttr{Foreground: true, Ctty: int(os.Stdin.Fd())}
	if err := other.Start(); err != nil {
		os.Exit(4)
	}
	defer func() { _ = other.Process.Kill() }()
	if foreground(os.Stdout) {
		os.Exit(5)
	}
	// The cursor writes through a decorator, as a program's own output
	// writers do: the terminal behind it is the one asked.
	decorated := wrapped{w: os.Stdout}
	if foreground(decorated) {
		os.Exit(6)
	}
	defer InstallSignalRestore()()
	NewCursor(decorated).Hide()
	time.Sleep(30 * time.Second) // the parent kills the child
	os.Exit(3)
}

// openTerminal returns both ends of a new pseudo-terminal.
func openTerminal(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	connection, err := master.SyscallConn()
	if err != nil {
		t.Fatalf("pseudo-terminal: %v", err)
	}
	var (
		number   int
		ioctlErr error
	)
	if err := connection.Control(func(fd uintptr) {
		if ioctlErr = unix.IoctlSetPointerInt(int(fd), unix.TIOCSPTLCK, 0); ioctlErr == nil {
			number, ioctlErr = unix.IoctlGetInt(int(fd), unix.TIOCGPTN)
		}
	}); err != nil || ioctlErr != nil {
		t.Skipf("no pseudo-terminal: %v", errors.Join(err, ioctlErr))
	}
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	return master, slave
}

// The two Cursors of a program's standard streams hide one cursor when both
// streams are the same terminal, and a writer that is no terminal counts as
// the foreground.
func TestStreamsOfOneTerminalShareACursor(t *testing.T) {
	resetRegistry(t)
	master, slave := openTerminal(t)
	second, err := os.OpenFile(slave.Name(), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open the terminal again: %v", err)
	}
	defer func() { _ = second.Close() }()
	defer func() { _ = slave.Close() }()
	output := readChild(master)

	stdout, stderr := NewCursor(slave), NewCursor(second)
	stdout.Hide()
	stderr.Hide()
	stderr.Show()
	output.expect(t, HideCursorSequence, "first hide was not written")
	output.expectNothing(t, "a second hide or an early show was written to the same terminal")
	stdout.Show()
	output.expect(t, ShowCursorSequence, "last show was not written")

	if !foreground(new(bytes.Buffer)) || !foreground(slave) {
		t.Fatal("a writer without a foreground process group does not count as the foreground")
	}
}

// childOutput is what a child process wrote, read as it arrives.
type childOutput chan byte

// startChild starts command and returns what it writes to its standard
// output.
func startChild(t *testing.T, command *exec.Cmd) childOutput {
	t.Helper()
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })
	return readChild(stdout)
}

func readChild(r io.Reader) childOutput {
	output := make(childOutput, 256)
	go func() {
		defer close(output)
		buf := make([]byte, 64)
		for {
			n, err := r.Read(buf)
			for _, b := range buf[:n] {
				output <- b
			}
			if err != nil {
				return
			}
		}
	}()
	return output
}

// expect reads the next len(want) bytes and fails with message unless they
// are want.
func (o childOutput) expect(t *testing.T, want, message string) {
	t.Helper()
	var got []byte
	timeout := time.After(5 * time.Second)
	for len(got) < len(want) {
		select {
		case b, ok := <-o:
			if !ok {
				t.Fatalf("%s: read %q, then the output ended", message, got)
			}
			got = append(got, b)
		case <-timeout:
			t.Fatalf("%s: read %q, want %q", message, got, want)
		}
	}
	if string(got) != want {
		t.Fatalf("%s: read %q, want %q", message, got, want)
	}
}

// expectNothing fails with message when anything is written within the time
// a stop takes to turn out.
func (o childOutput) expectNothing(t *testing.T, message string) {
	t.Helper()
	select {
	case b, ok := <-o:
		if ok {
			t.Fatalf("%s: read %q", message, string(b))
		}
	case <-time.After(2 * stopGrace):
	}
}

func expectDeathBySignal(t *testing.T, command *exec.Cmd, want syscall.Signal) {
	t.Helper()
	err := command.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("child exited with %v, want death by %v", err, want)
	}
	if status, ok := exitErr.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != want {
		t.Fatalf("child exit status = %v, want signaled %v", exitErr.ProcessState, want)
	}
}

// processState returns the /proc state letter of the process ('T' for
// stopped).
func processState(t *testing.T, pid int) byte {
	t.Helper()
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)) //nolint:gosec // the path holds nothing but a process ID
	if err != nil {
		t.Fatalf("read process state: %v", err)
	}
	// The state is the field after the parenthesized command name.
	fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
	if len(fields) == 0 {
		t.Fatalf("process state missing in %q", stat)
	}
	return fields[0][0]
}

// waitForProcessState blocks until the process reports the wanted /proc
// state letter.
func waitForProcessState(t *testing.T, pid int, want byte) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		state := processState(t, pid)
		if state == want {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("process state = %q, want %q", string(state), string(want))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
