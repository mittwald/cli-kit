package pick

import (
	"os"
	"unicode/utf8"

	"github.com/rivo/uniseg"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// InputOptions configures one text prompt. The prompt is a single line with
// the terminal's own cursor in the text, so it reads like any other prompt a
// terminal puts up.
//
// Prompt, Subject and Value are shown as plain single-line text: escape
// sequences and control characters in them are dropped, and a line break or
// a tab becomes a space.
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
	// It is made plain text like the rest, so a value left as it is comes
	// back the way it was shown.
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
//
// The caret moves and deletes by whole characters as they are drawn: a
// letter with its combining marks, or an emoji made of several code points,
// is one step. A value longer than the line scrolls with the caret, an
// ellipsis standing where text is out of sight.
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
	// caret is a position between runes, from 0 to len(value), and always
	// between two grapheme clusters: what is drawn as one character is
	// stepped over and deleted as one.
	caret int
	// first is the rune the drawn part of the value starts at, once the
	// value is longer than the line and has scrolled.
	first  int
	secret bool
	color  bool
	theme  theme.Theme
}

// minValueRoom is how many cells the value is given at least on a line the
// prompt would fill: the prompt is cut before the field is squeezed out.
const minValueRoom = 8

func newEditor(opts InputOptions, color bool) *editor {
	value := []rune(plainLine(opts.Value))
	return &editor{
		prompt:  plainLine(opts.Prompt),
		subject: plainLine(opts.Subject),
		value:   value,
		caret:   len(value),
		secret:  opts.Secret,
		color:   color,
		theme:   theme.Active(),
	}
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
		e.caret = clusterBefore(e.value, e.caret)
	case keyRight:
		e.caret = clusterAfter(e.value, e.caret)
	case keyHome, keyPageUp, keyUp:
		e.caret = 0
	case keyEnd, keyPageDown, keyDown:
		e.caret = len(e.value)
	case keyBackspace:
		start := clusterBefore(e.value, e.caret)
		e.value = deleteRunes(e.value, start, e.caret)
		e.caret = start
	case keyDelete:
		e.value = deleteRunes(e.value, e.caret, clusterAfter(e.value, e.caret))
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
	// An edit can join what was two characters into one, as a joiner typed
	// between two emoji does. The caret then moves behind the whole of it.
	e.caret = clusterEnd(e.value, e.caret)
	return actionNone
}

// clusterBounds are the positions between the grapheme clusters of value,
// counted in runes: 0, the end of every cluster, the last being len(value).
func clusterBounds(value []rune) []int {
	bounds := []int{0}
	graphemes := uniseg.NewGraphemes(string(value))
	for graphemes.Next() {
		bounds = append(bounds, bounds[len(bounds)-1]+len(graphemes.Runes()))
	}
	return bounds
}

// clusterBefore is where the cluster in front of caret starts, 0 at the
// start of the value.
func clusterBefore(value []rune, caret int) int {
	caret = min(caret, len(value))
	if caret <= 0 {
		return 0
	}
	// Whether two clusters part in front of a rune is decided by that rune
	// and those before it, so nothing past the caret has to be read.
	start := certainBound(value, caret-1)
	before := start
	eachBound(value[:caret], start, func(bound int) bool {
		if bound < caret {
			before = bound
		}
		return bound < caret
	})
	return before
}

// clusterAfter is where the cluster behind caret ends, len(value) at the
// end of the value.
func clusterAfter(value []rune, caret int) int {
	after := len(value)
	if caret >= after {
		return after
	}
	eachBound(value, certainBound(value, max(caret, 0)), func(bound int) bool {
		if bound > caret {
			after = bound
		}
		return bound <= caret
	})
	return after
}

// clusterEnd is caret when it stands between two clusters, else the end of
// the cluster it stands in.
func clusterEnd(value []rune, caret int) int {
	end := len(value)
	if caret <= 0 {
		return 0
	}
	if caret >= end {
		return end
	}
	start := certainBound(value, caret)
	if start == caret {
		return caret
	}
	eachBound(value, start, func(bound int) bool {
		if bound >= caret {
			end = bound
		}
		return bound < caret
	})
	return end
}

// certainBound returns a position at or in front of pos where two clusters
// part whatever stands around them: the start of the value, or a place
// between two printable ASCII characters, which no rule of grapheme
// clustering joins. The clusters around a caret are found from there
// instead of from the start of the value, so that an edit costs what its
// surroundings hold and not what the whole value holds: a long value typed
// or pasted rune by rune would otherwise be read once for every rune.
//
// Text without any ASCII in front of pos, such as a long run of Chinese,
// is read from its start.
func certainBound(value []rune, pos int) int {
	for p := min(pos, len(value)-1); p > 0; p-- {
		if printableASCII(value[p-1]) && printableASCII(value[p]) {
			return p
		}
	}
	return 0
}

