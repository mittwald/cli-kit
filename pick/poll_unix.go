//go:build unix && !darwin

package pick

import (
	"errors"
	"math"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// readable reports whether f has input within timeout. It polls the
// descriptor rather than setting a read deadline: a terminal inherited as
// stdin is not in Go's runtime poller, so os.File deadlines are not supported
// on it.
func readable(f *os.File, timeout time.Duration) bool {
	fd := f.Fd()
	if fd > math.MaxInt32 {
		return false
	}
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	deadline := time.Now().Add(timeout)
	for {
		left := max(time.Until(deadline), 0)
		n, err := unix.Poll(fds, int(left.Milliseconds()))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err == nil && n > 0 && fds[0].Revents&unix.POLLIN != 0
	}
}
