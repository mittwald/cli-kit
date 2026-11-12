//go:build windows

package secret

import (
	"io"
	"os"
)

// promptOutput returns where TTYPrompt writes its question on Windows: the
// console screen buffer, since the console input buffer the answer is read
// from cannot be written to. Without a console it falls back to stderr.
//
// The buffer is opened for reading as well: asking a console handle for its
// mode needs read access, and without it the handle would not be taken for
// a terminal, so a hint's links would stay plain.
func promptOutput(*os.File) (io.Writer, func()) {
	out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		return os.Stderr, func() {}
	}
	return out, func() { _ = out.Close() }
}
