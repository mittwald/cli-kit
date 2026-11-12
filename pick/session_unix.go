//go:build unix

package pick

import (
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// wakeable says a blocked read can be woken here: awaitInput waits on the
// terminal and on a pipe at once. It is what lets a view load its rows, and
// a signal end the prompt, while the prompt waits for a key. An input whose
// descriptors awaitInput cannot wait on does without, see newFileInput.
const wakeable = true

// endingSignals are the signals that end a process by default. One arriving
// while a prompt is up would leave the terminal in raw mode with its cursor
// hidden, since the process ends before any deferred cleanup runs.
var endingSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}

// raiseSignal sends sig to the process itself. It is a variable so a test
// can watch a signal being passed on without dying of it.
var raiseSignal = func(sig os.Signal) {
	if number, ok := sig.(syscall.Signal); ok {
		_ = syscall.Kill(syscall.Getpid(), number)
	}
}

// watchSignals calls restore when a signal that ends the process arrives and
// then raises the signal again, so it still does what it would have done
// without a prompt on screen: with nobody else listening the process ends
// with the signal's own exit status, SIGQUIT's goroutine dump included. A
// process that is still there a moment later handles the signal itself, and
// survived is called for the prompt to give way to it. The function returned
// stops the watching; it is safe to call more than once.
//
// Only the watcher's own registration is taken back before the signal is
// raised again, so a handler the application installed for itself stays in
// place and is not passed over: it sees the signal the system delivered and
// the one raised again. Go offers no way to tell whether such a handler
// exists, which would be the way to raise the signal only where it is
// needed. A signal the process ignores, such as SIGHUP under nohup, is not
// watched at all.
func watchSignals(restore, survived func()) (stop func()) {
	watched := make([]os.Signal, 0, len(endingSignals))
	for _, sig := range endingSignals {
		if !signal.Ignored(sig) {
			watched = append(watched, sig)
		}
	}
	if len(watched) == 0 {
		return func() {}
	}
	notify := make(chan os.Signal, len(watched))
	signal.Notify(notify, watched...)
	pass := func(sig os.Signal) {
		restore()
		signal.Stop(notify)
		raiseSignal(sig)
		// The signal is delivered to whichever thread the system picks, so
		// the process may outlive the call that raised it by a moment.
		time.Sleep(restoreGrace)
		survived()
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		select {
		case sig := <-notify:
			pass(sig)
		case <-done:
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			signal.Stop(notify)
			close(done)
			<-finished
			// A signal that arrived just as the prompt ended is passed on
			// all the same, rather than lost with the channel.
			select {
			case sig := <-notify:
				pass(sig)
			default:
			}
		})
	}
}

// hungUp reports whether err is what reading a terminal that went away
// returns: once the other end of a terminal is closed, a read fails with EIO
// rather than ending in io.EOF.
func hungUp(err error) bool {
	return errors.Is(err, syscall.EIO)
}
