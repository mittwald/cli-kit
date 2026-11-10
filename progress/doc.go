// Package progress draws a single-line progress bar on a terminal.
//
// The bar fills with a color gradient and carries a label: the step being
// worked on. Its label text stays readable on every color the gradient
// passes through. Between steps the fill keeps creeping toward the next step,
// so a slow step does not look stuck. When an operation inside a step waits
// longer than a moment, typically a network request, the bar counts down
// the time left until that operation's timeout.
//
// A [Bar] draws only when term.CanAnimate reports that its writer may be
// redrawn in place, so the same code runs unchanged with its output piped
// to a file. All methods are safe for concurrent use. Log lines written
// while the bar is shown go through [Bar.WithCleared] or [Bar.Writer], which
// take the bar off the line, write, and draw it again below.
package progress
