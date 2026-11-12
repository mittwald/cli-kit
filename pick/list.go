package pick

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/mittwald/cli-kit/theme"
)

const (
	// minVisible is how many rows a block keeps on screen before it gives up
	// the lines around them: on a terminal too short for both, the rows are
	// what the prompt is for.
	minVisible = 3
	// defaultVisible caps the rows drawn when the terminal height is unknown.
	defaultVisible = 15
)

// loadState is how far a view that loads its rows has come.
type loadState int

const (
	loadWaiting loadState = iota
	loadRunning
	loadDone
)

// list is the selection state: the items, the filter, and where the cursor
// and the scroll window sit inside the filtered subset. It is pure (keys go
// in through apply, lines come out of render) so the behavior is testable
// without a terminal.
type list struct {
	title    string
	subject  string
	note     string
	fallback string

	items []Item
	// given is how many items the caller gave. Typed rows that were marked
	// are appended after them.
	given  int
	widths []int
	// fields are the searchable parts of every item, lowered once.
	fields [][]MatchField

	color bool
	theme theme.Theme
	// frame is what the block keeps of the lines around the rows on the
	// terminal it is drawn on, and visible how many rows that leaves.
	frame   frame
	visible int

	filter []rune
	// matches are the indexes of the items the filter and view leave; cursor
	// and top are positions in that subset.
	matches []int
	cursor  int
	top     int

	// views are the ways of looking at the list. scoped says they hold their
	// own rows; otherwise they narrow the items by group, and a list with
	// fewer than two has none.
	views    []View
	view     int
	scoped   bool
	loads    []loadState
	failures []string
	// pendingKey is the key of the row selected when a view still loading
	// was switched to, for the cursor to land on once its rows are there.
	pendingKey string
	// initial is Options.Initial for as long as the view the list opened on
	// is still loading its rows: the item it names is not there before.
	initial int

	typed     func(string) bool
	typedNote string

	// multi lets Space mark rows. boxes is set by the first Space: until then
	// the list reads as one answered with a single row, in a lane kept blank
	// for the boxes so no row moves when they appear.
	multi  bool
	marked map[int]bool
	boxes  bool

	cellStyles  []theme.Style
	paintCell   func(int, string, bool) (string, bool)
	detailStyle theme.Style
	priorities  []int
	truncation  []Truncation

	rangeMode bool
	unit      string
	// anchor is the position an open range started on, -1 while none is open.
	anchor int

	keyLine            string
	nested             bool
	dismissOnBackspace bool
}

func newList(opts Options, height int, color bool) *list {
	l := &list{
		title:              plainLine(opts.Title),
		subject:            plainLine(opts.Subject),
		note:               plainLine(opts.Note),
		fallback:           plainLine(opts.Title),
		color:              color,
		theme:              theme.Active(),
		typed:              opts.Typed,
		typedNote:          plainLine(opts.TypedNote),
		multi:              opts.multi,
		marked:             map[int]bool{},
		cellStyles:         opts.CellStyles,
		paintCell:          opts.PaintCell,
		detailStyle:        opts.DetailStyle,
		priorities:         opts.ColumnPriority,
		truncation:         opts.ColumnTruncation,
		rangeMode:          opts.Range,
		unit:               plainLine(opts.RangeUnit),
		anchor:             -1,
		keyLine:            plainLine(opts.Hint),
		nested:             opts.Nested,
		dismissOnBackspace: opts.DismissOnBackspace,
	}
	l.scoped = opts.scoped()
	if l.scoped || len(opts.Views) > 1 {
		l.views = plainViews(opts.Views)
	}
	l.loads = make([]loadState, len(l.views))
	l.failures = make([]string, len(l.views))
	if opts.View > 0 && opts.View < len(l.views) {
		l.view = opts.View
	}
	if lens := opts.Lens; lens != nil {
		l.filter = []rune(plainLine(lens.Filter))
		// A lens that names no view leaves the one the options ask for.
		if lens.View != "" {
			for i := range l.views {
				if l.viewLabel(i) == lens.View {
					l.view = i
					break
				}
			}
		}
	}
	l.applyViewTitle()
	if l.scoped {
		l.setItems(l.views[l.view].Items)
	} else {
		l.setItems(opts.Items)
	}
	l.resize(opts.MaxVisible, height)
	l.applyFilter()
	if l.needsLoad() {
		l.initial = opts.Initial
	}
	l.moveToItem(opts.Initial)
	l.scrollToCursor()
	return l
}

