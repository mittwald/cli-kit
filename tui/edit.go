package tui

import (
	"strings"
	"unicode"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// clusters splits s into grapheme clusters, leaving out what cannot be
// shown and keeping line breaks out, for a line being edited.
func clusters(s string) []string {
	var out []string
	for _, g := range graphemes(s) {
		out = append(out, g.text)
	}
	return out
}

// clusterWidth is how many cells a cluster takes up.
func clusterWidth(c string) int {
	if c == "" {
		return 0
	}
	return term.VisibleWidth(c)
}

// space reports whether a cluster separates words.
func space(c string) bool {
	for _, r := range c {
		return unicode.IsSpace(r) || unicode.IsPunct(r)
	}
	return true
}

// wordLeft is the start of the word before caret.
func wordLeft(cl []string, caret int) int {
	i := caret
	for i > 0 && space(cl[i-1]) {
		i--
	}
	for i > 0 && !space(cl[i-1]) {
		i--
	}
	return i
}

// wordRight is the end of the word after caret.
func wordRight(cl []string, caret int) int {
	i := caret
	for i < len(cl) && space(cl[i]) {
		i++
	}
	for i < len(cl) && !space(cl[i]) {
		i++
	}
	return i
}

// editState is a snapshot for undo.
type editState struct {
	lines [][]string
	row   int
	col   int
}

// editor edits text as lines of grapheme clusters. A one-line editor
// joins what is pasted or inserted into its line; a multi-line one splits
// it into lines.
type editor struct {
	lines [][]string
	row   int
	col   int
	multi bool
	// limit is the most characters the text may hold, 0 for no limit.
	limit int
	// want is the column, in cells, up and down keep to.
	want      int
	undo      []editState
	redo      []editState
	lastGroup string
}

func newEditor(multi bool) *editor { return &editor{lines: [][]string{nil}, multi: multi} }

// String is the text, lines joined by "\n".
func (e *editor) String() string {
	parts := make([]string, len(e.lines))
	for i, l := range e.lines {
		parts[i] = strings.Join(l, "")
	}
	return strings.Join(parts, "\n")
}

// length is how many characters the text holds, line breaks included.
func (e *editor) length() int {
	n := len(e.lines) - 1
	for _, l := range e.lines {
		for _, c := range l {
			n += len([]rune(c))
		}
	}
	return n
}

// set replaces the text and puts the caret at its end.
func (e *editor) set(s string) {
	e.lines = [][]string{nil}
	e.row, e.col = 0, 0
	e.insertText(s)
	e.undo, e.redo, e.lastGroup = nil, nil, ""
}

func (e *editor) snapshot() editState {
	lines := make([][]string, len(e.lines))
	for i, l := range e.lines {
		lines[i] = append([]string(nil), l...)
	}
	return editState{lines: lines, row: e.row, col: e.col}
}

// remember keeps the state before an edit for undo. Edits of one group in
// a row, such as characters typed one after the other, undo together.
func (e *editor) remember(group string) {
	if group != "" && group == e.lastGroup {
		return
	}
	e.lastGroup = group
	e.undo = append(e.undo, e.snapshot())
	if len(e.undo) > 200 {
		e.undo = e.undo[1:]
	}
	e.redo = nil
}

func (e *editor) restore(s editState) {
	e.lines, e.row, e.col = s.lines, s.row, s.col
}

func (e *editor) undoEdit() bool {
	if len(e.undo) == 0 {
		return false
	}
	e.redo = append(e.redo, e.snapshot())
	e.restore(e.undo[len(e.undo)-1])
	e.undo = e.undo[:len(e.undo)-1]
	e.lastGroup = ""
	return true
}

func (e *editor) redoEdit() bool {
	if len(e.redo) == 0 {
		return false
	}
	e.undo = append(e.undo, e.snapshot())
	e.restore(e.redo[len(e.redo)-1])
	e.redo = e.redo[:len(e.redo)-1]
	e.lastGroup = ""
	return true
}

// insertText inserts s at the caret, up to the limit. A one-line editor
// takes line breaks as spaces.
func (e *editor) insertText(s string) bool {
	if !e.multi {
		s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
	}
	if e.limit > 0 {
		room := e.limit - e.length()
		if room <= 0 {
			return false
		}
		if r := []rune(s); len(r) > room {
			s = string(r[:room])
		}
	}
	parts := strings.Split(s, "\n")
	line := e.lines[e.row]
	before := strings.Join(line[:e.col], "")
	after := strings.Join(line[e.col:], "")
	if len(parts) == 1 {
		e.lines[e.row], e.col = joinAt(before+parts[0], after)
		return true
	}
	var lines [][]string
	lines = append(lines, clusters(before+parts[0]))
	for _, p := range parts[1 : len(parts)-1] {
		lines = append(lines, clusters(p))
	}
	last := parts[len(parts)-1]
	lastLine, lastCol := joinAt(last, after)
	lines = append(lines, lastLine)
	rest := append([][]string{}, e.lines[e.row+1:]...)
	e.lines = append(append(e.lines[:e.row], lines...), rest...)
	e.row += len(parts) - 1
	e.col = lastCol
	return true
}

// deleteRange removes the clusters of the caret's line from..to.
func (e *editor) deleteRange(from, to int) {
	line := e.lines[e.row]
	e.lines[e.row], e.col = joinAt(strings.Join(line[:from], ""), strings.Join(line[to:], ""))
}

// joinAt splits left+right into grapheme clusters anew, since characters
// that meet can make one, such as two halves of a flag, and returns them
// with the caret where left ends: after the cluster it ends in, where that
// cluster now spans the join.
func joinAt(left, right string) ([]string, int) {
	cl := clusters(left + right)
	n := 0
	for i, c := range cl {
		if n >= len(left) {
			return cl, i
		}
		n += len(c)
	}
	return cl, len(cl)
}

// joinUp joins the caret's line to the one above.
func (e *editor) joinUp() {
	if e.row == 0 {
		return
	}
	e.lines[e.row-1], e.col = joinAt(strings.Join(e.lines[e.row-1], ""), strings.Join(e.lines[e.row], ""))
	e.lines = append(e.lines[:e.row], e.lines[e.row+1:]...)
	e.row--
}

// handle applies a key that edits text or moves the caret within a line,
// and reports whether it took the key and whether the text changed. Up,
// Down and the keys that move by page are left to the editor's widget.
func (e *editor) handle(k Key) (took, changed bool) {
	line := e.lines[e.row]
	switch k.String() {
	case "left", "ctrl+b":
		switch {
		case e.col > 0:
			e.col--
		case e.multi && e.row > 0:
			e.row--
			e.col = len(e.lines[e.row])
		}
	case "right", "ctrl+f":
		switch {
		case e.col < len(line):
			e.col++
		case e.multi && e.row < len(e.lines)-1:
			e.row++
			e.col = 0
		}
	case "alt+left", "ctrl+left", "alt+b":
		e.col = wordLeft(line, e.col)
	case "alt+right", "ctrl+right", "alt+f":
		e.col = wordRight(line, e.col)
	case "home", "ctrl+a":
		e.col = 0
	case "end", "ctrl+e":
		e.col = len(line)
	case "backspace", "ctrl+h":
		switch {
		case e.col > 0:
			e.remember("delete")
			e.deleteRange(e.col-1, e.col)
		case e.multi && e.row > 0:
			e.remember("")
			e.joinUp()
		default:
			return true, false
		}
		return true, true
	case "delete", "ctrl+d":
		switch {
		case e.col < len(line):
			e.remember("delete")
			e.deleteRange(e.col, e.col+1)
		case e.multi && e.row < len(e.lines)-1:
			e.remember("")
			e.row++
			e.joinUp()
		default:
			return true, false
		}
		return true, true
	case "ctrl+w", "alt+backspace":
		if e.col == 0 {
			return true, false
		}
		e.remember("")
		e.deleteRange(wordLeft(line, e.col), e.col)
		return true, true
	case "alt+d":
		if e.col == len(line) {
			return true, false
		}
		e.remember("")
		e.deleteRange(e.col, wordRight(line, e.col))
		return true, true
	case "ctrl+u":
		if e.col == 0 {
			return true, false
		}
		e.remember("")
		e.deleteRange(0, e.col)
		return true, true
	case "ctrl+k":
		if e.col == len(line) {
			return true, false
		}
		e.remember("")
		e.deleteRange(e.col, len(line))
		return true, true
	case "ctrl+z":
		return true, e.undoEdit()
	case "ctrl+y", "ctrl+shift+z":
		return true, e.redoEdit()
	default:
		if !k.Printable() {
			return false, false
		}
		e.remember("type")
		return true, e.insertText(string(k.Rune))
	}
	e.lastGroup = ""
	e.want = -1
	return true, false
}

// drawLine draws the line of a one-line editor at x, y of c, scrolled so
// the caret is in view, and puts the terminal's cursor at the caret when
// focused. first is the first cluster shown, kept between frames.
func drawLine(c Canvas, x, y int, ed *editor, first *int, style theme.Style, focused bool) {
	line := ed.lines[0]
	room := max(c.Width()-x-1, 1)
	*first = min(*first, ed.col, len(line))
	for dist := colWidth(line[*first:], ed.col-*first); dist > room; *first++ {
		dist -= clusterWidth(line[*first])
	}
	used := x
	for _, cl := range line[*first:] {
		if used+clusterWidth(cl) > c.Width() {
			break
		}
		used += c.Text(used, y, cl, style)
	}
	if focused {
		c.ShowCursor(x+colWidth(line[*first:], ed.col-*first), y)
	}
}

// colWidth is how many cells the clusters of a line before col take up.
func colWidth(line []string, col int) int {
	w := 0
	for _, c := range line[:col] {
		w += clusterWidth(c)
	}
	return w
}

// colAt is the cluster index of a line at cells cells from its start.
func colAt(line []string, cells int) int {
	w := 0
	for i, c := range line {
		cw := clusterWidth(c)
		if w+cw > cells {
			return i
		}
		w += cw
	}
	return len(line)
}
