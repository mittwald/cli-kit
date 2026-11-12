//go:build darwin

package pick

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// maxAwaitFd is the highest descriptor awaitInput can wait on: select(2)
// takes a set of FD_SETSIZE descriptors and none past it. A process with
// that many files open gets a higher one, for the terminal it opens or for
// the pipe a read is woken through. It is a variable so that a test can
// lower it.
var maxAwaitFd = unix.FD_SETSIZE - 1

// awaitInput waits until f has something to read, until the descriptor wake
// has, or until timeout has passed, whichever comes first; a negative
// timeout waits without end and a wake of -1 is not waited on. It reports
// which of the two became readable.
//
// macOS's poll(2) does not support devices (see its BUGS section), terminals
// included, so this uses select(2), which does. Like the poll version on
// other systems it works on the descriptor, because a terminal inherited as
// stdin is not in Go's runtime poller and os.File deadlines are not
// supported on it.
//
// A descriptor past maxAwaitFd cannot be waited on, and nothing is reported
// for it. A prompt asks awaitable first and reads without waiting where a
// descriptor is out of reach, so that it never counts on a wake that cannot
// come.
func awaitInput(f *os.File, wake int, timeout time.Duration) (ready, woken bool) {
	if !awaitable(f.Fd()) || wake > maxAwaitFd {
		return false, false
	}
	fd := int(f.Fd())
	deadline := time.Now().Add(timeout)
	for {
		var set unix.FdSet
		set.Set(fd)
		highest := fd
		if wake >= 0 {
			set.Set(wake)
			highest = max(highest, wake)
		}
		var wait *unix.Timeval
		if timeout >= 0 {
			tv := unix.NsecToTimeval(max(time.Until(deadline), 0).Nanoseconds())
			wait = &tv
		}
		n, err := unix.Select(highest+1, &set, nil, nil, wait)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || n <= 0 {
			return false, false
		}
		return set.IsSet(fd), wake >= 0 && set.IsSet(wake)
	}
}
