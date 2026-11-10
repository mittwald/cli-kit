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
	// show sequence would raise another SIGTTOU and spin the handler.
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

// stopProcess stops the process the way the caught stop signal would have,
// and returns once the process continues. It reports whether the process was
// stopped.
func stopProcess() bool {
	return syscall.Kill(syscall.Getpid(), syscall.SIGSTOP) == nil
}
