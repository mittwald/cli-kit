package secret

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
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
