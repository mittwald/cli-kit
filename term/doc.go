// Package term answers the questions every command-line tool asks about its
// terminal: whether a stream is a terminal at all, how large it is, whether
// it may be painted in color, and how wide a piece of text is once it is
// drawn there.
//
// Color is decided once per process. [SetColorMode] records what the user
// asked for (usually through a --color flag), and [Colored] combines that
// with the environment (NO_COLOR, TERM=dumb) and the stream itself. Every
// package in this module asks [Colored] before it writes an escape sequence,
// so output headed for a pipe, a file or a test buffer stays plain. On
// Windows, Colored and [CanAnimate] switch on the console's virtual terminal
// processing before they report true, and report false for a console that
// would show escape sequences as text.
//
// Widths are counted in terminal cells and are the same on every machine:
// [VisibleWidth], [WidthAt], [Truncate], [Wrap] and [ScreenRows] count a wide
// rune as two cells, a tab as the cells up to the next of the tab stops every
// 8 columns, and a character of ambiguous width, such as a box-drawing line,
// as one cell whatever the locale.
//
// Text from elsewhere — a CI job's log, a remote command's output — goes
// through [Sanitize] or [SanitizeLines] before it is written: they keep its
// colors, 24-bit ones as the nearest of the 256 colors, and take out every
// escape sequence and control character that could do more than color it.
// Text that is shown plain, without the sender's colors, goes through [Plain]
// instead, and a value shown within one line, such as a name or a header,
// through [PlainLine]. A URL that becomes a link with [Hyperlink] must hold
// no control character, or it is no link.
package term
