package secret

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mittwald/cli-kit/live"
	"github.com/mittwald/cli-kit/term"
)

// openPTY returns both ends of a new pseudo terminal.
func openPTY(t *testing.T) (master, tty *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo terminal: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	// Not through Fd, which would make reads ignore their deadline.
	conn, err := master.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var n int
	var ioctlErr error
	if err := conn.Control(func(fd uintptr) {
		if ioctlErr = unix.IoctlSetPointerInt(int(fd), unix.TIOCSPTLCK, 0); ioctlErr == nil {
			n, ioctlErr = unix.IoctlGetInt(int(fd), unix.TIOCGPTN)
		}
	}); err != nil || ioctlErr != nil {
		t.Skipf("set up pseudo terminal: %v", errors.Join(err, ioctlErr))
	}
	tty, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("open pseudo terminal: %v", err)
	}
	t.Cleanup(func() { _ = tty.Close() })
	return master, tty
}

// readAvailable reads what the terminal shows until it stays quiet.
func readAvailable(t *testing.T, master *os.File) string {
	t.Helper()
	var shown bytes.Buffer
	buf := make([]byte, 4096)
	for {
		if err := master.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
			t.Fatal(err)
		}
		n, err := master.Read(buf)
		shown.Write(buf[:n])
		if err != nil {
			return shown.String()
		}
	}
}

// On a terminal the hint comes first, with its URL a hyperlink, and the
// field is asked the request's question; a repeated question has no hint.
func TestInputPromptOnATerminal(t *testing.T) {
	// Hyperlinks depend on the environment: NO_COLOR, TERM=dumb and the
	// process-wide color mode turn them off.
	t.Setenv("NO_COLOR", "")
	_ = os.Unsetenv("NO_COLOR")
	t.Setenv("TERM", "xterm-256color")
	previous := term.CurrentColorMode()
	term.SetColorMode(term.ColorAuto)
	t.Cleanup(func() { term.SetColorMode(previous) })

	master, tty := openPTY(t)
	var asked []string
	prompt := InputPrompt(tty, tty, func(in, out *os.File, question string) (string, error) {
		if in != tty || out != tty {
			t.Errorf("input got %v and %v, want the terminal", in.Name(), out.Name())
		}
		asked = append(asked, question)
		return "typed", nil
	})

	value, err := prompt(Request{Label: "API token", Name: "API token", Hint: "Create one at https://example.com/tokens."})
	if err != nil || value != "typed" {
		t.Fatalf("prompt = %q, %v; want the field's answer", value, err)
	}
	shown := readAvailable(t, master)
	link := "\x1b]8;;https://example.com/tokens\x1b\\https://example.com/tokens\x1b]8;;\x1b\\"
	if want := "Create one at " + link + "."; !strings.Contains(shown, want) {
		t.Errorf("terminal shows %q, want the hint %q", shown, want)
	}

	if _, err := prompt(Request{Label: "API token", Name: "API token", Hint: "Create one.", Repeat: true}); err != nil {
		t.Fatal(err)
	}
	if shown := readAvailable(t, master); strings.Contains(shown, "Create one.") {
		t.Errorf("the repeated question showed the hint again: %q", shown)
	}
	if want := []string{"API token: ", "Repeat API token: "}; strings.Join(asked, "|") != strings.Join(want, "|") {
		t.Errorf("questions = %q, want %q", asked, want)
	}
}

// The field's own errors, such as Esc, come back as they are.
func TestInputPromptReturnsTheFieldsError(t *testing.T) {
	_, tty := openPTY(t)
	back := errors.New("prompt aborted: went back")
	prompt := InputPrompt(tty, tty, func(*os.File, *os.File, string) (string, error) { return "", back })
	if _, err := prompt(Request{Label: "Password"}); !errors.Is(err, back) {
		t.Fatalf("prompt = %v, want the field's error", err)
	}
}

// echoes reports whether the terminal echoes what is typed.
func echoes(t *testing.T, tty *os.File) bool {
	t.Helper()
	conn, err := tty.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var on bool
	var ioctlErr error
	if err := conn.Control(func(fd uintptr) {
		var termios *unix.Termios
		if termios, ioctlErr = unix.IoctlGetTermios(int(fd), unix.TCGETS); ioctlErr == nil {
			on = termios.Lflag&unix.ECHO != 0
		}
	}); err != nil || ioctlErr != nil {
		t.Fatalf("read terminal state: %v", errors.Join(err, ioctlErr))
	}
	return on
}

