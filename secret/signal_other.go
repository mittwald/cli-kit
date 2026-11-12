//go:build !unix

package secret

// watchSignals does nothing outside Unix: there only the handler of
// package live restores the terminal for a process that is killed at the
// prompt.
func watchSignals(func()) (stop func()) {
	return func() {}
}
