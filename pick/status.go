package pick

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// Status writes text as a line of its own (or several, where it has newlines)
// and returns a function that erases it again. Use it for what is only of
// interest while it lasts, such as "Fetching releases...". On a terminal the
// text is drawn in the theme's Status style and the returned function takes
// it back off the screen; anywhere else the line stays, since a log is read
// after the fact.
//
// The text is written as plain text: escape sequences and control characters
// other than the newline are dropped and tabs become spaces, as term.Plain
// does it, so a name from elsewhere inside it cannot do more than be read.
//
// The returned function erases by counting screen rows up from the cursor,
// so it must be called before anything else is written. It erases once:
// calling it again, as a deferred call after an explicit one does, leaves
// what was written since alone.
func Status(w io.Writer, text string) func() {
	line := term.Plain(text)
	if term.Colored(w) {
		line = theme.Active().Status.PaintLines(line)
	}
	_, _ = fmt.Fprintln(w, line)
	var once sync.Once
	return func() {
		once.Do(func() {
			if term.IsTerminal(w) {
				cols, _ := term.Size(w)
				clearLines(w, term.ScreenRows(strings.Split(line, "\n"), cols))
			}
		})
	}
}

// clearLines moves up over n lines and erases everything from there down.
func clearLines(w io.Writer, n int) {
	if n <= 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "\r\x1b[%dA\x1b[J", n)
}

// Name renders a value the way a list draws a row's name: in the subject
// style, with whatever it says in parentheses receding. Use it for lines
// written beside the prompts, so a value reads the same whether it is being
// chosen or reported. Like a row's name, the value is made plain
// single-line text first: escape sequences and control characters in it are
// dropped, and a line break or a tab becomes a space. Where w takes no color
// that plain text is what Name returns.
func Name(w io.Writer, text string) string {
	text = plainLine(text)
	if !term.Colored(w) {
		return text
	}
	return name(text, theme.Active())
}

func name(text string, th theme.Theme) string {
	var b strings.Builder
	for _, part := range parenSpans(text) {
		style := th.Subject
		if part.paren {
			style = th.Qualifier
		}
		b.WriteString(painter{color: true, theme: th}.paint(style, part.text))
	}
	return b.String()
}

// Prompt renders a question the way Input draws it: the subject styled where
// the question holds %s for it, and the answers it spells out, such as
// [y/N], receding. Use it for the line that keeps an answered prompt on
// screen once the field has erased itself. Like Input, it fills the subject
// in for the first %s only and makes both plain single-line text. Where w
// takes no color it returns the question with the subject filled in.
func Prompt(w io.Writer, prompt, subject string) string {
	th := theme.Active()
	parts := promptParts(plainLine(prompt), plainLine(subject), th)
	if !term.Colored(w) {
		return partsText(parts)
	}
	p := painter{color: true, theme: th}
	var b strings.Builder
	for _, part := range parts {
		b.WriteString(p.paint(part.style, part.text))
	}
	return b.String()
}