// promptHelperEnv makes the test binary ask with TTYPrompt on its
// controlling terminal instead of skipping TestTTYPromptHelper. With the
// value "live" it installs the signal handler of package live first, as a
// program that draws does.
const promptHelperEnv = "CLI_KIT_SECRET_PROMPT_HELPER"

func TestTTYPromptHelper(t *testing.T) {
	switch os.Getenv(promptHelperEnv) {
	case "":
		t.Skip("runs in a copy of the test binary started by the TTYPrompt tests")
	case "live":
		defer live.InstallSignalRestore()()
	}
	value, err := TTYPrompt(Request{Label: "Password"})
	fmt.Fprintf(os.Stderr, "answer=%q err=%v\n", value, err)
}

// startPrompt runs TTYPrompt in a copy of the test binary that has tty for
// its controlling terminal, and returns once the prompt waits for the
// answer with echo turned off. mode is "plain", or "live" for a program
// that installed the signal handler of package live.
func startPrompt(t *testing.T, master, tty *os.File, mode string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestTTYPromptHelper$")
	cmd.Env = append(os.Environ(), promptHelperEnv+"="+mode)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	var shown string
	for deadline := time.Now().Add(20 * time.Second); !strings.Contains(shown, "Password: ") || echoes(t, tty); {
		if time.Now().After(deadline) {
			t.Fatalf("the prompt does not wait with echo off; terminal shows %q", shown)
		}
		shown += readAvailable(t, master)
	}
	return cmd
}

// A process that dies at the prompt gives the terminal its echo back
// first: otherwise the user's shell shows nothing of what is typed next.
// That holds for a program on its own, and for one that installed the
// signal handler of package live, which ends the process by itself: the
// terminal is restored whichever of the two handlers is first.
func TestTTYPromptRestoresEchoOnSignal(t *testing.T) {
	type send func(master *os.File, cmd *exec.Cmd) error
	ctrlC := func(master *os.File, _ *exec.Cmd) error {
		_, err := master.Write([]byte{3})
		return err
	}
	kill := func(sig syscall.Signal) send {
		return func(_ *os.File, cmd *exec.Cmd) error { return cmd.Process.Signal(sig) }
	}
	for _, tc := range []struct {
		name   string
		mode   string
		signal syscall.Signal
		send   send
	}{
		{"Ctrl-C", "plain", syscall.SIGINT, ctrlC},
		{"SIGTERM", "plain", syscall.SIGTERM, kill(syscall.SIGTERM)},
		{"SIGHUP", "plain", syscall.SIGHUP, kill(syscall.SIGHUP)},
		{"Ctrl-C with the handler of live", "live", syscall.SIGINT, ctrlC},
		{"SIGTERM with the handler of live", "live", syscall.SIGTERM, kill(syscall.SIGTERM)},
		{"SIGHUP with the handler of live", "live", syscall.SIGHUP, kill(syscall.SIGHUP)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			master, tty := openPTY(t)
			if !echoes(t, tty) {
				t.Skip("the pseudo terminal starts without echo")
			}
			cmd := startPrompt(t, master, tty, tc.mode)
			if err := tc.send(master, cmd); err != nil {
				t.Fatal(err)
			}
			err := cmd.Wait()
			// The signal still ends the process the way it does by default.
			status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
			if !ok || !status.Signaled() || status.Signal() != tc.signal {
				t.Errorf("the prompt ended with %v, want it killed by %v", err, tc.signal)
			}
			if !echoes(t, tty) {
				t.Error("the terminal is left with echo off")
			}
			if shown := readAvailable(t, master); !strings.Contains(shown, "\n") {
				t.Errorf("terminal shows %q after the signal, want the prompt's line ended", shown)
			}
		})
	}
}

// An answered prompt leaves the terminal as it was, and takes the signals
// back: a later Ctrl-C is the program's own business again.
func TestTTYPromptAnswered(t *testing.T) {
	master, tty := openPTY(t)
	if !echoes(t, tty) {
		t.Skip("the pseudo terminal starts without echo")
	}
	cmd := startPrompt(t, master, tty, "live")
	if _, err := master.WriteString("hunter2\n"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the prompt ended with %v", err)
	}
	shown := readAvailable(t, master)
	if !strings.Contains(shown, `answer="hunter2" err=<nil>`) {
		t.Errorf("terminal shows %q, want the answer reported", shown)
	}
	if strings.Count(shown, "hunter2") != 1 {
		t.Errorf("the answer was echoed: %q", shown)
	}
	if !echoes(t, tty) {
		t.Error("the terminal is left with echo off")
	}
}
