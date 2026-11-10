//go:build !unix

package live

import (
	"os"
	"syscall"
)

// Platforms without POSIX job control deliver only interrupt and terminate.
var (
	terminateSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
	stopSignals      []os.Signal
	resumeSignals    []os.Signal
)

// raiseDefault cannot raise a signal again without POSIX signals, so it
// exits directly.
func raiseDefault(os.Signal) {
	os.Exit(1)
}

// stopProcess is unreachable without stop signals.
func stopProcess() bool { return false }
