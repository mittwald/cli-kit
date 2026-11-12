//go:build unix

package live

import (
	"os"
	"os/signal"
	"syscall"
)

var (
	terminateSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}
	// Only SIGTSTP: SIGTTIN and SIGTTOU stay at their default stop action.
	// They mean the terminal is denied to this process group, so writing the
	// show sequence would raise another SIGTTOU and spin the handler. That
	// SIGTTIN keeps its default action is also what stopProcess relies on.
	stopSignals   = []os.Signal{syscall.SIGTSTP}
	resumeSignals = []os.Signal{syscall.SIGCONT}
)

// raiseDefault restores the default disposition for sig and raises it again,
// so the process ends exactly as it would have without a handler, including
// the 128+n exit status.
func raiseDefault(sig os.Signal) {
	signal.Reset(sig)
	number, ok := sig.(syscall.Signal)
	if !ok {
		os.Exit(1)
	}
	if err := syscall.Kill(syscall.Getpid(), number); err == nil {
		return
	}
	// The signal could not be raised again, so exit with the status a shell
	// would have derived from it.
	os.Exit(128 + int(number))
}

// stopProcess stops the process where the caught SIGTSTP would have stopped
// it, and reports whether the stop signal was sent. The process stops a
// moment later; the continue signal, or the time that passes without one,
// tells the handler how it went.
//
// SIGTSTP itself cannot be raised again: once os/signal has been asked for
// it, signal.Reset leaves the runtime's own handler in place, which drops
// the signal instead of stopping the process. SIGSTOP always stops, also a
// process in an orphaned process group, which the kernel never stops for
// SIGTSTP because no shell is there to continue it. SIGTTIN is a stop
// signal of the terminal like SIGTSTP, falls under the same rule, and still
// has its default disposition, since this package never asks for it.
//
// A process started with SIGTTIN ignored is not stopped. A shell starts the
// commands of a command substitution that way, with SIGTSTP ignored as
// well, so that the keyboard cannot stop them; os/signal cannot tell, since
// signal.Ignored only knows the signals a Go program ignored itself.
func stopProcess() bool {
	return syscall.Kill(syscall.Getpid(), syscall.SIGTTIN) == nil
}
