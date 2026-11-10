package pick

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mittwald/cli-kit/theme"
)

const (
	// minVisible is the smallest row window the list draws; a terminal too
	// short for it scrolls rather than dropping the list.
	minVisible = 3
	// defaultVisible caps the rows drawn when the terminal height is unknown.
	defaultVisible = 15
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

	color   bool
	theme   theme.Theme
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
	loaded   []bool
	failures []string
	pending  bool

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
		title:              opts.Title,
		subject:            opts.Subject,
		note:               opts.Note,
		fallback:           opts.Title,
		color:              color,
		theme:              theme.Active(),
		typed:              opts.Typed,
		typedNote:          opts.TypedNote,
		multi:              opts.multi,
		marked:             map[int]bool{},
		cellStyles:         opts.CellStyles,
		paintCell:          opts.PaintCell,
		detailStyle:        opts.DetailStyle,
		priorities:         opts.ColumnPriority,
		truncation:         opts.ColumnTruncation,
		rangeMode:          opts.Range,
		unit:               opts.RangeUnit,
		anchor:             -1,
		keyLine:            opts.Hint,
		nested:             opts.Nested,
		dismissOnBackspace: opts.DismissOnBackspace,
	}
	switch {
	case opts.scoped():
		l.scoped = true
		l.views = slices.Clone(opts.Views)
	case len(opts.Views) > 1:
		l.views = opts.Views
	}
	l.loaded = make([]bool, len(l.views))
	l.failures = make([]string, len(l.views))
	if opts.View > 0 && opts.View < len(l.views) {
		l.view = opts.View
	}
	if lens := opts.Lens; lens != nil {
		l.filter = []rune(lens.Filter)
		for i, view := range l.views {
			if view.Name == lens.View {
				l.view = i
			}
		}
	}
	if l.scoped {
		l.title = firstNonEmpty(l.views[l.view].Title, l.fallback)
		l.setItems(l.views[l.view].Items)
		l.pending = l.needsLoad()
	} else {
		l.setItems(opts.Items)
	}
	l.visible = visibleRows(opts.MaxVisible, height, l.chrome())
	l.applyFilter()
	// Initial names an item, and the cursor stands among the matches: an item
	// the filter or view hides leaves the cursor on the first row shown.
	if opts.Initial > 0 {
		for i, index := range l.matches {
			if index == opts.Initial {
				l.cursor = i
				break
			}
		}
	}
	l.scrollToCursor()
	return l
}

// setItems makes items the rows on screen.
func (l *list) setItems(items []Item) {
	l.items = items
	l.given = len(items)
	l.fields = make([][]MatchField, len(items))
	for i, item := range items {
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
		view = l.views[l.view].Name
	}
	return Lens{Filter: string(l.filter), View: view}
}

// chrome is how many lines the block spends on something other than rows:
// the title, the blank line under it, the blank line under the rows, the
// position line, the key line, and the note where there is one.
func (l *list) chrome() int {
	if l.note == "" {
		return 5
	}
	return 6
}

// visibleRows leaves room for the lines around the rows plus one spare, so
// the whole block fits on screen: a block taller than the terminal would
// scroll, and the in-place redraw would then repaint the wrong lines.
func visibleRows(maxVisible, height, chrome int) int {
	rows := defaultVisible
	if height > 0 {
		rows = height - chrome - 1
	}
	if maxVisible > 0 && maxVisible < rows {
		rows = maxVisible
	}
	return max(rows, minVisible)
}

// resize re-derives the row window after a terminal size change.
func (l *list) resize(maxVisible, height int) {
	rows := visibleRows(maxVisible, height, l.chrome())
	if rows == l.visible {
		return
	}
	l.visible = rows
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
			l.anchor = -1
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
// cleared with it: a row hidden between the two ends would end up inside a
// range nobody could see.
func (l *list) openRange() {
	item := l.selected()
	l.anchor = l.cursor
	if len(l.filter) > 0 {
		l.setFilter(nil)
		l.anchor = max(slices.Index(l.matches, item), 0)
		l.cursor = l.anchor
		l.scrollToCursor()
	}
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
		l.setFilter(l.filter)
		return
	}
	key := ""
	if index := l.selected(); index >= 0 && index < len(l.items) {
		key = l.items[index].Key
	}
	l.view = (l.view + delta + n) % n
	l.pending = l.needsLoad()
	l.loadView(key)
}

// loadView makes the current view's own rows the ones on screen. The filter
// carries over, and the cursor lands on the row carrying key, or at the top.
func (l *list) loadView(key string) {
	view := l.views[l.view]
	l.title = firstNonEmpty(view.Title, l.fallback)
	l.setItems(view.Items)
	l.cursor, l.top = 0, 0
	l.applyFilter()
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

// needsLoad reports whether the current view still has to fetch its rows.
func (l *list) needsLoad() bool {
	return l.scoped && l.views[l.view].Load != nil && !l.loaded[l.view]
}

func (l *list) loadPending() bool { return l.pending }

// runPendingLoad fetches the current view's rows. The key loop runs it
// between two draws, so the view and its loading line are on screen while
// the fetch blocks.
func (l *list) runPendingLoad() {
	l.pending = false
	view := l.views[l.view]
	if view.Load == nil || l.loaded[l.view] {
		return
	}
	l.loaded[l.view] = true
	items, err := view.Load()
	if err != nil {
		// Whatever was gathered before the failure is still worth showing;
		// with nothing gathered, the rows the view already had stay.
		l.failures[l.view] = err.Error()
		if len(items) == 0 {
			return
		}
	}
	l.views[l.view].Items = items
	l.loadView("")
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
		// group the views know.
		if !l.scoped && len(l.views) > 0 && i < l.given && !l.views[l.view].shows(l.items[i]) {
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
// accepts it, and no row is named by it already.
func (l *list) offersTyped() bool {
	text := l.typedText()
	if l.typed == nil || text == "" || !l.typed(text) {
		return false
	}
	for _, item := range l.items {
		if len(item.Cells) > 0 && item.Cells[0] == text {
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
	lines := make([]string, 0, l.visible+l.chrome())
	lines = append(lines, l.renderTitle(width))
	if l.note != "" {
		lines = append(lines, l.styled(th.Muted, cut(l.note, width)))
	}
	lines = append(lines, "")
	end := min(l.top+l.visible, len(l.matches))
	for position := l.top; position < end; position++ {
		lines = append(lines, l.renderRowWith(position, width, widths))
	}
	if len(l.matches) == 0 {
		text, style := l.emptyLine()
		lines = append(lines, l.styled(style, cut(text, width)))
	}
	lines = append(lines, "")
	lines = append(lines, l.renderPosition(width))
	lines = append(lines, l.styled(th.Muted, l.hint(width)))
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
	case l.pending:
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
		for i, view := range l.views {
			label := firstNonEmpty(view.Name, strconv.Itoa(i+1))
			if i == l.view {
				parts = append(parts, positionPart{text: separator}, positionPart{text: "[" + label + "]", active: true})
				continue
			}
			parts = append(parts, positionPart{text: separator + label})
		}
	}
	if l.pending {
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
