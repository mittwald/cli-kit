package tui

import (
	"strconv"
	"strings"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// Column is a column of a list shown as a table.
type Column struct {
	// Title is the header of the column.
	Title string
	// Width is the width of the column in cells. Zero sizes the column to
	// its widest cell among the rows, unless Grow is set.
	Width int
	// Grow makes the column share the room the other columns leave, by
	// weight, like [GrowBy].
	Grow int
	// Min is the narrowest the column gets when room is short.
	Min int
	// Align aligns the cells.
	Align Align
}

// List shows rows to choose from, as a list or as a table with a header,
// and filters them as you type. It is generic over the type of its items:
// the cells of a row come from a function of the item.
//
//	↑/↓ k/j               move
//	pgup/pgdown ctrl+b/f  page up/down
//	ctrl+u/d              half a page up/down
//	g/G home/end          first/last row
//	enter                 choose the row (OnSelect)
//	space                 mark the row, for an action on several
//	ctrl+a                mark every row shown, or none if all are marked
//	/                     filter; enter keeps the filter, esc clears it
//	esc                   clear the filter, else the marks
type List[T any] struct {
	items   []T
	shown   []int
	cells   func(T) []Span
	match   func(T) string
	columns []Column
	style   func(T) theme.Style
	marks   map[int]bool
	keymap  *Keymap

	cursor   int
	top      int
	height   int
	disabled bool

	filtering   bool
	filter      *editor
	filterFirst int
	empty       string
	loading     bool
	total       int

	// OnSelect is called with the item Enter was pressed on.
	OnSelect func(item T)
	// OnCursor is called with the item under the cursor whenever it
	// changes, such as for a preview.
	OnCursor func(item T)
	// OnFilter is called with the filter after every change, for a list
	// whose rows come from a search on a server. Leave Match unset then, so
	// the list does not filter the rows itself as well.
	OnFilter func(filter string)
	// OnEnd is called when the cursor comes within a page of the last row,
	// to load more rows.
	OnEnd func()
}

// NewList returns a list whose rows cells makes from its items.
func NewList[T any](cells func(item T) []Span) *List[T] {
	l := &List[T]{cells: cells, marks: map[int]bool{}, keymap: NewKeymap(), filter: newEditor(false), empty: "Nothing to show"}
	l.match = func(item T) string {
		var b strings.Builder
		for _, s := range l.cells(item) {
			b.WriteString(s.Text)
			b.WriteByte(' ')
		}
		return b.String()
	}
	k := l.keymap
	k.Bind("move", func() { l.Move(-1) }, "up", "k").InGroup("List")
	k.Bind("move", func() { l.Move(1) }, "down", "j").Hide().InGroup("List")
	k.Bind("page up", func() { l.Move(-l.page()) }, "pgup", "ctrl+b").Hide().InGroup("List")
	k.Bind("page down", func() { l.Move(l.page()) }, "pgdown", "ctrl+f").Hide().InGroup("List")
	k.Bind("half page up", func() { l.Move(-max(l.height/2, 1)) }, "ctrl+u").Hide().InGroup("List")
	k.Bind("half page down", func() { l.Move(max(l.height/2, 1)) }, "ctrl+d").Hide().InGroup("List")
	k.Bind("first", func() { l.SetCursor(0) }, "g", "home").Hide().InGroup("List")
	k.Bind("last", func() { l.SetCursor(len(l.shown) - 1) }, "G", "end").Hide().InGroup("List")
	k.Bind("open", l.choose, "enter").When(func() bool { return l.OnSelect != nil && len(l.shown) > 0 }).InGroup("List")
	k.Bind("mark", l.toggleMark, "space").When(func() bool { return len(l.shown) > 0 }).InGroup("List")
	k.Bind("mark all", l.toggleAll, "ctrl+a").When(func() bool { return len(l.shown) > 0 }).Hide().InGroup("List")
	k.Bind("filter", l.openFilter, "/").InGroup("List")
	k.Bind("clear", l.clear, "esc").When(func() bool { return l.Filter() != "" || len(l.marks) > 0 }).InGroup("List")
	return l
}

// Keymap returns the keys of the list.
func (l *List[T]) Keymap() *Keymap { return l.keymap }

// SetColumns shows the list as a table with these columns, one per cell.
func (l *List[T]) SetColumns(columns ...Column) *List[T] { l.columns = columns; return l }

// SetMatch sets the text the filter is matched against, the cells
// otherwise. A nil fn leaves filtering to OnFilter.
func (l *List[T]) SetMatch(fn func(item T) string) *List[T] { l.match = fn; l.refilter(); return l }

// SetRowStyle sets a style for whole rows, such as a muted one for closed
// tickets.
func (l *List[T]) SetRowStyle(fn func(item T) theme.Style) *List[T] { l.style = fn; return l }

// SetEmpty sets what the list says when there are no rows.
func (l *List[T]) SetEmpty(text string) *List[T] { l.empty = term.PlainLine(text); return l }

// SetLoading shows that rows are being loaded.
func (l *List[T]) SetLoading(loading bool) { l.loading = loading }

// SetTotal sets how many items there are in all, where the list holds only
// the first of them, for the position to show "20/52541".
func (l *List[T]) SetTotal(n int) { l.total = n }

// SetDisabled keeps the list from taking the focus.
func (l *List[T]) SetDisabled(disabled bool) *List[T] { l.disabled = disabled; return l }

// SetItems replaces the items. The cursor stays on the row it was on where
// it can, and the marks go.
func (l *List[T]) SetItems(items []T) {
	l.items = items
	l.marks = map[int]bool{}
	// The index under the cursor may stay while the item there changed:
	// the cursor is announced once, whether refilter moved it or not.
	if !l.refilter() {
		if item, ok := l.Selected(); ok && l.OnCursor != nil {
			l.OnCursor(item)
		}
	}
}

// AppendItems adds items at the end, as a page loaded after OnEnd. The
// cursor and the marks stay.
func (l *List[T]) AppendItems(items ...T) {
	l.items = append(l.items, items...)
	l.refilter()
}

// Items returns every item, those filtered out included.
func (l *List[T]) Items() []T { return l.items }

// Shown returns the items the filter lets through, in order.
func (l *List[T]) Shown() []T {
	out := make([]T, len(l.shown))
	for i, j := range l.shown {
		out[i] = l.items[j]
	}
	return out
}

// Selected returns the item under the cursor, and false when there is
// none.
func (l *List[T]) Selected() (T, bool) {
	var zero T
	if l.cursor < 0 || l.cursor >= len(l.shown) {
		return zero, false
	}
	return l.items[l.shown[l.cursor]], true
}

// Marked returns the marked items, or the item under the cursor when none
// is marked: what an action on several items acts on.
func (l *List[T]) Marked() []T {
	var out []T
	for _, i := range l.shown {
		if l.marks[i] {
			out = append(out, l.items[i])
		}
	}
	if len(out) == 0 {
		if item, ok := l.Selected(); ok {
			out = append(out, item)
		}
	}
	return out
}

// ClearMarks removes every mark.
func (l *List[T]) ClearMarks() { l.marks = map[int]bool{} }

// Cursor returns the index of the row under the cursor among the rows
// shown.
func (l *List[T]) Cursor() int { return l.cursor }

// SetCursor puts the cursor on the row of index i among the rows shown.
func (l *List[T]) SetCursor(i int) {
	if len(l.shown) == 0 {
		l.cursor = 0
		return
	}
	i = min(max(i, 0), len(l.shown)-1)
	moved := i != l.cursor
	l.cursor = i
	if moved && l.OnCursor != nil {
		l.OnCursor(l.items[l.shown[i]])
	}
	if l.OnEnd != nil && i >= len(l.shown)-max(l.height, 1) {
		l.OnEnd()
	}
}

// Move moves the cursor n rows down, or up for a negative n.
func (l *List[T]) Move(n int) { l.SetCursor(l.cursor + n) }

// Filter returns the filter.
func (l *List[T]) Filter() string { return l.filter.String() }

// SetFilter sets the filter.
func (l *List[T]) SetFilter(s string) {
	l.filter.set(term.PlainLine(s))
	l.filterChanged()
}

// Filtering reports whether the filter line is open for typing.
func (l *List[T]) Filtering() bool { return l.filtering }

// CanFocus reports whether the list takes the focus.
func (l *List[T]) CanFocus() bool { return !l.disabled }

// TakesText reports whether the filter line takes typed characters now.
func (l *List[T]) TakesText() bool { return l.filtering }

func (l *List[T]) page() int { return max(l.height-1, 1) }

func (l *List[T]) choose() {
	if item, ok := l.Selected(); ok && l.OnSelect != nil {
		l.OnSelect(item)
	}
}

func (l *List[T]) toggleMark() {
	if l.cursor >= len(l.shown) {
		return
	}
	i := l.shown[l.cursor]
	if l.marks[i] {
		delete(l.marks, i)
	} else {
		l.marks[i] = true
	}
	l.Move(1)
}

func (l *List[T]) toggleAll() {
	all := true
	for _, i := range l.shown {
		all = all && l.marks[i]
	}
	for _, i := range l.shown {
		if all {
			delete(l.marks, i)
		} else {
			l.marks[i] = true
		}
	}
}

func (l *List[T]) openFilter() { l.filtering = true }

func (l *List[T]) clear() {
	if l.Filter() != "" {
		l.SetFilter("")
		return
	}
	l.ClearMarks()
}

func (l *List[T]) filterChanged() {
	l.refilter()
	if l.OnFilter != nil {
		l.OnFilter(l.Filter())
	}
}

// refilter works out the rows the filter lets through, keeping the cursor
// on the item it was on where it is still shown.
// It reports whether it announced a new cursor item to OnCursor.
func (l *List[T]) refilter() (notified bool) {
	at := -1
	if l.cursor < len(l.shown) {
		at = l.shown[l.cursor]
	}
	defer func() {
		now := -1
		if l.cursor < len(l.shown) {
			now = l.shown[l.cursor]
		}
		if now != at && now >= 0 && l.OnCursor != nil {
			l.OnCursor(l.items[now])
			notified = true
		}
	}()
	terms := strings.Fields(strings.ToLower(l.Filter()))
	l.shown = l.shown[:0]
	for i, item := range l.items {
		if len(terms) > 0 && l.match != nil {
			text := strings.ToLower(l.match(item))
			ok := true
			for _, t := range terms {
				if !strings.Contains(text, t) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
		}
		l.shown = append(l.shown, i)
	}
	l.cursor = min(l.cursor, max(len(l.shown)-1, 0))
	for i, j := range l.shown {
		if j == at {
			l.cursor = i
			break
		}
	}
	return false
}

// HandleKey edits the filter while its line is open.
func (l *List[T]) HandleKey(k Key) bool {
	if !l.filtering {
		return false
	}
	switch k.String() {
	case "enter", "down", "up", "tab":
		l.filtering = false
		if k.Code == KeyDown || k.Code == KeyUp {
			return false
		}
		return k.Code == KeyEnter
	case "esc":
		l.filtering = false
		l.SetFilter("")
		return true
	}
	took, changed := l.filter.handle(k)
	if changed {
		l.filterChanged()
	}
	// Characters the editor did not take are swallowed rather than taken
	// for bindings; Ctrl and Alt keys go on, so Ctrl-C still quits.
	return took || k.Printable()
}

// HandlePaste takes pasted text into the filter while its line is open.
func (l *List[T]) HandlePaste(text string) bool {
	if !l.filtering {
		return false
	}
	l.filter.remember("")
	if l.filter.insertText(text) {
		l.filterChanged()
	}
	return true
}

// HandleMouse moves the cursor to the row clicked, and scrolls with the
// wheel.
func (l *List[T]) HandleMouse(m Mouse) bool {
	if l.disabled {
		return false
	}
	switch {
	case m.Button == MouseWheelUp:
		l.Move(-3)
	case m.Button == MouseWheelDown:
		l.Move(3)
	case m.Button == MouseLeft && m.Action == MousePress:
		y := m.Y
		if l.columns != nil {
			y--
		}
		if y >= 0 && l.top+y < len(l.shown) {
			l.SetCursor(l.top + y)
		}
	default:
		return false
	}
	return true
}

// Position is where the cursor is, as "3/20", or "3/20 of 52541" with a
// total set.
func (l *List[T]) Position() string {
	if len(l.shown) == 0 {
		return "0/0"
	}
	s := strconv.Itoa(l.cursor+1) + "/" + strconv.Itoa(len(l.shown))
	if l.total > len(l.items) {
		s += " of " + strconv.Itoa(l.total)
	}
	return s
}

// widths works out the width of each column in width cells.
func (l *List[T]) widths(width int, rows [][]Span) []int {
	n := len(l.columns)
	out := make([]int, n)
	gaps := max(n-1, 0)
	left := width - gaps - 2
	grow := 0
	for i, c := range l.columns {
		switch {
		case c.Width > 0:
			out[i] = c.Width
		case c.Grow > 0:
			grow += c.Grow
			out[i] = c.Min
		default:
			w := textWidth(c.Title)
			for _, r := range rows {
				if i < len(r) {
					w = max(w, textWidth(r[i].Text))
				}
			}
			out[i] = w
		}
		left -= out[i]
	}
	if grow > 0 && left > 0 {
		given, last := 0, -1
		for i, c := range l.columns {
			if c.Grow > 0 {
				share := left * c.Grow / grow
				out[i] += share
				given += share
				last = i
			}
		}
		out[last] += left - given
	}
	// Short of room, the widest columns give way first, down to their
	// minimum.
	for left < 0 {
		widest := -1
		for i, c := range l.columns {
			if out[i] > max(c.Min, 1) && (widest < 0 || out[i] > out[widest]) {
				widest = i
			}
		}
		if widest < 0 {
			break
		}
		out[widest]--
		left++
	}
	return out
}

// drawCells draws the cells of a row into the columns.
func drawCells(c Canvas, x, y int, cells []Span, widths []int, columns []Column, base theme.Style) {
	for i, w := range widths {
		if i >= len(cells) {
			break
		}
		text := truncate(oneLine(cells[i].Text), w)
		off := 0
		switch columns[i].Align {
		case AlignRight:
			off = w - textWidth(text)
		case AlignCenter:
			off = (w - textWidth(text)) / 2
		}
		c.Text(x+off, y, text, theme.Join(base, cells[i].Style))
		x += w + 1
	}
}

// Draw draws the header, the rows in view, the scroll bar and the filter
// line.
func (l *List[T]) Draw(c Canvas) {
	th := c.Theme()
	focused := c.Focused(l)
	y0 := 0
	height := c.Height()
	showFilter := l.filtering || l.Filter() != ""
	if showFilter {
		height--
	}
	if l.columns != nil {
		y0 = 1
		height--
	}
	l.height = max(height, 1)
	if l.cursor < l.top {
		l.top = l.cursor
	}
	if l.cursor >= l.top+l.height {
		l.top = l.cursor - l.height + 1
	}
	l.top = min(l.top, max(len(l.shown)-l.height, 0))
	rows := make([][]Span, 0, l.height)
	for i := l.top; i < len(l.shown) && i < l.top+l.height; i++ {
		rows = append(rows, l.cells(l.items[l.shown[i]]))
	}
	width := c.Width() - 1
	var widths []int
	if l.columns != nil {
		widths = l.widths(width, rows)
		var header []Span
		for _, col := range l.columns {
			header = append(header, Span{Text: col.Title})
		}
		drawCells(c, 2, 0, header, widths, l.columns, th.Header)
	}
	if len(l.shown) == 0 {
		text := l.empty
		if l.loading {
			text = "Loading…"
		}
		c.Text(2, y0, text, th.Muted)
	}
	for r, cells := range rows {
		i := l.top + r
		y := y0 + r
		item := l.items[l.shown[i]]
		base := theme.Style("")
		if l.style != nil {
			base = l.style(item)
		}
		if l.marks[l.shown[i]] {
			base = theme.Join(base, th.Marked)
			c.Text(0, y, "▌", th.Marked)
		}
		if i == l.cursor {
			cursor := th.CursorBlurred
			if focused {
				cursor = th.Cursor
			}
			// The row keeps its own color, on the cursor's background.
			base = theme.Join(cursor, base, background(cursor))
			c.Sub(0, 0, width, c.Height()).WithStyle(cursor).FillRow(1, y, "")
		}
		if l.columns != nil {
			drawCells(c.Sub(0, 0, width, c.Height()), 2, y, cells, widths, l.columns, base)
			continue
		}
		x := 2
		for _, s := range cells {
			x += c.Sub(0, 0, width, c.Height()).Text(x, y, oneLine(s.Text), theme.Join(base, s.Style))
		}
		if x > width {
			c.Text(width-1, y, term.Ellipsis, base)
		}
	}
	c.Sub(0, y0, c.Width(), l.height).VScrollbar(c.Width()-1, l.top, l.height, len(l.shown))
	if showFilter {
		y := c.Height() - 1
		x := c.Text(0, y, "/", th.Key)
		style := th.Text
		if !l.filtering {
			style = th.Accent
		}
		drawLine(c, x, y, l.filter, &l.filterFirst, style, l.filtering && focused)
	}
}
