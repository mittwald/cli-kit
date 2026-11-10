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
// Every prompt switches its input to raw mode, draws itself in place below
// the cursor, and erases itself before returning, restoring the terminal
// whatever happened. A prompt needs both its input and its output to be a
// terminal; otherwise it returns [ErrNotATerminal] and the caller falls back
// to its non-interactive behavior. Colors come from the active theme (see
// package theme) and are used only where term.Colored allows them.
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
// word, Ctrl-U deletes to the start and Ctrl-K to the end. In a set of
// switches Space or Tab throws the switch under the cursor, "a" turns all on
// and "n" all off.
//
// # Errors
//
// A prompt that is left without an answer returns an error matching
// [ErrAborted]. Two more specific errors tell why:
//
//   - [ErrBack]: the user pressed Esc and wants the previous question again.
//   - [ErrInterrupted]: the user pressed Ctrl-C or Ctrl-D, or the input was
//     closed, and wants out of the whole run.
//
// A caller that does not distinguish the two checks errors.Is(err,
// ErrAborted). A list with nothing to show returns [ErrNoItems].
package pick