// plainViews copies the views with what they draw made plain text. The copy
// is the list's own, so rows a view loads never show up in the caller's.
func plainViews(views []View) []View {
	plain := slices.Clone(views)
	for i := range plain {
		plain[i].Name = plainLine(plain[i].Name)
		plain[i].Title = plainLine(plain[i].Title)
		plain[i].Loading = plainLine(plain[i].Loading)
		plain[i].Empty = plainLine(plain[i].Empty)
	}
	return plain
}

// viewLabel names a view in the status line and in a Lens: its name, or its
// position, counted from 1, when it has none.
func (l *list) viewLabel(view int) string {
	return firstNonEmpty(l.views[view].Name, strconv.Itoa(view+1))
}

// moveToItem puts the cursor on the item with the index Options.Initial
// names. Initial names an item, and the cursor stands among the matches: an
// item the filter or view hides, or an index out of range, leaves the cursor
// where it is, on the first row shown.
func (l *list) moveToItem(index int) {
	if index <= 0 {
		return
	}
	if position := slices.Index(l.matches, index); position >= 0 {
		l.cursor = position
	}
}

// setItems makes items the rows on screen: copies of them, with everything
// a row draws made plain text. What the filter searches and what identifies
// a row is left as the caller gave it.
func (l *list) setItems(items []Item) {
	l.items = make([]Item, len(items))
	l.given = len(items)
	l.fields = make([][]MatchField, len(items))
	for i, item := range items {
		item.Cells = plainLines(item.Cells)
		item.Action = plainLine(item.Action)
		item.Flag = plainLine(item.Flag)
		item.Detail = plainLine(item.Detail)
		if len(item.Details) > 0 {
			item.Details = slices.Clone(item.Details)
			for j := range item.Details {
				item.Details[j].Text = plainLine(item.Details[j].Text)
			}
		}
		l.items[i] = item
		l.fields[i] = item.matchFields()
	}
}

// switchable reports whether there are views to switch between.
func (l *list) switchable() bool { return len(l.views) > 1 }

// lens is the filter and the view the list stands on now. A list without
// views to switch between leaves the view in was alone, so a list of another
// shape asked in between does not lose it for the next.
func (l *list) lens(was Lens) Lens {
	view := was.View
	if len(l.views) > 0 {
		view = l.viewLabel(l.view)
	}
	return Lens{Filter: string(l.filter), View: view}
}

// frame is what a block draws around its rows, and how many rows it shows.
// The lines are listed in the order they are drawn.
type frame struct {
	title    bool
	note     bool
	gapAbove bool
	rows     int
	gapBelow bool
	position bool
	hint     bool
}

// fitFrame lays a block out on a terminal height rows tall, leaving one row
// spare so the block never fills the screen: a block taller than the
// terminal would scroll it, and the in-place redraw would then repaint the
// wrong lines.
//
// want is how many rows the block would show given the room, note whether it
// has a note, and counted whether its position line is always there (a list)
// or only while some rows are out of sight (a set of switches). A terminal
// too short for the whole block gives up the lines around the rows, one
// after the other, until minVisible rows fit: first the note, then the blank
// lines, the key line and the position line. The title goes last, when
// there is not even room for it and one row. An unknown height (0 or less)
// keeps everything.
func fitFrame(height, want int, note, counted bool) frame {
	f := frame{title: true, note: note, gapAbove: true, gapBelow: true, position: true, hint: true}
	want = max(want, 1)
	if height <= 0 {
		f.rows = want
		f.position = counted
		return f
	}
	room := max(height-1, 1)
	fill := func() {
		rows := room
		for _, line := range []bool{f.title, f.note, f.gapAbove, f.gapBelow, f.hint} {
			if line {
				rows--
			}
		}
		if f.position && (counted || rows < want) {
			rows--
		}
		f.rows = min(rows, want)
	}
	enough := min(want, minVisible)
	fill()
	for _, line := range []*bool{&f.note, &f.gapBelow, &f.gapAbove, &f.hint, &f.position} {
		if f.rows >= enough {
			break
		}
		*line = false
		fill()
	}
	if f.rows < 1 {
		f.title = false
		fill()
	}
	f.position = f.position && (counted || f.rows < want)
	return f
}

