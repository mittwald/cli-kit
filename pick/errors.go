package pick

import (
	"errors"
	"fmt"
)

// ErrAborted reports that a prompt was left without an answer. Every way out
// of a prompt other than answering it matches ErrAborted, so a caller that
// only needs to know "nothing was chosen" checks for it alone.
var ErrAborted = errors.New("prompt aborted")

// ErrBack reports that a prompt was left with Esc, or with Backspace on an
// empty filter where Options.DismissOnBackspace allows it: the user wants to
// return to the question asked before this one. It matches [ErrAborted].
var ErrBack = fmt.Errorf("%w: went back", ErrAborted)

// ErrInterrupted reports that a prompt was left with Ctrl-C or Ctrl-D, or that
// there is nobody left to answer it: its input ended or was closed, the
// terminal went away, or the prompt was taken off the screen for the process
// to end (by a signal, or by live.RestoreAll) and the application lived on.
// The user wants out of the whole run, not just this question. It matches
// [ErrAborted].
var ErrInterrupted = fmt.Errorf("%w: interrupted", ErrAborted)

// ErrNoItems reports that there was nothing to choose from. The caller knows
// what an empty list means in its context and says so itself.
var ErrNoItems = errors.New("pick: nothing to select")

// ErrNotATerminal reports that the input or the output of a prompt is not a
// terminal, so nothing can be drawn. Callers fall back to their
// non-interactive behavior, such as requiring a flag.
var ErrNotATerminal = errors.New("pick: not a terminal")

// errScopedChoice reports a combination the list cannot answer: views that
// hold their own rows change what a row index means, so they cannot be
// combined with typed answers or with marking several rows.
var errScopedChoice = errors.New("pick: views with their own items cannot be combined with typed answers or multiple selection")
