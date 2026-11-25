package tui

import (
	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// TextArea is a multi-line text field, such as the body of a message.
// Lines wider than the field wrap at spaces. It takes the keys of [Input]
// and these:
//
//	enter                 new line
//	↑/↓                   line up/down, as wrapped
//	pgup/pgdown           page up/down
//	ctrl+home/ctrl+end    start/end of the text
//
// Tab is left to move the focus, so a TextArea in a form behaves like the
// other fields.
type TextArea struct {
	ed          *editor
	placeholder string
	disabled    bool
	top         int
	height      int
	width       int
	keymap      *Keymap
	// scrolled says the wheel moved the view away from the cursor; the
	// view follows the cursor again at the next key.
	scrolled bool

	// OnChange is called with the text after every change.
	OnChange func(text string)
}

// NewTextArea returns an empty text area.
func NewTextArea() *TextArea {
	t := &TextArea{ed: newEditor(true), keymap: NewKeymap()}
	t.ed.want = -1
	return t
}

// Keymap returns the keys of the field.
func (t *TextArea) Keymap() *Keymap { return t.keymap }

// Value returns the text.
func (t *TextArea) Value() string { return t.ed.String() }

// SetValue replaces the text and puts the cursor at its end. OnChange is
// not called.
func (t *TextArea) SetValue(s string) {
	t.ed.set(term.Plain(s))
	t.top = 0
}

// SetPlaceholder sets the text shown, muted, while the field is empty.
func (t *TextArea) SetPlaceholder(s string) *TextArea { t.placeholder = term.PlainLine(s); return t }

// SetLimit sets the most characters the field takes, line breaks counted,
// 0 for no limit.
func (t *TextArea) SetLimit(n int) *TextArea { t.ed.limit = max(n, 0); return t }

// SetDisabled keeps the field from taking the focus.
func (t *TextArea) SetDisabled(disabled bool) *TextArea { t.disabled = disabled; return t }

// Len is how many characters the text holds, line breaks counted: what a
// counter such as "120/8000" shows.
func (t *TextArea) Len() int { return t.ed.length() }

// Limit is the most characters the field takes, 0 for no limit.
func (t *TextArea) Limit() int { return t.ed.limit }

// CanFocus reports whether the field is enabled.
func (t *TextArea) CanFocus() bool { return !t.disabled }

// TakesText reports that the field takes typed characters.
func (t *TextArea) TakesText() bool { return true }

// segments are where a line breaks into rows of at most width cells: at a
// space where it can, inside a word where the word is wider than a row.
func segments(line []string, width int) [][2]int {
	width = max(width, 1)
	if len(line) == 0 {
		return [][2]int{{0, 0}}
	}
	var out [][2]int
	start := 0
	for start < len(line) {
		used, end, lastSpace := 0, start, -1
		for end < len(line) && used+clusterWidth(line[end]) <= width {
			if line[end] == " " {
				lastSpace = end
			}
			used += clusterWidth(line[end])
			end++
		}
		if end < len(line) && lastSpace >= start && line[end] != " " {
			end = lastSpace + 1
		}
		if end == start {
			end = start + 1
		}
		out = append(out, [2]int{start, end})
		start = end
	}
	return out
}

// row is one row on screen: a line and the clusters of it the row shows.
type row struct {
	line       int
	start, end int
}

func (t *TextArea) rows(width int) []row {
	var out []row
	for i, l := range t.ed.lines {
		for _, s := range segments(l, width) {
			out = append(out, row{line: i, start: s[0], end: s[1]})
		}
	}
	return out
}

// cursorRow is the index of the row the cursor is on.
func cursorRow(rows []row, line, col int) int {
	at := 0
	for i, r := range rows {
		if r.line != line {
			continue
		}
		if col >= r.start {
			at = i
		}
	}
	return at
}

func (t *TextArea) changed() {
	if t.OnChange != nil {
		t.OnChange(t.Value())
	}
}

// moveRows moves the cursor n rows up (negative) or down, keeping to the
// column it was in.
func (t *TextArea) moveRows(n int) {
	width := max(t.width, 1)
	rows := t.rows(width)
	cur := cursorRow(rows, t.ed.row, t.ed.col)
	r := rows[cur]
	if t.ed.want < 0 {
		t.ed.want = colWidth(t.ed.lines[r.line][r.start:], t.ed.col-r.start)
	}
	to := min(max(cur+n, 0), len(rows)-1)
	r = rows[to]
	line := t.ed.lines[r.line][r.start:r.end]
	col := r.start + colAt(line, t.ed.want)
	if col == r.end && r.end > r.start && to < len(rows)-1 && rows[to+1].line == r.line {
		// The end of a row that wraps is the start of the next one.
		col--
	}
	t.ed.row, t.ed.col = r.line, col
}

// HandleKey edits the text.
func (t *TextArea) HandleKey(k Key) bool {
	t.scrolled = false
	page := max(t.height-1, 1)
	switch k.String() {
	case "up", "down", "pgup", "pgdown", "ctrl+home", "ctrl+end":
		// Typing after the caret moved is an edit of its own for undo.
		t.ed.lastGroup = ""
	}
	switch k.String() {
	case "up":
		t.moveRows(-1)
		return true
	case "down":
		t.moveRows(1)
		return true
	case "pgup":
		t.moveRows(-page)
		return true
	case "pgdown":
		t.moveRows(page)
		return true
	case "ctrl+home":
		t.ed.row, t.ed.col, t.ed.want = 0, 0, -1
		return true
	case "ctrl+end":
		t.ed.row = len(t.ed.lines) - 1
		t.ed.col, t.ed.want = len(t.ed.lines[t.ed.row]), -1
		return true
	case "enter":
		t.ed.remember("")
		if t.ed.insertText("\n") {
			t.ed.want = -1
			t.changed()
		}
		return true
	}
	took, changed := t.ed.handle(k)
	if took {
		t.ed.want = -1
	}
	if changed {
		t.changed()
	}
	return took
}

// HandlePaste inserts pasted text, its lines included.
func (t *TextArea) HandlePaste(text string) bool {
	t.scrolled = false
	t.ed.remember("")
	if t.ed.insertText(text) {
		t.ed.want = -1
		t.changed()
	}
	return true
}

// HandleMouse puts the cursor where the field was clicked, and scrolls with
// the wheel.
func (t *TextArea) HandleMouse(m Mouse) bool {
	rows := t.rows(max(t.width, 1))
	switch {
	case m.Button == MouseWheelUp:
		t.top = max(t.top-3, 0)
		t.scrolled = true
	case m.Button == MouseWheelDown:
		t.top = min(t.top+3, max(len(rows)-t.height, 0))
		t.scrolled = true
	case m.Button == MouseLeft && m.Action == MousePress:
		t.scrolled = false
		i := min(t.top+m.Y, len(rows)-1)
		r := rows[i]
		t.ed.row = r.line
		t.ed.col = r.start + colAt(t.ed.lines[r.line][r.start:r.end], m.X)
		t.ed.want = -1
		t.ed.lastGroup = ""
	default:
		return false
	}
	return true
}

// Draw draws the visible rows and a scroll bar where the text does not fit.
func (t *TextArea) Draw(c Canvas) {
	th := c.Theme()
	focused := c.Focused(t)
	t.height = c.Height()
	width := c.Width() - 1
	t.width = max(width, 1)
	if t.ed.length() == 0 && t.placeholder != "" {
		for i, l := range WrapText(t.placeholder, t.width, th.Muted) {
			c.Line(0, i, l)
		}
		if focused {
			c.ShowCursor(0, 0)
		}
		return
	}
	rows := t.rows(t.width)
	cur := cursorRow(rows, t.ed.row, t.ed.col)
	if !t.scrolled {
		if cur < t.top {
			t.top = cur
		}
		if cur >= t.top+t.height {
			t.top = cur - t.height + 1
		}
	}
	t.top = min(t.top, max(len(rows)-t.height, 0))
	for y := 0; y < t.height && t.top+y < len(rows); y++ {
		r := rows[t.top+y]
		x := 0
		for _, cl := range t.ed.lines[r.line][r.start:r.end] {
			x += c.Text(x, y, cl, theme.Style(""))
		}
	}
	c.VScrollbar(c.Width()-1, t.top, t.height, len(rows))
	if focused && cur >= t.top && cur < t.top+t.height {
		r := rows[cur]
		x := colWidth(t.ed.lines[r.line][r.start:], t.ed.col-r.start)
		// The cursor at the end of a full row stands in the column the
		// scroll bar keeps free.
		c.ShowCursor(min(x, t.width), cur-t.top)
	}
}
