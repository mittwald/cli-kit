//go:build unix

package term

import "os"

// OpenTTY opens the controlling terminal for reading and writing. It works
// even when stdin and stdout are redirected, which lets a command that reads
// its input from a pipe still ask the user a question. The caller closes the
// file.
func OpenTTY() (*os.File, error) {
	return os.OpenFile("/dev/tty", os.O_RDWR, 0)
}
