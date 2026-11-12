//go:build windows

package term

import (
	"sync"

	"golang.org/x/sys/windows"
)

// vtConsoles remembers, for every console handle asked about, whether its
// virtual terminal processing is on, so that the console mode is read and
// set once per handle.
var vtConsoles sync.Map // uintptr to bool

// plainConsole reports whether v is, or wraps, a console that shows escape
// sequences as text. It first tries to make the console read them, by
// switching on its virtual terminal processing, which Windows leaves off for
// a console unless the program before left it on.
func plainConsole(v any) bool {
	plain := false
	descriptor(v, func(fd uintptr) { plain = isTerminal(fd) && !enableVT(fd) })
	return plain
}

// enableVT switches on virtual terminal processing for the console screen
// buffer fd is a handle of, and reports whether it is on. A handle that is
// no screen buffer, such as the console's input, reports false and is left
// alone: on an input handle the same bit means something else.
func enableVT(fd uintptr) bool {
	if known, ok := vtConsoles.Load(fd); ok {
		on, _ := known.(bool)
		return on
	}
	handle := windows.Handle(fd)
	var (
		info windows.ConsoleScreenBufferInfo
		mode uint32
	)
	on := windows.GetConsoleScreenBufferInfo(handle, &info) == nil && windows.GetConsoleMode(handle, &mode) == nil
	if on && mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING == 0 {
		on = windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
	}
	vtConsoles.Store(fd, on)
	return on
}
