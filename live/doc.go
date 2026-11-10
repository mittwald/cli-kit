// Package live draws terminal output that changes in place, and keeps the
// terminal usable when the program drawing it stops unexpectedly.
//
// [Live] redraws a block of lines in place: each [Live.Draw] replaces the
// previous block instead of adding to the scrollback. [Blocks] sets a
// command's step-by-step detail lines apart from the results it reports.
//
// [Cursor] hides and shows a terminal's cursor, and [InstallSignalRestore]
// makes sure a hidden cursor is shown again when the process is interrupted,
// terminated or suspended. A cursor is a property of the terminal, not of
// any one writer, and signal handlers are process-wide, so this part of the
// package keeps process-wide state: a registry of hidden cursors and at most
// one installed signal handler. Everything else is per instance.
//
// None of the types here decide whether drawing is appropriate. Check
// term.CanAnimate before drawing in place: escape sequences that move the
// cursor ruin a log file or a pipe.
package live
