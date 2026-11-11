//go:build windows

package secret

import (
	"io"
	"os"
)

// promptOutput returns where TTYPrompt writes its question on Windows: the
// console screen buffer, since the console input buffer the answer is read
// from cannot be written to. Without a console it falls back to stderr.
func promptOutput(*os.File) (io.Writer, func()) {
	out, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return os.Stderr, func() {}
	}
	return out, func() { _ = out.Close() }
}
