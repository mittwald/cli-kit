//go:build !windows

package secret

import (
	"io"
	"os"
)

// promptOutput returns where TTYPrompt writes its question: the terminal it
// reads the answer from.
func promptOutput(tty *os.File) (io.Writer, func()) {
	return tty, func() {}
}
