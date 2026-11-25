//go:build unix

package tui

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// canSuspend says Ctrl-Z can hand the terminal back to the shell here.
const canSuspend = true

// continueTimeout bounds the wait for SIGCONT where the stop never came,
// as for a process group the system will not stop.
const continueTimeout = 3 * time.Second

// stopProcess stops the process group as Ctrl-Z would, and returns once it
// was continued. A handler of SIGTSTP, such as package live's, may stop the
// process a while after the signal, so it waits for SIGCONT rather than for
// a fixed time.
func stopProcess() {
	if signal.Ignored(syscall.SIGTSTP) {
		return
	}
	cont := make(chan os.Signal, 1)
	signal.Notify(cont, syscall.SIGCONT)
	defer signal.Stop(cont)
	_ = syscall.Kill(0, syscall.SIGTSTP)
	timer := time.NewTimer(continueTimeout)
	defer timer.Stop()
	select {
	case <-cont:
	case <-timer.C:
	}
}

// endingSignals end a process by default; one arriving while the
// application runs would leave the terminal in raw mode on the alternate
// screen.
var endingSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}

// raiseSignal sends sig to the process itself; a variable for tests.
var raiseSignal = func(sig os.Signal) {
	if number, ok := sig.(syscall.Signal); ok {
		_ = syscall.Kill(syscall.Getpid(), number)
	}
}

// watchSignals calls restore when a signal that ends the process arrives,
// and raises the signal again so it does what it would have done; resized
// is called on SIGWINCH. The function returned stops the watching.
func watchSignals(restore, resized func()) (stop func()) {
	var watched []os.Signal
	for _, sig := range endingSignals {
		if !signal.Ignored(sig) {
			watched = append(watched, sig)
		}
	}
	ending := make(chan os.Signal, len(watched)+1)
	if len(watched) > 0 {
		signal.Notify(ending, watched...)
	}
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			select {
			case sig := <-ending:
				restore()
				signal.Stop(ending)
				raiseSignal(sig)
				return
			case <-winch:
				resized()
			case <-done:
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			signal.Stop(ending)
			signal.Stop(winch)
			close(done)
			<-finished
		})
	}
}
