//go:build unix

package secret

import (
	"os"
	"os/signal"
	"syscall"
)

// watchSignals calls restore when a SIGINT, SIGTERM or SIGHUP arrives, and
// then ends the process the way the signal does by default. It serves a
// program that has not installed the handler of package live; where both
// are there, each restores before it lets the signal through, so the
// terminal is restored whichever is first. The returned stop ends the
// watch.
//
// A signal the process ignores is left alone, so a tool run under nohup
// does not start to die of SIGHUP.
func watchSignals(restore func()) (stop func()) {
	var watched []os.Signal
	for _, sig := range []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		if !signal.Ignored(sig) {
			watched = append(watched, sig)
		}
	}
	if len(watched) == 0 {
		return func() {}
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, watched...)
	done, finished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		select {
		case sig := <-signals:
			restore()
			// Back to the default, for every handler: the read of the answer
			// cannot be interrupted, so a handler that only asks the program
			// to wind down would leave it waiting at a prompt.
			signal.Reset(sig)
			if number, ok := sig.(syscall.Signal); ok {
				_ = syscall.Kill(syscall.Getpid(), number)
			}
		case <-done:
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
		<-finished
	}
}