// resize lays the block out again after a terminal size change.
func (l *list) resize(maxVisible, height int) {
	want := math.MaxInt
	if height <= 0 {
		want = defaultVisible
	}
	if maxVisible > 0 {
		want = min(want, max(maxVisible, minVisible))
	}
	fitted := fitFrame(height, want, l.note != "", true)
	if fitted == l.frame {
		return
	}
	l.frame, l.visible = fitted, fitted.rows
	l.scrollToCursor()
}

func columnWidths(items []Item) []int {
	var widths []int
	for _, item := range items {
		for i, cell := range item.Cells {
			for len(widths) <= i {
				widths = append(widths, 0)
			}
			if i == 0 {
				cell = flagged(cell, item.Flag)
			}
			widths[i] = max(widths[i], width(cell))
		}
	}
	return widths
}

// selected is the index of the item under the cursor, or -1 when nothing
// matches.
func (l *list) selected() int {
	if l.cursor < 0 || l.cursor >= len(l.matches) {
		return -1
	}
	return l.matches[l.cursor]
}

// selectedRange is the pair of item indexes the list ends on, in drawing
// order. Outside an open range both are the row under the cursor.
func (l *list) selectedRange() (int, int) {
	first := l.selected()
	if !l.rangeMode || l.anchor < 0 || first < 0 {
		return first, first
	}
	low, high := min(l.anchor, l.cursor), max(l.anchor, l.cursor)
	return l.matches[low], l.matches[high]
}

// inSpan reports whether a position lies inside the open range.
func (l *list) inSpan(position int) bool {
	if l.anchor < 0 {
		return false
	}
	return position >= min(l.anchor, l.cursor) && position <= max(l.anchor, l.cursor)
}

// spanSize is how many rows the open range covers, 0 while none is open.
func (l *list) spanSize() int {
	if l.anchor < 0 {
		return 0
	}
	return max(l.anchor, l.cursor) - min(l.anchor, l.cursor) + 1
}

func (l *list) apply(k key) action {
	switch k.kind {
	case keyEnter:
		if len(l.marked) > 0 {
			return actionSelect
		}
		if l.selected() < 0 || l.row(l.selected()).Disabled {
			return actionNone
		}
		if l.rangeMode && l.anchor < 0 {
			l.openRange()
			return actionNone
		}
		return actionSelect
	case keyBack:
		if l.anchor >= 0 {
			// Esc drops an open range before it leaves the list.
			l.closeRange()
			return actionNone
		}
		return actionBack
	case keyInterrupt:
		return actionInterrupt
	case keyUp:
		l.move(-1)
	case keyDown:
		l.move(1)
	case keyPageUp:
		l.move(-l.visible)
	case keyPageDown:
		l.move(l.visible)
	case keyHome:
		l.move(-len(l.matches))
	case keyEnd:
		l.move(len(l.matches))
	case keyBackspace:
		if l.anchor >= 0 {
			// The visible rows are frozen while a range is open.
			return actionNone
		}
		if len(l.filter) == 0 {
			if l.dismissOnBackspace {
				return actionBack
			}
			return actionNone
		}
		l.setFilter(l.filter[:len(l.filter)-1])
	case keyClearFilter:
		if l.anchor < 0 && len(l.filter) > 0 {
			l.setFilter(nil)
		}
	case keyRune:
		if l.anchor >= 0 {
			break
		}
		if l.multi && k.rune == ' ' {
			l.boxes = true
			l.toggleMark()
			break
		}
		l.setFilter(append(l.filter, k.rune))
	case keyTab, keyRight:
		l.switchView(1)
	case keyBackTab, keyLeft:
		l.switchView(-1)
	}
	return actionNone
}

// openRange anchors a range on the row under the cursor. The filter is
// cleared with it, and a view that narrows the rows is set aside for as long
// as the range is open: a row hidden between the two ends would end up
// inside a range nobody could see.
func (l *list) openRange() {
	item := l.selected()
	l.anchor = l.cursor
	if len(l.filter) > 0 || l.narrowed() {
		l.filter = nil
		// With the anchor set, applyFilter leaves the view out.
		l.applyFilter()
		l.anchor = max(slices.Index(l.matches, item), 0)
		l.cursor = l.anchor
		l.scrollToCursor()
	}
}

