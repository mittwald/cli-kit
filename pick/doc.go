// Package pick draws keyboard-driven prompts on a terminal: the interactive
// counterpart of a flag the user did not pass.
//
// There are three shapes of prompt:
//
//   - A list ([Select], [SelectOrType], [SelectManyOrType], [SelectRange],
//     [SelectView]) picks one row, several rows or a range of rows from many.
//     Rows are aligned columns; typing filters them, views narrow them, and
//     rows can carry an action, a flag, details and their own colors.
//   - A text field ([Input]) takes a value, optionally without echoing it.
//   - Switches ([Toggles], [Checklist]) set several yes/no values at once.
//
// Every prompt switches its input to raw mode, draws itself in place from
// the line the cursor is on, and erases itself before returning, restoring
// the terminal however it ends. A prompt needs both its input and its output
// to be a terminal; otherwise it returns [ErrNotATerminal] and the caller
// falls back to its non-interactive behavior. Colors come from the active
// theme (see package theme) and are used only where term.Colored allows
// them.
//
// # Text
//
// Everything a prompt is given to draw (titles, notes, rows, labels, the
// value a text field starts with) is shown as plain text on one line: escape
// sequences and control characters are dropped, a line break or a tab
// becomes a space, and the characters that reorder text on screen go. Text
// from elsewhere, such as the title of a pull request or the name of a
// branch, can be handed in as it is. Colors are set with styles
// (Options.CellStyles, Options.PaintCell, the theme), not inside the text.
// The same holds for what is typed: a control character pasted into a
// filter or a text field is left out.
//
// # The terminal
//
// A prompt takes the line the cursor is on: what was written on that line
// before is erased with the prompt, so end what you print before a prompt
// with a newline. The block is as tall as the terminal lets it be. On a
// terminal too short for all of it, the lines around the rows go first (the
// note, the blank lines, the key line, the position line) and the title
// last, so the rows stay.
//
// The terminal is put back as it was found when the prompt is answered or
// left, when the program panics, and, on Unix, when one of the signals that
// end a process arrives: SIGINT, SIGTERM, SIGHUP or SIGQUIT. The prompt
// erases itself, shows the cursor and restores the terminal mode, and then
// raises the signal again, so it does what it would have done without a
// prompt on screen. A program that handles such a signal itself keeps its
// handler, and the prompt returns [ErrInterrupted] to it; its handler sees
// the signal twice, once as delivered and once as raised again, since
// nothing tells a prompt that somebody else is listening. A signal the
// process ignores is left alone. SIGKILL cannot be caught: a process ended
// that way leaves the terminal in raw mode.
//
// A prompt also registers with package live for as long as it is up, so a
// program that uses live to leave its terminal in order has its prompts
// covered: the signal handler of live.InstallSignalRestore, which ends the
// process itself, puts the prompt's terminal back first, and so do
// live.RestoreAll and a panic passed through live's Cursor.RestoreOnPanic.
// On Windows, where a prompt watches no signals of its own, that handler is
// what puts the terminal back. A prompt put away like that is not drawn
// again. Should the process still be there a moment later, as after a call
// of live.RestoreAll that was not followed by an exit, the prompt returns
// [ErrInterrupted].
//
// Keys typed ahead are not lost between two prompts: what one prompt read
// past its answer, such as an answer pasted for the question after it, goes
// to the next prompt that reads from the same input. Ctrl-C drops it.
//
// A prompt redraws itself by moving the cursor back over the rows it drew,
// counted at the width the terminal has by then. That is right for a
// terminal that wraps its lines again when it is made narrower, as nearly
// all do. One that cuts them off instead (GNU screen, the Linux console)
// has fewer rows on screen than counted, and making it narrower while a
// prompt is up erases lines above the prompt on the next key.
//
// # Keys
//
// In a list:
//
//	↑ ↓, Ctrl-P Ctrl-N    move the cursor
//	PgUp PgDn             move a page
//	Home End, Ctrl-A Ctrl-E
//	                      move to the first or last row
//	typing                filters the rows; every word typed has to match
//	Backspace, Ctrl-U     delete from the filter, or clear it
//	Tab Shift-Tab, → ←    switch to the next or previous view
//	Space                 mark a row, in a list that takes several
//	Enter                 choose the row, or anchor and end a range
//	Esc                   go back (an open range is dropped first)
//	Ctrl-C, Ctrl-D        interrupt
//
// In a text field the arrows, Home and End move the caret, Ctrl-W deletes a
// word, Ctrl-U deletes to the start and Ctrl-K to the end. The caret steps
// over a character as it is drawn, however many code points it is made of,
// and a value longer than the line scrolls with it. In a set of switches
// Space or Tab throws the switch under the cursor, "a" turns all on and "n"
// all off.
//
// Alt with an arrow, or with another key that sends a sequence of its own,
// acts as that key alone; Alt with a character is ignored.
//
// # Errors
//
// A prompt that is left without an answer returns an error matching
// [ErrAborted]. Two more specific errors tell why:
//
//   - [ErrBack]: the user pressed Esc and wants the previous question again.
//   - [ErrInterrupted]: the user pressed Ctrl-C or Ctrl-D, or nobody is left
//     to answer (the input ended or was closed, the terminal went away, or a
//     signal ended the prompt), and the whole run is over.
//
// A caller that does not distinguish the two checks errors.Is(err,
// ErrAborted). A list with nothing to show returns [ErrNoItems].
package pick
