package term

import (
	"errors"
	"io"
	"os"

	xterm "golang.org/x/term"
)

// fdHolder is implemented by *os.File and by anything else that wraps a file
// descriptor.
type fdHolder interface{ Fd() uintptr }

// unwrapper is implemented by writers that decorate another writer, such as
// the one [Links] returns. [IsTerminal] and [Size] look through them.
type unwrapper interface{ Unwrap() io.Writer }

// file finds the file descriptor behind v, looking through writers that
// implement Unwrap() io.Writer.
func file(v any) (fdHolder, bool) {
	for v != nil {
		if f, ok := v.(fdHolder); ok {
			return f, true
		}
		w, ok := v.(unwrapper)
		if !ok {
			return nil, false
		}
		v = w.Unwrap()
	}
	return nil, false
}

// IsTerminal reports whether v is, or wraps, a file that is a terminal. v is
// usually an io.Writer or an io.Reader; a writer wraps another by having an
// Unwrap() io.Writer method.
func IsTerminal(v any) bool {
	f, ok := file(v)
	return ok && xterm.IsTerminal(int(f.Fd()))
}

// Size returns the width and height of the terminal v is, or wraps, in
// cells. It returns 0, 0 when v is no terminal or its size cannot be read.
func Size(v any) (width, height int) {
	f, ok := file(v)
	if !ok {
		return 0, 0
	}
	width, height, err := xterm.GetSize(int(f.Fd()))
	if err != nil {
		return 0, 0
	}
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
