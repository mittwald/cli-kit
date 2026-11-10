//go:build windows

package term

import (
	"errors"
	"os"
)

// OpenTTY opens the console input buffer for reading. Windows has no
// /dev/tty; CONIN$ is the console the process is attached to, whatever its
// standard streams point at. The caller closes the file.
func OpenTTY() (*os.File, error) {
	f, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, errors.Join(ErrNoTTY, err)
	}
	return f, nil
}
