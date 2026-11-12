package live

import (
	"os"
	"os/signal"
	"slices"
	"sync"
	"time"
)

var handledSignals = slices.Concat(terminateSignals, stopSignals, resumeSignals)

// stopGrace is how long the handler waits for the continue signal after it
// asked for the process to be stopped. A stop takes effect at once, so a
// process that is still running after this time was never stopped: the
// kernel does not stop a process nothing could continue.
const stopGrace = 250 * time.Millisecond

// The single process-wide handler is installed by the first call of
// InstallSignalRestore and removed by the last uninstall. All three are
// guarded by installMu.
var (
	installMu     sync.Mutex
	installs      int
	removeHandler func()
)

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
// hidden [Cursor], call the functions registered with [OnRestore], and are
// then re-raised with their default disposition, so the exit status and
// SIGQUIT's goroutine dump are unchanged. A terminal that does not take the
// show sequence, or a registered function that does not return, holds the
// signal up for a moment and no longer.
//
// SIGTSTP (Ctrl-Z) shows the cursors and stops the process where the kernel
// would have stopped it: not in an orphaned process group, such as a command
// started by ssh or docker exec without a shell around it, where nothing
// could continue the process again and Ctrl-Z keeps doing nothing. Once
// SIGCONT continues the process the cursors are hidden again, unless the
// process was continued in the background, as a shell's bg does: there they
// stay visible until a SIGCONT finds the process in the foreground. The
// process stops itself with SIGTTIN, the terminal's stop signal that has
// its default disposition, so a shell that names the signal a job was
// stopped by names that one. SIGTTIN and SIGTTOU are otherwise left alone:
// they mean the terminal is denied to the process, and writing to it would
// only raise another one. A process started with the terminal's stop signals
// ignored, as a shell starts the commands of a command substitution, is not
// stopped either: the cursors show for a moment and are hidden again.
// Elsewhere, os.Interrupt and SIGTERM show the
// cursors, call the registered functions and end the process with exit
// status 1.
//
// SIGKILL and SIGSTOP cannot be caught; a process ended or stopped by them
// leaves the cursor hidden.
//
// Signal dispositions are process-wide, so one handler serves every caller:
// calls are counted, the first one installs the handler, and it stays
// installed until every returned function has been called.
//
// Installing the handler also means the signals it handles no longer end the
// process before the handler has run; a program that handles SIGINT itself
// should call [RestoreAll] from its own handler instead.
func InstallSignalRestore() (uninstall func()) {
	installMu.Lock()
	defer installMu.Unlock()
	if installs == 0 {
		removeHandler = startHandler()
	}
	installs++
	var once sync.Once
	return func() {
		once.Do(func() {
			installMu.Lock()
			defer installMu.Unlock()
			installs--
			if installs == 0 {
				removeHandler()
				removeHandler = nil
			}
		})
	}
}

// startHandler installs the handler and returns the function that removes
// it.
func startHandler() (remove func()) {
	notify := make(chan os.Signal, len(handledSignals)+1)
	signal.Notify(notify, handledSignals...)
	done := make(chan struct{})
	go func() {
		var h handler
		for {
			select {
			case sig := <-notify:
				h.handle(sig)
			case <-h.overdue:
				h.continued()
			case <-done:
				h.finish(notify)
				return
			}
		}
	}()
	return func() {
		signal.Stop(notify)
		close(done)
	}
}

// handler is the state of the installed signal handler. It is used by the
// handler's goroutine alone.
type handler struct {
	// overdue fires when the process is still running, or running again,
	// stopGrace after the handler asked for it to be stopped. It is nil
	// while no stop is under way.
	overdue <-chan time.Time
}

func (h *handler) handle(sig os.Signal) {
	switch {
	case slices.Contains(resumeSignals, sig):
		h.continued()
	case slices.Contains(stopSignals, sig):
		suspendAll()
		if !stopSelf() {
			// The process keeps running, so it does so with visible cursors
			// rather than hiding them again.
			forgetSuspended()
			return
		}
		// The stop signal is on its way and need not have stopped the
		// process by now, so the cursors are not hidden here: that is for
		// the continue signal, or for the time after which no stop is to
		// come.
		h.overdue = time.After(stopGrace)
	default:
		RestoreAll()
		raiseSignal(sig)
	}
}

// continued hides the cursors again that a stop showed: the process was
// continued, or it was never stopped.
func (h *handler) continued() {
	h.overdue = nil
	resumeAll()
}

// finish ends the handler's work when it is uninstalled. Terminating
// signals that arrived before that still end the process, and a stop under
// way is taken as over.
func (h *handler) finish(notify <-chan os.Signal) {
	for {
		select {
		case sig := <-notify:
			if slices.Contains(terminateSignals, sig) {
				h.handle(sig)
			}
		default:
			if h.overdue != nil {
				h.continued()
			}
			return
		}
	}
}
