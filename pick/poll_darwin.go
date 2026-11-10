//go:build darwin

package pick

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// readable reports whether f has input within timeout. macOS's poll(2) does
// not support devices (see its BUGS section), terminals included, so this
// uses select(2), which does. Like the poll version on other systems it
// works on the descriptor, because a terminal inherited as stdin is not in
// Go's runtime poller and os.File deadlines are not supported on it.
func readable(f *os.File, timeout time.Duration) bool {
	fd := int(f.Fd())
	if fd < 0 || fd >= unix.FD_SETSIZE {
		return false
	}
	deadline := time.Now().Add(timeout)
	for {
		var set unix.FdSet
		set.Set(fd)
		tv := unix.NsecToTimeval(max(time.Until(deadline), 0).Nanoseconds())
		n, err := unix.Select(fd+1, &set, nil, nil, &tv)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err == nil && n > 0 && set.IsSet(fd)
	}
}