// closeRange drops the open range. The view the range set aside narrows the
// rows again, and the cursor stays on its row where the view shows it.
func (l *list) closeRange() {
	item := l.selected()
	l.anchor = -1
	if !l.narrowed() {
		return
	}
	l.applyFilter()
	l.cursor = max(slices.Index(l.matches, item), 0)
	l.scrollToCursor()
}

// narrowed reports whether the view the list stands on shows only some of
// the items.
func (l *list) narrowed() bool {
	return !l.scoped && len(l.views) > 0 && len(l.views[l.view].Groups) > 0
}

// switchView moves delta views along, wrapping at both ends. It does nothing
// while a range is open or when there is no view to switch to.
func (l *list) switchView(delta int) {
	if !l.switchable() || l.anchor >= 0 {
		return
	}
	n := len(l.views)
	if !l.scoped {
		l.view = (l.view + delta + n) % n
		l.applyViewTitle()
		l.setFilter(l.filter)
		return
	}
	key := ""
	if index := l.selected(); index >= 0 && index < len(l.items) {
		key = l.items[index].Key
	}
	l.view = (l.view + delta + n) % n
	// Initial was about the view the list opened on.
	l.initial = 0
	l.pendingKey = ""
	if l.loading() {
		l.pendingKey = key
	}
	l.loadView(key)
}

// applyViewTitle shows the current view's title, or the options' title for
// a view without one.
func (l *list) applyViewTitle() {
	if len(l.views) > 0 {
		l.title = firstNonEmpty(l.views[l.view].Title, l.fallback)
	}
}

// loadView makes the current view's own rows the ones on screen. The filter
// carries over, and the cursor lands on the row carrying key, or at the top.
func (l *list) loadView(key string) {
	view := l.views[l.view]
	l.applyViewTitle()
	l.setItems(view.Items)
	l.cursor, l.top = 0, 0
	l.applyFilter()
	// The view the list opened on starts where Options.Initial says, also
	// when its rows only arrive now.
	if !l.loading() {
		l.moveToItem(l.initial)
		l.initial = 0
	}
	if key != "" {
		for position, index := range l.matches {
			if l.items[index].Key == key {
				l.cursor = position
				break
			}
		}
	}
	l.scrollToCursor()
}

// needsLoad reports whether the current view has rows to fetch and nobody
// fetching them yet.
func (l *list) needsLoad() bool {
	return l.scoped && l.views[l.view].Load != nil && l.loads[l.view] == loadWaiting
}

// loading reports whether the current view is still without the rows it
// loads: the fetch has not started yet, or has not come back.
func (l *list) loading() bool {
	return l.scoped && l.views[l.view].Load != nil && l.loads[l.view] != loadDone
}

// nextLoad hands the key loop the fetch of the current view's rows, once:
// a function that calls the view's Load, to be run off the key loop, whose
// result takes the rows into the list, back on the key loop. The key loop
// asks after a draw, so the view and its loading line are on screen while
// the fetch runs, and the list keeps taking keys meanwhile: Esc leaves it,
// Tab moves on to another view, and typing already filters what is to come.
func (l *list) nextLoad() func() func() {
	if !l.needsLoad() {
		return nil
	}
	view := l.view
	load := l.views[view].Load
	l.loads[view] = loadRunning
	return func() func() {
		items, err := load()
		return func() { l.finishLoad(view, items, err) }
	}
}

// finishLoad takes the rows a view loaded. They go on screen when the list
// still stands on that view, with the cursor on the row it was to land on,
// and are kept for the view otherwise.
func (l *list) finishLoad(view int, items []Item, err error) {
	l.loads[view] = loadDone
	if err != nil {
		// Whatever was gathered before the failure is still worth showing;
		// with nothing gathered, the rows the view already had stay.
		l.failures[view] = plainLine(err.Error())
		if len(items) == 0 {
			return
		}
	}
	l.views[view].Items = items
	if view != l.view {
		return
	}
	key := l.pendingKey
	l.pendingKey = ""
	l.loadView(key)
}

