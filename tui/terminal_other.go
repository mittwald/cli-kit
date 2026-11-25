//go:build !unix

package tui

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// canSuspend says there is no job control to hand the terminal to here.
const canSuspend = false

func stopProcess() {}

// exitAfterSignal ends the process after a signal put the terminal back; a
// variable for tests.
var exitAfterSignal = func() { os.Exit(1) }

// watchSignals puts the terminal back on Ctrl-Break or the console being
// closed, which end the process without its deferred calls, and then ends
// it. There is no SIGWINCH to wait for here, so it asks for a redraw a few
// times a second, which picks a new size up.
func watchSignals(restore, resized func()) (stop func()) {
	ending := make(chan os.Signal, 2)
	signal.Notify(ending, os.Interrupt, syscall.SIGTERM)
	ticker := time.NewTicker(250 * time.Millisecond)
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			select {
			case <-ending:
				restore()
				signal.Stop(ending)
				exitAfterSignal()
				return
			case <-ticker.C:
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
			ticker.Stop()
			close(done)
			<-finished
		})
	}
}
