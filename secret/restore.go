package secret

import (
	"io"
	"os"
	"sync"
	"time"

	xterm "golang.org/x/term"

	"github.com/mittwald/cli-kit/live"
)

// endLineTimeout is how long a restore waits for the terminal to take the
// newline that ends the prompt's line. It is the moment package live gives
// the functions registered with it.
const endLineTimeout = 100 * time.Millisecond

// restoreOnExit guards a read with echo turned off on tty. Until the
// returned stop is called, every way the process ends without reaching its
// deferred calls puts the terminal back into the state it is in now and
// ends the prompt's line on out: the ways package live knows of, through
// [live.OnRestore], and the signals watchSignals handles itself. Without it
// the process would die with the echo still off, and the user's shell would
// show nothing of what is typed next.
//
// Both may restore at the same time, and both may do so while the read
// returns the ordinary way, so the restoring is safe to run concurrently
// and repeatedly, and does nothing once stop has returned: by then tty may
// be closed, and its descriptor may belong to another file.
func restoreOnExit(tty *os.File, out io.Writer) (stop func()) {
	fd := int(tty.Fd())
	state, err := xterm.GetState(fd)
	if err != nil {
		return func() {}
	}
	var (
		mu          sync.Mutex
		over, ended bool
	)
	restore := func() {
		mu.Lock()
		defer mu.Unlock()
		if over {
			return
		}
		_ = xterm.Restore(fd, state)
		if ended {
			return
		}
		ended = true
		// Whoever restores at the same time waits here as well, and so does
		// not end the process before the line is ended. Not for long: a
		// terminal that takes no output must not keep a signal from ending
		// the process.
		written := make(chan struct{})
		go func() {
			defer close(written)
			_, _ = io.WriteString(out, "\n")
		}()
		timer := time.NewTimer(endLineTimeout)
		defer timer.Stop()
		select {
		case <-written:
		case <-timer.C:
		}
	}
	remove := live.OnRestore(restore)
	unwatch := watchSignals(restore)
	return func() {
		unwatch()
		remove()
		mu.Lock()
		over = true
		mu.Unlock()
	}
}
