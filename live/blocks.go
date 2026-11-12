package live

import (
	"io"
	"strings"
	"sync"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// Blocks sets a command's progress lines apart from the results it reports.
// Detail lines are written as a block of their own, with a blank line before
// and after it, and painted in the theme's Status style on a terminal that
// takes color. A detail line that should stand out, a warning by default, is
// left unpainted. Plain lines are everything else; the first plain line after
// a block closes it.
//
// Both writers expect whole lines per write, the way fmt.Fprintln writes
// them. A Blocks is safe for concurrent use.
type Blocks struct {
	mu        sync.Mutex
	w         io.Writer
	color     bool
	style     theme.Style
	highlight func(text string) bool
	open      bool
	// spaced says a blank line written past Blocks stands above whatever is
	// written next, so a block opening there does not add its own.
	spaced bool
}

// BlocksOption configures a [Blocks].
type BlocksOption func(*Blocks)

// WithDetailStyle paints detail lines in style instead of the active theme's
// Status style.
func WithDetailStyle(style theme.Style) BlocksOption {
	return func(b *Blocks) { b.style = style }
}

// WithHighlight decides which detail lines are left unpainted so they stand
// out from the rest of the block. The default leaves lines containing
// "WARNING" or "ERROR" unpainted.
func WithHighlight(highlight func(text string) bool) BlocksOption {
	return func(b *Blocks) { b.highlight = highlight }
}

// NewBlocks returns a Blocks writing to w. Detail lines are painted when
// term.Colored(w) reports true.
func NewBlocks(w io.Writer, options ...BlocksOption) *Blocks {
	b := &Blocks{
		w:         w,
		color:     term.Colored(w),
		style:     theme.Active().Status,
		highlight: defaultHighlight,
	}
	for _, option := range options {
		option(b)
	}
	return b
}

func defaultHighlight(text string) bool {
	return strings.Contains(text, "WARNING") || strings.Contains(text, "ERROR")
}

// Detail returns the writer for progress lines. The writer also has Close
// and Spaced methods that act on b, for callers that hold only the writer.
func (b *Blocks) Detail() io.Writer { return detailWriter{b} }

// Plain returns the writer for everything else written to the same place.
func (b *Blocks) Plain() io.Writer { return plainWriter{b} }

// Close ends an open block. Call it before output that Blocks does not see,
// such as a line written to the other stream of the same terminal. Closing a
// closed block writes nothing.
func (b *Blocks) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closeLocked()
}

// Spaced tells Blocks that a blank line was written past it, for example
// after an answered prompt, and that output continues below that line. The
// blank line ends an open block, and a block opening next does not add a
// second one.
func (b *Blocks) Spaced() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.open = false
	b.spaced = true
}

func (b *Blocks) closeLocked() {
	if b.open {
		_, _ = io.WriteString(b.w, "\n")
		b.open = false
	}
}

type detailWriter struct{ b *Blocks }

// Unwrap returns the writer the block is written to, so term.IsTerminal and
// term.Size see the terminal behind it.
func (d detailWriter) Unwrap() io.Writer { return d.b.w }

// Close ends the open block, see [Blocks.Close].
func (d detailWriter) Close() { d.b.Close() }

// Spaced records a blank line written past the block, see [Blocks.Spaced].
func (d detailWriter) Spaced() { d.b.Spaced() }

func (d detailWriter) Write(p []byte) (int, error) {
	b := d.b
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.open {
		if !b.spaced {
			if _, err := io.WriteString(b.w, "\n"); err != nil {
				return 0, err
			}
		}
		b.open = true
		b.spaced = false
	}
	text := string(p)
	if b.color && b.style != "" && (b.highlight == nil || !b.highlight(text)) {
		body, newline := strings.CutSuffix(text, "\n")
		text = b.style.PaintLines(body)
		if newline {
			text += "\n"
		}
	}
	if _, err := io.WriteString(b.w, text); err != nil {
		return 0, err
	}
	return len(p), nil
}

type plainWriter struct{ b *Blocks }

// Unwrap returns the writer the plain lines are written to.
func (w plainWriter) Unwrap() io.Writer { return w.b.w }

func (w plainWriter) Write(p []byte) (int, error) {
	b := w.b
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closeLocked()
	b.spaced = false
	return b.w.Write(p)
}
