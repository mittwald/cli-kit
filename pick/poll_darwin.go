//go:build darwin

package pick

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

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
func awaitInput(f *os.File, wake int, timeout time.Duration) (ready, woken bool) {
	fd := int(f.Fd())
	if fd < 0 || fd >= unix.FD_SETSIZE || wake >= unix.FD_SETSIZE {
		return false, false
	}
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
