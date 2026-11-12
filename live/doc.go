// Package live draws terminal output that changes in place, and keeps the
// terminal usable when the program drawing it stops unexpectedly.
//
// [Live] redraws a block of lines in place: each [Live.Draw] replaces the
// previous block instead of adding to the scrollback. [Blocks] sets a
// command's step-by-step detail lines apart from the results it reports.
//
// [Cursor] hides and shows a terminal's cursor, and [InstallSignalRestore]
// makes sure a hidden cursor is shown again when the process is interrupted,
// terminated or suspended. [OnRestore] lets other code that changed the
// terminal, its input mode for example, put it back on the same occasions.
// A cursor is a property of the terminal, not of any one writer, and signal
// handlers are process-wide, so this part of the package keeps process-wide
// state: a registry of hidden cursors, counted per terminal, the functions
// registered with OnRestore, and at most one installed signal handler.
// Everything else is per instance.
//
// Nothing on the way out of the process waits for the terminal: a terminal
// that takes no more output, or a registered function that does not return,
// holds a signal up for a moment and no longer.
//
// None of the types here decide whether drawing is appropriate. Check
// term.CanAnimate before drawing in place: escape sequences that move the
// cursor ruin a log file or a pipe.
//
// Redrawing in place counts the rows a block takes up, and when the window
// of the terminal changes its width, it counts them at the new width. That
// is right for the terminals that wrap their lines again for the new width,
// which most do. On one that cuts its lines off instead (xterm, GNU screen,
// the Linux console), a block with a line wider than the narrowed window is
// taken for taller than it is, and the next draw erases that many rows
// above it. The package cannot tell the two kinds apart.
package live
