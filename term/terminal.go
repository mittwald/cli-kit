package term

import (
	"errors"
	"io"
	"os"
	"syscall"

	xterm "golang.org/x/term"
)

// fdHolder is implemented by anything that wraps a file descriptor.
type fdHolder interface{ Fd() uintptr }

// rawFile is implemented by *os.File, and by network connections. It hands
// out the file descriptor without changing the file, which Fd does not: the
// Fd of an *os.File puts the file into blocking mode, after which its
// deadlines no longer work and Close no longer ends a Read that waits.
type rawFile interface {
	SyscallConn() (syscall.RawConn, error)
}

// unwrapper is implemented by writers that decorate another writer, such as
// the one [Links] returns. [IsTerminal] and [Size] look through them.
type unwrapper interface{ Unwrap() io.Writer }

// descriptor calls use with the file descriptor behind v, looking through
// writers that implement Unwrap() io.Writer. It does not call use when v has
// no file descriptor, or one that is closed. The descriptor is valid only
// while use runs.
func descriptor(v any, use func(fd uintptr)) {
	for v != nil {
		switch f := v.(type) {
		case rawFile:
			if conn, err := f.SyscallConn(); err == nil {
				_ = conn.Control(use)
			}
			return
		case fdHolder:
			use(f.Fd())
			return
		case unwrapper:
			v = f.Unwrap()
		default:
			return
		}
	}
}

// IsTerminal reports whether v is, or wraps, a file that is a terminal. v is
// usually an io.Writer or an io.Reader; a writer wraps another by having an
// Unwrap() io.Writer method. Asking leaves the file as it is: its deadlines
// keep working afterwards.
func IsTerminal(v any) bool {
	terminal := false
	descriptor(v, func(fd uintptr) { terminal = isTerminal(fd) })
	return terminal
}

// isTerminal reports whether fd is a terminal.
func isTerminal(fd uintptr) bool {
	return xterm.IsTerminal(int(fd))
}

// Size returns the width and height of the terminal v is, or wraps, in
// cells. It returns 0, 0 when v is no terminal or its size cannot be read.
func Size(v any) (width, height int) {
	descriptor(v, func(fd uintptr) {
		w, h, err := xterm.GetSize(int(fd))
		if err == nil {
			width, height = w, h
		}
	})
	return width, height
}

// Columns returns the width of the terminal v is, or wraps, or fallback when
// it is no terminal or its size cannot be read.
func Columns(v any, fallback int) int {
	if width, _ := Size(v); width > 0 {
		return width
	}
	return fallback
}

// Interactive reports whether both in and out are terminals, which is what a
// prompt needs before it can ask anything.
func Interactive(in, out *os.File) bool {
	return in != nil && out != nil && IsTerminal(in) && IsTerminal(out)
}

// ErrNoTTY is returned by [OpenTTY] where no controlling terminal can be
// opened apart from the standard streams.
var ErrNoTTY = errors.New("term: no controlling terminal")