func printableASCII(r rune) bool { return r >= 0x20 && r <= 0x7e }

// boundWindow is how many runes eachBound reads at first; a cluster is
// seldom longer than a few.
const boundWindow = 32

// eachBound calls yield with the end of every cluster of value behind
// start, in order, until yield returns false. start has to lie between two
// clusters. It reads value in stretches that grow only while a cluster
// reaches their end, since converting all of value for a cluster a few
// runes on would cost as much as reading it all.
func eachBound(value []rune, start int, yield func(bound int) bool) {
	for size := boundWindow; start < len(value); size *= 2 {
		end := min(start+size, len(value))
		rest, state := string(value[start:end]), -1
		for rest != "" {
			var cluster string
			cluster, rest, _, state = uniseg.FirstGraphemeClusterInString(rest, state)
			bound := start + utf8.RuneCountInString(cluster)
			if bound == end && end < len(value) {
				// The cluster may go on past this stretch: read a longer one
				// from where it starts.
				break
			}
			if !yield(bound) {
				return
			}
			start = bound
		}
	}
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
// spells out styled, and the value in the theme's value style.
func (e *editor) render(n int) []string {
	parts, _ := e.layout(n)
	if n <= 0 {
		n = 80
	}
	return []string{drawParts(parts, n, painter{color: e.color, theme: e.theme})}
}

// cursorColumn is where the terminal's cursor belongs, 1-based: on the
// character the caret stands in front of, or behind the last one.
func (e *editor) cursorColumn(n int) int {
	_, column := e.layout(n)
	return column
}

// layout fits the line into n cells and says which column the caret is in,
// 1-based. The value gets what the prompt leaves, less one cell for the
// cursor behind its last character; a prompt that leaves the value less
// than it needs is cut down to make room, to minValueRoom cells at most.
func (e *editor) layout(n int) ([]styledText, int) {
	if n <= 0 {
		n = 80
	}
	label := promptParts(e.prompt, e.subject, e.theme)
	labelWidth := width(partsText(label))
	if e.secret {
		return label, min(labelWidth+1, n)
	}
	need := min(width(string(e.value)), minValueRoom)
	if most := max(n-1-need, 0); labelWidth > most {
		label = cutParts(label, most)
		labelWidth = width(partsText(label))
	}
	shown, before := e.window(n - labelWidth - 1)
	column := min(labelWidth+before+1, n)
	return append(label, styledText{shown, e.theme.Value}), column
}

// window is the stretch of the value drawn in room cells, and how many of
// its cells lie in front of the caret. A value that fits is drawn whole.
// One that does not scrolls with the caret, as far as it has to and no
// further, with an ellipsis at either end that hides text. The ellipsis at
// the end may stand in the cell past room, the one kept for the cursor.
func (e *editor) window(room int) (string, int) {
	bounds := clusterBounds(e.value)
	count := len(bounds) - 1
	widths := make([]int, count)
	caret, first := count, 0
	for i := range count {
		widths[i] = width(string(e.value[bounds[i]:bounds[i+1]]))
		if bounds[i] == e.caret {
			caret = i
		}
		if bounds[i] <= e.first {
			first = i
		}
	}
	span := func(from, to int) int {
		cells := 0
		for _, w := range widths[from:to] {
			cells += w
		}
		return cells
	}
	if span(0, count) <= room {
		e.first = 0
		return string(e.value), span(0, caret)
	}
	if room <= 0 {
		return "", 0
	}
	// The ellipsis in front of a scrolled value takes a cell of its own.
	lead := func(from int) int {
		if from > 0 {
			return 1
		}
		return 0
	}
	first = min(first, caret)
	for first < caret && lead(first)+span(first, caret) > room {
		first++
	}
	// After a deletion the rest may fit from further left again.
	for first > 0 && lead(first-1)+span(first-1, count) <= room {
		first--
	}
	e.first = bounds[first]
	cells, end := lead(first), first
	for end < count && cells+widths[end] <= room {
		cells += widths[end]
		end++
	}
	shown := string(e.value[bounds[first]:bounds[end]])
	if first > 0 {
		shown = term.Ellipsis + shown
	}
	if end < count {
		shown += term.Ellipsis
	}
	return shown, lead(first) + span(first, caret)
}
