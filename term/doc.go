// Package term answers the questions every command-line tool asks about its
// terminal: whether a stream is a terminal at all, how large it is, whether
// it may be painted in color, and how wide a piece of text is once it is
// drawn there.
//
// Color is decided once per process. [SetColorMode] records what the user
// asked for (usually through a --color flag), and [Colored] combines that
// with the environment (NO_COLOR, TERM=dumb) and the stream itself. Every
// package in this module asks [Colored] before it writes an escape sequence,
// so output headed for a pipe, a file or a test buffer stays plain.
package term