// toggleMark marks the row under the cursor or unmarks it. The cursor stays,
// so the mark is seen landing and a second Space takes it back. A disabled
// row cannot be marked. A typed row becomes a row of its own as it is
// marked, kept after the caller's whatever the filter turns into next.
func (l *list) toggleMark() {
	index := l.selected()
	if index < 0 || l.row(index).Disabled {
		return
	}
	switch {
	case index == l.typedIndex():
		item := l.row(index)
		l.items = append(slices.Clip(l.items), item)
		l.fields = append(l.fields, item.matchFields())
		l.marked[index] = true
		l.setFilter(l.filter)
	case l.marked[index]:
		delete(l.marked, index)
	default:
		l.marked[index] = true
	}
}

// chosen is what the list was answered with: the rows marked, in list order,
// and what was typed and marked, in the order it was marked. With nothing
// marked it is the row under the cursor alone.
func (l *list) chosen() ([]int, []string) {
	var indexes []int
	var typed []string
	add := func(index int) {
		switch {
		case index == l.typedIndex():
			typed = append(typed, l.typedText())
		case index >= l.given:
			typed = append(typed, l.items[index].Cells[0])
		case index >= 0:
			indexes = append(indexes, index)
		}
	}
	if len(l.marked) == 0 {
		add(l.selected())
		return indexes, typed
	}
	for index := range l.items {
		if l.marked[index] {
			add(index)
		}
	}
	return indexes, typed
}

func (l *list) move(delta int) {
	if len(l.matches) == 0 {
		l.cursor = 0
		return
	}
	l.cursor = min(max(l.cursor+delta, 0), len(l.matches)-1)
	l.scrollToCursor()
}

// setFilter recomputes the matching subset, keeping the cursor on the item it
// pointed at when that item still matches.
func (l *list) setFilter(filter []rune) {
	previous := l.selected()
	l.filter = filter
	l.applyFilter()
	for i, index := range l.matches {
		if index == previous {
			l.cursor = i
			break
		}
	}
	l.scrollToCursor()
}

// applyFilter rebuilds the matching subset and puts the cursor at the top.
// Every whitespace-separated term has to match, so typing more words narrows
// instead of widening.
func (l *list) applyFilter() {
	terms := strings.Fields(strings.ToLower(string(l.filter)))
	l.matches = l.matches[:0]
	for i := range l.items {
		// A typed row that was marked shows in every view: it is of no
		// group the views know. And an open range shows every row, since
		// every row between its ends is part of it.
		if l.narrowed() && l.anchor < 0 && i < l.given && !l.views[l.view].shows(l.items[i]) {
			continue
		}
		if matchesTerms(l.fields[i], terms) {
			l.matches = append(l.matches, i)
		}
	}
	// The typed row is drawn in the columns the others are, so its text
	// counts towards their widths while it is shown.
	rows := l.items
	if l.offersTyped() {
		l.matches = append(l.matches, l.typedIndex())
		rows = append(slices.Clip(l.items), l.row(l.typedIndex()))
	}
	l.widths = columnWidths(rows)
	l.cursor = 0
}

// typedIndex is where the typed row stands among the indexes matches holds:
// one past the items, so it is never mistaken for one of them.
func (l *list) typedIndex() int { return len(l.items) }

// typedText is the filter taken as an answer.
func (l *list) typedText() string { return strings.TrimSpace(string(l.filter)) }

// offersTyped reports whether the filter is itself an answer: the caller
// accepts it, and no row on offer is named by it already. A row of that name
// which the view or the filter hides cannot be chosen in its place, so it
// does not stand in the way.
func (l *list) offersTyped() bool {
	text := l.typedText()
	if l.typed == nil || text == "" || !l.typed(text) {
		return false
	}
	for _, index := range l.matches {
		if item := l.items[index]; len(item.Cells) > 0 && item.Cells[0] == text {
			return false
		}
	}
	return true
}

// row is the item at index, the typed row included.
func (l *list) row(index int) Item {
	if index == l.typedIndex() {
		return Item{Cells: []string{l.typedText(), l.typedNote}}
	}
	return l.items[index]
}

// matchesTerms requires every term to appear in one of the row's fields. A
// field only answers terms at least as long as its MinTerm.
func matchesTerms(fields []MatchField, terms []string) bool {
	for _, term := range terms {
		if !matchesTerm(fields, term) {
			return false
		}
	}
	return true
}

