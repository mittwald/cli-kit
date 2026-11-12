package pick

import (
	"strings"

	"github.com/mittwald/cli-kit/theme"
)

const (
	// minColumnWidth is how narrow a shrunken column may get before another
	// one is shortened instead: below this nothing readable is left.
	minColumnWidth = 8
	// stubColumnWidth is the floor of a PriorityLow column. It is shortened
	// first, so without a wider floor it would end up the narrowest thing on
	// the row.
	stubColumnWidth = 35
)

// renderRowWith draws one row: the cursor marker, the boxes of a list that
// marks rows, the cells padded to the column widths, and the action. A cell
// is only padded while something follows it on the row.
func (l *list) renderRowWith(position, n int, widths []int) string {
	th := l.theme
	index := l.matches[position]
	item := l.row(index)
	selected := position == l.cursor
	// Every row of an open range is lit, so the span reads as one block; the
	// marker stays on the row the keys move.
	lit := selected || l.inSpan(position)

	var b strings.Builder
	left := n
	write := func(text string, style theme.Style) {
		if left <= 0 {
			return
		}
		text = cut(text, left)
		left -= width(text)
		b.WriteString(l.styled(style, text))
	}
	writePainted := func(text string) {
		if left <= 0 {
			return
		}
		text = cut(text, left)
		left -= width(text)
		b.WriteString(text)
	}

	marker := strings.Repeat(" ", width(cursorMarker))
	if selected {
		marker = cursorMarker
	}
	nameStyle, mutedStyle := l.nameStyle(lit), l.mutedStyle(lit)
	actionStyle := th.Action
	if lit {
		actionStyle = th.SelectedAction
	}
	receded := false
	if item.Disabled {
		// A row there is nothing to do with recedes as a whole; the cursor
		// still shows on it, a step back from a choice.
		nameStyle, mutedStyle = th.Muted, th.Muted
		if lit {
			nameStyle, mutedStyle = th.SelectedQualifier, th.SelectedQualifier
		}
		receded = true
	}
	if item.Muted && !lit {
		nameStyle, mutedStyle, actionStyle = th.Muted, th.Muted, th.Muted
		receded = true
	}

	write(marker, nameStyle)
	if l.multi && !l.boxes {
		write(strings.Repeat(" ", width(uncheckedBox+" ")), "")
	}
	if l.boxes {
		box, boxStyle := uncheckedBox, mutedStyle
		if l.marked[index] {
			box, boxStyle = checkedBox, th.Action
			if lit {
				boxStyle = th.SelectedAction
			}
		}
		write(box+" ", boxStyle)
	}
	last := lastFilledCell(item.Cells)
	if item.Action != "" {
		last = len(widths) - 1
	}
	for i := 0; i <= last && i < len(widths); i++ {
		if i > 0 {
			write(gapBefore(widths, i), "")
		}
		cell := ""
		if i < len(item.Cells) {
			cell = item.Cells[i]
		}
		padded := i < last || item.Action != ""
		if i == 0 && item.Flag != "" {
			l.writeFlagged(write, cell, item.Flag, nameStyle, mutedStyle, padded, widths[0])
			continue
		}
		text := l.cutCell(cell, i, widths[i])
		if !receded && l.color {
			if l.paintCell != nil {
				if painted, ok := l.paintCell(i, text, lit); ok {
					if padded {
						painted = pad(painted, widths[i])
					}
					writePainted(painted)
					continue
				}
			}
			if style := l.cellStyle(item, i); style != "" {
				if lit {
					style = theme.Join(theme.Bold, style)
				}
				if padded {
					text = pad(text, widths[i])
				}
				write(text, style)
				continue
			}
		}
		if padded {
			text = pad(text, widths[i])
		}
		if i > 0 {
			// Every column past the first qualifies the row rather than
			// naming it, so it recedes.
			write(text, mutedStyle)
			continue
		}
		l.writeName(write, text, nameStyle, mutedStyle)
	}
	if item.Action != "" {
		write(columnGap, "")
		write(item.Action, actionStyle)
	}
	return b.String()
}

// cutCell shortens a cell to its column the way the column asks for.
func (l *list) cutCell(cell string, column, n int) string {
	if column < len(l.truncation) && l.truncation[column] == TruncateMiddle {
		return cutMiddle(cell, n)
	}
	return cut(cell, n)
}

// cellStyle is the row's own style for a column when it sets one, else the
// column's style.
func (l *list) cellStyle(item Item, column int) theme.Style {
	if column < len(item.CellStyles) && item.CellStyles[column] != "" {
		return item.CellStyles[column]
	}
	if column < len(l.cellStyles) {
		return l.cellStyles[column]
	}
	return ""
}

// flagged is the first cell as it is drawn, flag and all.
func flagged(cell, flag string) string {
	if flag == "" {
		return cell
	}
	return cell + " " + flag
}

