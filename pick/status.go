package pick

import (
	"fmt"
	"io"
	"strings"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// Status writes text as a line of its own (or several, where it has newlines)
// and returns a function that erases it again. Use it for what is only of
// interest while it lasts, such as "Fetching releases...". On a terminal the
// text is muted and the returned function takes it back off the screen;
// anywhere else the line stays, since a log is read after the fact.
//
// The returned function erases by counting screen rows up from the cursor,
// so it must be called before anything else is written.
func Status(w io.Writer, text string) func() {
	line := text
	if term.Colored(w) {
		line = theme.Active().Muted.PaintLines(text)
	}
	_, _ = fmt.Fprintln(w, line)
	return func() {
		if term.IsTerminal(w) {
			cols, _ := term.Size(w)
			clearLines(w, term.ScreenRows(strings.Split(line, "\n"), cols))
		}
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
// chosen or reported. Where w takes no color it returns text unchanged.
func Name(w io.Writer, text string) string {
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
// screen once the field has erased itself. Where w takes no color it returns
// the question with the subject filled in.
func Prompt(w io.Writer, prompt, subject string) string {
	if !term.Colored(w) {
		return strings.ReplaceAll(prompt, "%s", subject)
	}
	th := theme.Active()
	p := painter{color: true, theme: th}
	var b strings.Builder
	for _, part := range promptParts(prompt, subject, th) {
		b.WriteString(p.paint(part.style, part.text))
	}
	return b.String()
}