func matchesTerm(fields []MatchField, term string) bool {
	for _, field := range fields {
		if field.MinTerm > 0 && len([]rune(term)) < field.MinTerm {
			continue
		}
		if strings.Contains(field.Text, term) {
			return true
		}
	}
	return false
}

func (l *list) scrollToCursor() {
	if l.cursor < l.top {
		l.top = l.cursor
	}
	if l.cursor >= l.top+l.visible {
		l.top = l.cursor - l.visible + 1
	}
	l.top = max(min(l.top, len(l.matches)-l.visible), 0)
}

func (l *list) painter() painter { return painter{color: l.color, theme: l.theme} }

func (l *list) styled(style theme.Style, text string) string {
	return l.painter().paint(style, text)
}

// render returns the block to draw, one string per terminal line, already cut
// to width. The caller owns cursor movement and erasing.
func (l *list) render(width int) []string {
	if width <= 0 {
		width = 80
	}
	th := l.theme
	widths := l.fittedWidths(width)
	lines := make([]string, 0, l.visible+6)
	if l.frame.title {
		lines = append(lines, l.renderTitle(width))
	}
	if l.frame.note {
		lines = append(lines, l.styled(th.Muted, cut(l.note, width)))
	}
	if l.frame.gapAbove {
		lines = append(lines, "")
	}
	end := min(l.top+l.visible, len(l.matches))
	for position := l.top; position < end; position++ {
		lines = append(lines, l.renderRowWith(position, width, widths))
	}
	if len(l.matches) == 0 {
		text, style := l.emptyLine()
		lines = append(lines, l.styled(style, cut(text, width)))
	}
	if l.frame.gapBelow {
		lines = append(lines, "")
	}
	if l.frame.position {
		lines = append(lines, l.renderPosition(width))
	}
	if l.frame.hint {
		lines = append(lines, l.styled(th.Muted, l.hint(width)))
	}
	return lines
}

// emptyLine says why there are no rows: the view is still loading, it failed
// to load, the filter matched nothing, or the view holds nothing.
func (l *list) emptyLine() (string, theme.Style) {
	var view View
	if len(l.views) > 0 {
		view = l.views[l.view]
	}
	switch {
	case l.loading():
		return "  " + firstNonEmpty(view.Loading, "loading…"), l.theme.Muted
	case len(l.views) > 0 && l.failures[l.view] != "":
		return "  " + l.failures[l.view], l.theme.Warning
	case len(l.filter) > 0:
		return "  no match", l.theme.Muted
	}
	return "  " + firstNonEmpty(view.Empty, "nothing here"), l.theme.Muted
}

// hint names the keys of the state the list is in, in as much detail as the
// terminal has room for.
func (l *list) hint(width int) string {
	if l.keyLine != "" {
		return cut(l.keyLine, width)
	}
	if l.anchor >= 0 {
		return fitHint(
			"↑/↓ extend · PgUp/PgDn page · Enter selects the range · Esc drops it",
			"↑/↓ extend · Enter selects · Esc drops the range", width)
	}
	enter := "Enter select"
	if l.rangeMode {
		enter = "Enter opens the range"
	}
	esc := "Esc cancel"
	if l.nested {
		esc = "Esc back"
	}
	long := []string{"↑/↓ move", "PgUp/PgDn page"}
	short := []string{"↑/↓ move"}
	if l.switchable() {
		long = append(long, "Tab view")
		short = append(short, "Tab view")
	}
	if l.multi {
		long = append(long, "Space mark")
		short = append(short, "Space mark")
	}
	long = append(long, "type to filter", "Ctrl-U clear", enter, esc)
	short = append(short, "type to filter", enter, esc)
	return fitHint(strings.Join(long, separator), strings.Join(short, separator), width)
}

// renderTitle draws the question, the subject in the theme's subject style,
// followed by the details of the row under the cursor or the summary of an
// open range.
func (l *list) renderTitle(width int) string {
	parts := titleParts(l.title, l.subject, l.theme)
	if l.anchor >= 0 {
		parts = append(parts, l.spanParts()...)
	} else if index := l.selected(); index >= 0 {
		parts = append(parts, l.detailParts(l.row(index))...)
	}
	return drawParts(parts, width, l.painter())
}

func (l *list) detailStyleOrDefault() theme.Style {
	if l.detailStyle != "" {
		return l.detailStyle
	}
	return l.theme.Detail
}

