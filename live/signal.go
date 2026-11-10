package live

import (
	"os"
	"os/signal"
	"slices"
	"sync"
	"sync/atomic"
)

var handledSignals = slices.Concat(terminateSignals, stopSignals, resumeSignals)

// signalInstalled guards the single process-wide handler.
var signalInstalled atomic.Bool

// raiseHandler re-raises a signal with its default disposition; stopHandler
// suspends the process. Both are variables so tests can observe them without
// killing or stopping the test process.
var (
	signalActionMu sync.Mutex
	raiseHandler   = raiseDefault
	stopHandler    = stopProcess
)

func raiseSignal(sig os.Signal) {
	signalActionMu.Lock()
	raise := raiseHandler
	signalActionMu.Unlock()
	raise(sig)
}

func stopSelf() bool {
	signalActionMu.Lock()
	stop := stopHandler
	signalActionMu.Unlock()
	return stop()
}

// InstallSignalRestore keeps a hidden cursor from staying hidden when the
// process never reaches its deferred cleanup. It returns the function that
// uninstalls the handler again; that function is safe to call more than
// once.
//
// On Unix, terminating signals (SIGINT, SIGTERM, SIGHUP, SIGQUIT) show every
// hidden [Cursor] and are then re-raised with their default disposition, so
// the exit status and SIGQUIT's goroutine dump are unchanged. SIGTSTP
// (Ctrl-Z) shows the cursors while the process is stopped and hides them
// again once SIGCONT continues it. SIGTTIN and SIGTTOU are left alone: they
// mean the terminal is denied to the process, and writing to it would only
// raise another one. Elsewhere, os.Interrupt and SIGTERM show the cursors
// and end the process with exit status 1.
//
// SIGKILL and SIGSTOP cannot be caught; a process ended or stopped by them
// leaves the cursor hidden.
//
// Signal dispositions are process-wide, so at most one handler is installed.
// While one is active, further calls do nothing and return a function that
// does nothing.
//
// Installing the handler also means the signals it handles no longer end the
// process before the handler has run; a program that handles SIGINT itself
// should call [RestoreAll] from its own handler instead.
func InstallSignalRestore() (uninstall func()) {
	if !signalInstalled.CompareAndSwap(false, true) {
		return func() {}
	}
	notify := make(chan os.Signal, len(handledSignals)+1)
	signal.Notify(notify, handledSignals...)
	done := make(chan struct{})
	var once sync.Once
	go func() {
		for {
			select {
			case sig := <-notify:
				handleSignal(sig)
			case <-done:
				return
			}
		}
	}()
	return func() {
		once.Do(func() {
			signal.Stop(notify)
			close(done)
			signalInstalled.Store(false)
		})
	}
}

func handleSignal(sig os.Signal) {
	switch {
	case slices.Contains(resumeSignals, sig):
		resumeAll()
	case slices.Contains(stopSignals, sig):
		suspendAll()
		// SIGSTOP rather than a re-raised SIGTSTP: SIGSTOP cannot be caught,
		// so it always stops the process and never re-enters this handler.
		// The call returns once SIGCONT continues the process.
		if stopSelf() {
			resumeAll()
			return
		}
		// The process was never stopped, so it keeps running with visible
		// cursors rather than hiding them again.
		forgetSuspended()
	default:
		RestoreAll()
		raiseSignal(sig)
	}
}
