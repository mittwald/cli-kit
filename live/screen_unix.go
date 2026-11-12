//go:build unix

package live

import (
	"io"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/mittwald/cli-kit/term"
)

// terminalDevice holds the device number of a terminal: what the standard
// streams of a program share when they are open on the same terminal. The
// number is kept in the integer type the platform has for it.
type terminalDevice struct{ number any }

// terminalID returns the device of the terminal w is open on, and false for
// a writer that is no terminal.
func terminalID(w io.Writer) (any, bool) {
	if !term.IsTerminal(w) {
		return nil, false
	}
	var (
		stat  unix.Stat_t
		found bool
	)
	descriptor(w, func(fd uintptr) { found = unix.Fstat(int(fd), &stat) == nil })
	if !found {
		return nil, false
	}
	return terminalDevice{number: stat.Rdev}, true
}

// foreground reports whether the process may draw on the terminal w is open
// on: its process group is the terminal's foreground process group. A writer
// that is no terminal, or one that does not tell, counts as the foreground.
func foreground(w io.Writer) bool {
	inForeground := true
	descriptor(w, func(fd uintptr) {
		group, err := unix.IoctlGetInt(int(fd), unix.TIOCGPGRP)
		if err != nil {
			return
		}
		if own, err := unix.Getpgid(0); err == nil {
			inForeground = group == own
		}
	})
	return inForeground
}

// descriptor calls use with the file descriptor of w, or of the writer w is
// a decorator around: an *os.File or anything else that hands out its
// descriptor the same way. It goes through SyscallConn rather than Fd, which
// would put the file into blocking mode.
func descriptor(w io.Writer, use func(fd uintptr)) {
	file, ok := innermost(w).(interface {
		SyscallConn() (syscall.RawConn, error)
	})
	if !ok {
		return
	}
	if conn, err := file.SyscallConn(); err == nil {
		_ = conn.Control(use)
	}
}
