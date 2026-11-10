package pick

import (
	"os"
	"strings"

	"github.com/mittwald/cli-kit/theme"
)

// InputOptions configures one text prompt. The prompt is a single line with
// the terminal's own cursor in the text, so it reads like any other prompt a
// terminal puts up.
type InputOptions struct {
	// Prompt is written in front of the value, with one %s where Subject
	// goes, such as "Name for %s: ". Anything more to say belongs on a line
	// the caller prints before the prompt, which stays on screen when the
	// prompt is erased.
	Prompt string
	// Subject is the thing the prompt is about, drawn in the theme's subject
	// style inside the prompt.
	Subject string
	// Value is what the field starts out holding, with the cursor at its end.
	Value string
	// Secret draws nothing for what is typed, the way a password prompt does.
	Secret bool
}

// Input draws a one-line text field and returns what was typed. The field
// supports the usual line-editing keys: the arrows, Home and End (or Ctrl-A
// and Ctrl-E), Backspace and Delete, Ctrl-W to delete a word, Ctrl-U to
// delete to the start, and Ctrl-K to delete to the end. Enter answers, Esc
// returns ErrBack, and Ctrl-C or Ctrl-D returns ErrInterrupted. A cleared
// field answers with the empty string.
func Input(in, out *os.File, opts InputOptions) (string, error) {
	session, err := openTerminal(in, out, false)
	if err != nil {
		return "", err
	}
	defer session.close()

	state := newEditor(opts, session.color)
	state.theme = session.theme
	if err := session.drive(state, 0); err != nil {
		return "", err
	}
	return state.answer(), nil
}

// editor is the state of the one-line text field. Like the list it is pure,
// so the editing is testable without a terminal.
type editor struct {
	prompt  string
	subject string
	value   []rune
	// caret is a position between runes, from 0 to len(value).
	caret  int
	secret bool
	color  bool
	theme  theme.Theme
}

func newEditor(opts InputOptions, color bool) *editor {
	value := []rune(opts.Value)
	return &editor{
		prompt:  opts.Prompt,
		subject: opts.Subject,
		value:   value,
		caret:   len(value),
		secret:  opts.Secret,
		color:   color,
		theme:   theme.Active(),
	}
}

// label is the prompt as drawn, subject filled in.
func (e *editor) label() string {
	return strings.ReplaceAll(e.prompt, "%s", e.subject)
}

// answer is what was typed.
func (e *editor) answer() string { return string(e.value) }

// resize is what makes an editor a screenState; one line has nothing to
// refit.
func (e *editor) resize(int, int) {}

func (e *editor) apply(k key) action {
	switch k.kind {
	case keyEnter:
		return actionSelect
	case keyBack:
		return actionBack
	case keyInterrupt:
		return actionInterrupt
	case keyLeft:
		if e.caret > 0 {
			e.caret--
		}
	case keyRight:
		if e.caret < len(e.value) {
			e.caret++
		}
	case keyHome, keyPageUp, keyUp:
		e.caret = 0
	case keyEnd, keyPageDown, keyDown:
		e.caret = len(e.value)
	case keyBackspace:
		if e.caret > 0 {
			e.value = deleteRunes(e.value, e.caret-1, e.caret)
			e.caret--
		}
	case keyDelete:
		if e.caret < len(e.value) {
			e.value = deleteRunes(e.value, e.caret, e.caret+1)
		}
	case keyDeleteWord:
		start := wordStart(e.value, e.caret)
		e.value = deleteRunes(e.value, start, e.caret)
		e.caret = start
	case keyKillToEnd:
		e.value = e.value[:e.caret]
	case keyClearFilter:
		// Ctrl-U deletes to the start, as in a line editor; from the end of
		// the value, where the field opens, that is the whole field.
		e.value = e.value[e.caret:]
		e.caret = 0
	case keyRune:
		e.value = append(e.value[:e.caret], append([]rune{k.rune}, e.value[e.caret:]...)...)
		e.caret++
	}
	return actionNone
}

// wordStart is where the word ending at caret begins: the separators in
// front of it, then the characters before those.
func wordStart(value []rune, caret int) int {
	start := caret
	for start > 0 && isWordSeparator(value[start-1]) {
		start--
	}
	for start > 0 && !isWordSeparator(value[start-1]) {
		start--
	}
	return start
}

// isWordSeparator is whitespace and nothing else, the way a shell's Ctrl-W
// works: punctuation inside a value such as "v1.2.3-rc1" holds it together.
func isWordSeparator(r rune) bool {
	return r == ' ' || r == '\t'
}

// deleteRunes cuts [from, to) out of value into a fresh slice, so one edit
// never shows up in a slice another still holds.
func deleteRunes(value []rune, from, to int) []rune {
	return append(append([]rune{}, value[:from]...), value[to:]...)
}

// render is the one line: the prompt, with its subject and the answers it
// spells out styled, and the value in the theme's value style. It is cut to
// the terminal in reading order.
func (e *editor) render(width int) []string {
	if width <= 0 {
		width = 80
	}
	parts := promptParts(e.prompt, e.subject, e.theme)
	if !e.secret {
		parts = append(parts, styledText{string(e.value), e.theme.Value})
	}
	return []string{drawParts(parts, width, painter{color: e.color, theme: e.theme})}
}

// cursorColumn is where the terminal's cursor belongs, 1-based: in front of
// the rune the caret stands on, or in the last column when the value is
// longer than the line.
func (e *editor) cursorColumn(n int) int {
	column := width(e.label()) + 1
	if !e.secret {
		column += width(string(e.value[:e.caret]))
	}
	if n > 0 && column > n {
		return n
	}
	return column
}
