//go:build !windows

package term

// plainConsole reports whether v is a console that shows escape sequences
// as text. Only Windows has such consoles.
func plainConsole(any) bool { return false }
