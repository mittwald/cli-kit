package pick

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mittwald/cli-kit/theme"
)

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
		lines = append(lines, l.styled(firstStyle(l.hintStyle, th.Muted), l.hint(width)))
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
		return "  " + firstNonEmpty(view.Loading, "loading…"), firstStyle(l.emptyStyle, l.theme.Muted)
	case len(l.views) > 0 && l.failures[l.view] != "":
		return "  " + l.failures[l.view], l.theme.Warning
	case len(l.filter) > 0:
		return "  no match", firstStyle(l.emptyStyle, l.theme.Muted)
	}
	return "  " + firstNonEmpty(view.Empty, "nothing here"), firstStyle(l.emptyStyle, l.theme.Muted)
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
	esc := firstNonEmpty(l.keys.Cancel, "Esc cancel")
	if l.nested {
		esc = firstNonEmpty(l.keys.Back, "Esc back")
	}
	view := firstNonEmpty(l.keys.View, "Tab view")
	viewShort := firstNonEmpty(l.keys.ViewShort, view)
	long := []string{"↑/↓ move", "PgUp/PgDn page"}
	short := []string{"↑/↓ move"}
	if l.switchable() && !l.keys.ViewLast {
		long = append(long, view)
		short = append(short, viewShort)
	}
	if l.multi {
		long = append(long, "Space mark")
		short = append(short, "Space mark")
	}
	long = append(long, "type to filter", "Ctrl-U clear", enter, esc)
	short = append(short, "type to filter", enter, esc)
	if l.switchable() && l.keys.ViewLast {
		long = append(long, view)
		short = append(short, viewShort)
	}
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
		return []styledText{{" ", ""}, l.paintedPart(item.Detail, l.detailStyleOrDefault(), l.paintDetail)}
	}
	var parts []styledText
	for _, field := range item.Details {
		if field.Text == "" {
			continue
		}
		parts = append(parts, styledText{separator, l.theme.Muted}, l.paintedPart(field.Text, field.Style, field.Paint))
	}
	return parts
}

// paintedPart is text in base, or as paint paints it where color is on and
// paint only adds colors to it.
func (l *list) paintedPart(text string, base theme.Style, paint func(string, theme.Style) string) styledText {
	if l.color && paint != nil && text != "" {
		if painted := paint(text, base); paintedFrom(painted, text) {
			return styledText{painted, ""}
		}
	}
	return styledText{text, base}
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
	return append(parts, styledText{separator, l.theme.Muted}, l.paintedPart(from, style, l.paintDetail),
		styledText{"..", l.theme.Muted}, l.paintedPart(to, style, l.paintDetail))
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
// overrides the muted style, or the terminal's color of an active part.
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
				parts = append(parts, positionPart{text: separator}, positionPart{text: "[" + label + "]", active: true, style: l.activeView})
				continue
			}
			if l.hintStyle == "" {
				parts = append(parts, positionPart{text: separator + label})
				continue
			}
			parts = append(parts, positionPart{text: separator}, positionPart{text: label, style: l.hintStyle})
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
		case part.style != "":
			style = part.style
		case part.active:
			style = ""
		}
		b.WriteString(l.styled(style, text))
	}
	return b.String()
}

// firstStyle returns style, or fallback when style is empty.
func firstStyle(style, fallback theme.Style) theme.Style {
	if style != "" {
		return style
	}
	return fallback
}