// writeFlagged draws a first cell that carries a flag: the name, then the
// flag in the theme's flag style, then the padding the column wants.
//
// In a column too narrow for both, the name comes first: it keeps
// minColumnWidth cells, or all it needs when that is less, and the flag is
// shortened to what is left, or left out when nothing is. A row is told
// apart by its name, and a flag without one says nothing.
func (l *list) writeFlagged(write func(string, theme.Style), cell, flag string, nameStyle, mutedStyle theme.Style, padded bool, columnWidth int) {
	room := max(columnWidth-width(" "+flag), min(width(cell), minColumnWidth, columnWidth))
	name := l.cutCell(cell, 0, room)
	used := width(name)
	l.writeName(write, name, nameStyle, mutedStyle)
	if left := columnWidth - used - 1; left > 0 {
		mark := cut(flag, left)
		write(" ", "")
		write(mark, l.theme.Flag)
		used += 1 + width(mark)
	}
	if gap := columnWidth - used; padded && gap > 0 {
		write(strings.Repeat(" ", gap), "")
	}
}

// writeName draws a first cell: the name carries the weight, whatever it says
// in parentheses recedes.
func (l *list) writeName(write func(string, theme.Style), text string, nameStyle, mutedStyle theme.Style) {
	for _, part := range parenSpans(text) {
		if part.paren {
			write(part.text, mutedStyle)
			continue
		}
		write(part.text, nameStyle)
	}
}

// nameStyle and mutedStyle are the two weights of a row, in the color the
// cursor gives them.
func (l *list) nameStyle(selected bool) theme.Style {
	if selected {
		return l.theme.Selected
	}
	return l.theme.Subject
}

func (l *list) mutedStyle(selected bool) theme.Style {
	if selected {
		return l.theme.SelectedQualifier
	}
	return l.theme.Qualifier
}

// lastFilledCell is the last cell of a row that holds anything, -1 for a row
// that holds nothing.
func lastFilledCell(cells []string) int {
	last := -1
	for i, cell := range cells {
		if cell != "" {
			last = i
		}
	}
	return last
}

// gapBefore is the space between column i-1 and column i: a single space
// after a one-cell marker column, the normal gap everywhere else. A marker
// belongs to the row it marks.
func gapBefore(widths []int, column int) string {
	if column > 0 && column-1 < len(widths) && widths[column-1] == 1 {
		return " "
	}
	return columnGap
}

// fittedWidths are the column widths shrunk to fit a row into n cells.
func (l *list) fittedWidths(n int) []int {
	overhead := width(cursorMarker)
	if l.multi {
		overhead += width(uncheckedBox + " ")
	}
	for i := 1; i < len(l.widths); i++ {
		overhead += len(gapBefore(l.widths, i))
	}
	action := 0
	for _, index := range l.matches {
		action = max(action, width(l.row(index).Action))
	}
	if action > 0 {
		overhead += len(columnGap) + action
	}
	return fitWidths(l.widths, l.priorities, n-overhead)
}

// fitWidths shrinks columns until their sum fits room. The least important
// column gives up its width first, and columns of equal importance level
// down together rather than one collapsing next to another that kept
// everything. Columns already at or below their floor keep their width.
func fitWidths(widths, priorities []int, room int) []int {
	if len(widths) == 0 {
		return nil
	}
	fitted := append([]int(nil), widths...)
	for {
		total := 0
		for _, w := range fitted {
			total += w
		}
		if total <= room {
			return fitted
		}
		target := shrinkTarget(fitted, priorities)
		if target < 0 {
			// Nothing left to give; cutting the line takes over.
			return fitted
		}
		reduce := total - room
		if floor := shrinkFloor(fitted, priorities, target); fitted[target]-reduce < floor {
			reduce = fitted[target] - floor
		}
		fitted[target] -= max(reduce, 1)
	}
}

// columnMin is how far a column may be shrunk.
func columnMin(priorities []int, column int) int {
	if columnPriority(priorities, column) == PriorityLow {
		return stubColumnWidth
	}
	return minColumnWidth
}

// shrinkTarget picks the column to shorten next: the least important one
// that still has width to give, the widest among those of equal importance.
func shrinkTarget(fitted, priorities []int) int {
	target := -1
	for i, w := range fitted {
		if w <= columnMin(priorities, i) {
			continue
		}
		if target < 0 {
			target = i
			continue
		}
		switch {
		case columnPriority(priorities, i) < columnPriority(priorities, target):
			target = i
		case columnPriority(priorities, i) == columnPriority(priorities, target) && w > fitted[target]:
			target = i
		}
	}
	return target
}

// shrinkFloor is how far the target may fall in one step: down to the widest
// other column of the same importance, so a pair levels a cell at a time, and
// never below the target's own floor.
func shrinkFloor(fitted, priorities []int, target int) int {
	floor := columnMin(priorities, target)
	for i, w := range fitted {
		if i == target || columnPriority(priorities, i) != columnPriority(priorities, target) {
			continue
		}
		if w <= fitted[target] && w > floor {
			floor = w
		}
	}
	return floor
}

// columnPriority is a column's declared importance, PriorityNormal for every
// column the caller did not rank.
func columnPriority(priorities []int, column int) int {
	if column < len(priorities) {
		return priorities[column]
	}
	return PriorityNormal
}
