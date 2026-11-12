// Package progress draws a single-line progress bar on a terminal.
//
// The bar fills with a color gradient and carries a label: the step being
// worked on. Its label text stays readable on every color of a gradient
// that keeps to dark colors, as the default one does, or to light ones; see
// [Palette]. Between steps the fill keeps creeping toward the next step, so
// a slow step does not look stuck. When an operation inside a step waits
// longer than a moment, typically a network request, the bar counts down
// the time left until that operation's timeout.
//
// A [Bar] draws only when term.CanAnimate reports that its writer may be
// redrawn in place, so the same code runs unchanged with its output piped
// to a file. It is as wide as the terminal and stays so when the window
// changes its width, and it paints 24-bit colors only where the terminal
// announces them, the nearest of the 256 colors otherwise. All methods are
// safe for concurrent use. Log lines written while the bar is shown go
// through [Bar.WithCleared] or [Bar.Writer], which take the bar off the
// line, write, and draw it again below.
//
// The bar hides the cursor while it is shown, through a live.Cursor of its
// own. The cursors of one terminal count together, so a bar that finishes
// does not show a cursor the application around it keeps hidden, and
// live.InstallSignalRestore shows it again when the process is interrupted.
//
// Redrawing after the window was made narrower counts on a terminal that
// wraps its lines again for the new width, as most do. On one that cuts
// its lines off instead (xterm, GNU screen, the Linux console) the bar
// erases the row above it when a window narrower than the bar is drawn in
// for the first time.
package progress
