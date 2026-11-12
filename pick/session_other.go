//go:build !unix

package pick

// wakeable says a blocked read cannot be woken here: there is no portable
// way to wait on a console and on something else at once. A view then loads
// its rows before the next key is read, and cannot be left while it does.
const wakeable = false

// watchSignals does nothing here: the signals a prompt puts the terminal
// back on are POSIX signals. A process ended from outside while a prompt is
// up leaves the console in the mode the prompt set.
func watchSignals(_, _ func()) (stop func()) { return func() {} }

// hungUp reports whether err is what reading a terminal that went away
// returns, which no error is known to mean here.
func hungUp(error) bool { return false }
