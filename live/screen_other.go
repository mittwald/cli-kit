//go:build !unix && !windows

package live

import "io"

// terminalID reports false: the platform does not tell which terminal a
// writer is open on, so writers are told apart by themselves.
func terminalID(io.Writer) (any, bool) { return nil, false }

// foreground reports true: the platform has no job control.
func foreground(io.Writer) bool { return true }
