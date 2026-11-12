//go:build unix && !darwin

package pick

import (
	"errors"
	"math"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// awaitInput waits until f has something to read, until the descriptor wake
// has, or until timeout has passed, whichever comes first; a negative
// timeout waits without end and a wake of -1 is not waited on. It reports
// which of the two became readable. A descriptor that failed or hung up
// counts as readable, since the read that follows is what reports it.
//
// It polls the descriptor rather than setting a read deadline: a terminal
// inherited as stdin is not in Go's runtime poller, so os.File deadlines are
// not supported on it.
func awaitInput(f *os.File, wake int, timeout time.Duration) (ready, woken bool) {
	fd := f.Fd()
	if fd > math.MaxInt32 || wake > math.MaxInt32 {
		return false, false
	}
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	if wake >= 0 {
		fds = append(fds, unix.PollFd{Fd: int32(wake), Events: unix.POLLIN})
	}
	deadline := time.Now().Add(timeout)
	for {
		wait := -1
		if timeout >= 0 {
			wait = int(max(time.Until(deadline), 0).Milliseconds())
		}
		n, err := unix.Poll(fds, wait)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || n <= 0 {
			return false, false
		}
		return fds[0].Revents != 0, len(fds) > 1 && fds[1].Revents != 0
	}
}