// detailParts is what the title line says about the row under the cursor.
func (l *list) detailParts(item Item) []styledText {
	if len(item.Details) == 0 {
		if item.Detail == "" {
			return nil
		}
		return []styledText{{" ", ""}, {item.Detail, l.detailStyleOrDefault()}}
	}
	var parts []styledText
	for _, field := range item.Details {
		if field.Text == "" {
			continue
		}
		parts = append(parts, styledText{separator, l.theme.Muted}, styledText{field.Text, field.Style})
	}
	return parts
}

// spanParts summarizes an open range after the title: how many rows it
// covers and, where both ends carry a Detail, the two ends.
func (l *list) spanParts() []styledText {
	size := l.spanSize()
	count := strconv.Itoa(size)
	if l.unit != "" {
		count += " " + l.unit
		if size != 1 {
			count += "s"
		}
	}
	parts := []styledText{{" " + count, l.theme.Muted}}
	first, last := l.selectedRange()
	if first < 0 {
		return parts
	}
	from, to := l.row(last).Detail, l.row(first).Detail
	if from == "" || to == "" {
		return parts
	}
	// Oldest first, the way a range is written: "from..to".
	style := l.detailStyleOrDefault()
	return append(parts, styledText{separator, l.theme.Muted}, styledText{from, style},
		styledText{"..", l.theme.Muted}, styledText{to, style})
}

// position is the position line as plain text.
func (l *list) position() string {
	var b strings.Builder
	for _, part := range l.positionParts() {
		b.WriteString(part.text)
	}
	return b.String()
}

// positionPart is a stretch of the position line. Active parts are what the
// user set, the view showing and the filter typed, and are not muted; style
// overrides the muted style of the others.
type positionPart struct {
	text   string
	active bool
	style  theme.Style
}

func (l *list) positionParts() []positionPart {
	count := fmt.Sprintf("%d of %d", l.cursor+1, len(l.matches))
	if len(l.matches) == 0 {
		count = fmt.Sprintf("0 of %d", len(l.items))
	}
	if l.switchable() || len(l.filter) > 0 {
		// Whatever follows the count keeps its place while the count changes
		// length.
		count = pad(count, l.countWidth())
	}
	parts := []positionPart{{text: count}}
	if l.switchable() {
		// A view an open range has set aside is not the one in force.
		inForce := l.anchor < 0 || !l.narrowed()
		for i := range l.views {
			label := l.viewLabel(i)
			if i == l.view && inForce {
				parts = append(parts, positionPart{text: separator}, positionPart{text: "[" + label + "]", active: true})
				continue
			}
			parts = append(parts, positionPart{text: separator + label})
		}
	}
	if l.loading() {
		parts = append(parts, positionPart{text: separator + firstNonEmpty(l.views[l.view].Loading, "loading…")})
	} else if len(l.views) > 0 && l.failures[l.view] != "" && len(l.items) > 0 {
		// With no rows at all the failure stands in their place instead.
		parts = append(parts, positionPart{text: separator}, positionPart{text: l.failures[l.view], style: l.theme.Warning})
	}
	if len(l.filter) > 0 {
		parts = append(parts, positionPart{text: separator + "filter: "}, positionPart{text: string(l.filter), active: true})
	}
	if len(l.marked) > 0 {
		parts = append(parts, positionPart{text: separator}, positionPart{text: fmt.Sprintf("%d marked", len(l.marked)), active: true})
	}
	if index := l.selected(); index >= 0 && l.row(index).Disabled {
		parts = append(parts, positionPart{text: separator + "nothing to do"})
	}
	return parts
}

// countWidth is room for the widest count the list can show.
func (l *list) countWidth() int {
	total := max(len(l.items), 999)
	return width(fmt.Sprintf("%d of %d", total, total))
}

// renderPosition draws the position line muted, except for the parts the
// user set.
func (l *list) renderPosition(n int) string {
	var b strings.Builder
	left := n
	for _, part := range l.positionParts() {
		if left <= 0 {
			break
		}
		text := cut(part.text, left)
		left -= width(text)
		style := l.theme.Muted
		switch {
		case part.active:
			style = ""
		case part.style != "":
			style = part.style
		}
		b.WriteString(l.styled(style, text))
	}
	return b.String()
}
