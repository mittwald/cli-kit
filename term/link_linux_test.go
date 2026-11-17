package term

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// openTTY returns the terminal end of a new pseudo terminal.
func openTTY(t *testing.T) *os.File {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo terminal: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
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
	tty, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("open pseudo terminal: %v", err)
	}
	t.Cleanup(func() { _ = tty.Close() })
	return tty
}

// Under auto a terminal gets links, unless the environment asks for output
// without escape sequences.
func TestShouldLinkTerminal(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"terminal", nil, true},
		{"NO_COLOR", map[string]string{"NO_COLOR": ""}, false},
		{"TERM=dumb", map[string]string{"TERM": "dumb"}, false},
	}
	tty := openTTY(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TERM", "xterm-256color")
			if value, set := os.LookupEnv("NO_COLOR"); set {
				t.Setenv("NO_COLOR", value) // restored after the test
				_ = os.Unsetenv("NO_COLOR")
			}
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			if got := ShouldLink(ColorAuto, tty); got != tt.want {
				t.Errorf("ShouldLink(auto, terminal) = %v, want %v", got, tt.want)
			}
			if !ShouldLink(ColorAlways, tty) || ShouldLink(ColorNever, tty) {
				t.Error("always or never do not decide alone")
			}
		})
	}
}
