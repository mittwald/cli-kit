package pick

import (
	"fmt"
	"os"
	"strings"

	"github.com/mittwald/cli-kit/theme"
)

// Toggle is one switch: what it is called, the two words its positions stand
// for, and where it starts. On and Off are the caller's vocabulary, such as
// "enabled" and "disabled"; they default to "yes" and "no". Label, On and
// Off are shown as plain single-line text, as everything a prompt draws is.
type Toggle struct {
	Label string
	On    string
	Off   string
	Value bool
}

// ToggleOptions configures one set of switches. Title, Subject and Note are
// shown as plain single-line text: escape sequences and control characters
// in them are dropped, and a line break or a tab becomes a space.
type ToggleOptions struct {
	// Title is the question, with one %s where Subject goes.
	Title string
	// Subject is the thing the switches belong to.
	Subject string
	// Note is an optional line under the title.
	Note    string
	Toggles []Toggle
	// Initial is the switch the cursor starts on.
	Initial int
	// Nested names Esc "back" instead of "cancel" in the key line.
	Nested bool
}

// Toggles draws a set of switches and returns the position each was left in,
// in the order given. The arrows move the cursor, Space or Tab throws the
// switch under it, "a" turns all on and "n" all off, and Enter answers. A
// thrown switch shows what it was before. Esc returns ErrBack and Ctrl-C or
// Ctrl-D returns ErrInterrupted, without values.
func Toggles(in, out *os.File, opts ToggleOptions) ([]bool, error) {
	rows := make([]switchRow, len(opts.Toggles))
	for i, toggle := range opts.Toggles {
		rows[i] = switchRow{label: toggle.Label, on: toggle.On, off: toggle.Off, value: toggle.Value}
	}
	return runSwitches(in, out, switchOptions{
		title: opts.Title, subject: opts.Subject, note: opts.Note,
		rows: rows, initial: opts.Initial, nested: opts.Nested,
	})
}

// Box is one row of a checklist: what it is called, an optional note on what
// ticking it means, and whether it starts ticked. Label and Note are shown
// as plain single-line text, as everything a prompt draws is.
type Box struct {
	Label   string
	Note    string
	Checked bool
}

// ChecklistOptions configures one checklist. Title, Subject and Note are
// shown as plain single-line text: escape sequences and control characters
// in them are dropped, and a line break or a tab becomes a space.
type ChecklistOptions struct {
	// Title is the question, with one %s where Subject goes.
	Title string
	// Subject is the thing the checklist is about.
	Subject string
	// Note is an optional line under the title.
	Note  string
	Boxes []Box
	// Initial is the box the cursor starts on.
	Initial int
	// Nested names Esc "back" instead of "cancel" in the key line.
	Nested bool
}

// Checklist draws a list of boxes and returns which were left ticked, in the
// order given. It takes the same keys as Toggles, but draws the box in front
// of each label, with the label's note beside it, the way a form does.
func Checklist(in, out *os.File, opts ChecklistOptions) ([]bool, error) {
	rows := make([]switchRow, len(opts.Boxes))
	for i, box := range opts.Boxes {
		rows[i] = switchRow{label: box.Label, note: box.Note, value: box.Checked}
	}
	return runSwitches(in, out, switchOptions{
		title: opts.Title, subject: opts.Subject, note: opts.Note,
		rows: rows, initial: opts.Initial, nested: opts.Nested, checklist: true,
	})
}

// switchRow is one switch or box.
type switchRow struct {
	label, note string
	on, off     string
	value       bool
}

type switchOptions struct {
	title, subject, note string
	rows                 []switchRow
	initial              int
	nested               bool
	checklist            bool
}

func runSwitches(in, out *os.File, opts switchOptions) ([]bool, error) {
	if len(opts.rows) == 0 {
		return nil, ErrNoItems
	}
	session, err := openTerminal(in, out, true)
	if err != nil {
		return nil, err
	}
	defer session.close()

	state := newSwitches(opts, session.color)
	state.theme = session.theme
	if err := session.drive(state, 0); err != nil {
		return nil, err
	}
	return state.values(), nil
}

// switches is the state of a set of switches or a checklist. Pure, like the
// list: keys in through apply, lines out of render.
type switches struct {
	title, subject, note string
	rows                 []switchRow
	// initial is what each switch was when the prompt opened, which is what
	// a thrown switch says it changed from.
	initial    []bool
	labelWidth int
	cursor     int
	// top is the first row drawn and visible how many are, so the block
	// fits on the terminal; visible is all rows until resize says less.
	// frame is what the block keeps of the lines around the rows.
	top, visible int
	frame        frame
	nested       bool
	checklist    bool
	color        bool
	theme        theme.Theme
}

func newSwitches(opts switchOptions, color bool) *switches {
	s := &switches{
		title:     plainLine(opts.title),
		subject:   plainLine(opts.subject),
		note:      plainLine(opts.note),
		rows:      make([]switchRow, len(opts.rows)),
		initial:   make([]bool, len(opts.rows)),
		nested:    opts.nested,
		checklist: opts.checklist,
		color:     color,
		theme:     theme.Active(),
	}
	for i, row := range opts.rows {
		row.label, row.note = plainLine(row.label), plainLine(row.note)
		row.on, row.off = plainLine(row.on), plainLine(row.off)
		s.rows[i] = row
		s.initial[i] = row.value
		s.labelWidth = max(s.labelWidth, width(row.label))
	}
	s.resize(0, 0)
	if opts.initial > 0 && opts.initial < len(s.rows) {
		s.cursor = opts.initial
	}
	return s
}

func (s *switches) values() []bool {
	values := make([]bool, len(s.rows))
	for i, row := range s.rows {
		values[i] = row.value
	}
	return values
}

// resize fits the block to the terminal height, as the list does: a block
// taller than the terminal would scroll, and the in-place redraw would then
// repaint the wrong lines. Rows that do not fit scroll with the cursor, a
// position line saying where it is among them.
// A set of switches has no cap on its rows the way a list has, so the
// terminal alone decides.
func (s *switches) resize(_, height int) {
	s.frame = fitFrame(height, len(s.rows), s.note != "", false)
	s.visible = s.frame.rows
	s.scrollToCursor()
}

func (s *switches) scrollToCursor() {
	if s.cursor < s.top {
		s.top = s.cursor
	}
	if s.cursor >= s.top+s.visible {
		s.top = s.cursor - s.visible + 1
	}
	s.top = max(min(s.top, len(s.rows)-s.visible), 0)
}

func (s *switches) apply(k key) action {
	switch k.kind {
	case keyEnter:
		return actionSelect
	case keyBack:
		return actionBack
	case keyInterrupt:
		return actionInterrupt
	case keyUp:
		s.cursor = max(s.cursor-1, 0)
		s.scrollToCursor()
	case keyDown:
		s.cursor = min(s.cursor+1, len(s.rows)-1)
		s.scrollToCursor()
	case keyHome, keyPageUp:
		s.cursor = 0
		s.scrollToCursor()
	case keyEnd, keyPageDown:
		s.cursor = len(s.rows) - 1
		s.scrollToCursor()
	case keyTab:
		s.throw()
	case keyRune:
		switch k.rune {
		case ' ':
			s.throw()
		case 'k':
			return s.apply(key{kind: keyUp})
		case 'j':
			return s.apply(key{kind: keyDown})
		case 'a':
			s.setAll(true)
		case 'n':
			s.setAll(false)
		}
	}
	return actionNone
}

func (s *switches) throw() {
	s.rows[s.cursor].value = !s.rows[s.cursor].value
}

func (s *switches) setAll(on bool) {
	for i := range s.rows {
		s.rows[i].value = on
	}
}

func (s *switches) painter() painter { return painter{color: s.color, theme: s.theme} }

// hint names the keys, Esc as "back" in a nested prompt.
func (s *switches) hint(n int) string {
	esc := "Esc cancel"
	if s.nested {
		esc = "Esc back"
	}
	if s.checklist {
		return fitHint("↑/↓ move · Space/Tab tick · a all · n none · Enter confirm · "+esc,
			"↑/↓ · Space tick · Enter · Esc", n)
	}
	return fitHint("↑/↓ move · Space/Tab toggle · Enter confirm · "+esc,
		"↑/↓ · Space toggle · Enter · Esc", n)
}

// render is the block: the question, the rows, and the keys, in the shape of
// the list.
func (s *switches) render(n int) []string {
	if n <= 0 {
		n = 80
	}
	p := s.painter()
	lines := make([]string, 0, s.visible+6)
	if s.frame.title {
		lines = append(lines, drawParts(titleParts(s.title, s.subject, s.theme), n, p))
	}
	if s.frame.note {
		lines = append(lines, p.paint(s.theme.Muted, cut(s.note, n)))
	}
	if s.frame.gapAbove {
		lines = append(lines, "")
	}
	for i := s.top; i < min(s.top+s.visible, len(s.rows)); i++ {
		lines = append(lines, s.renderRow(i, n))
	}
	if s.frame.gapBelow {
		lines = append(lines, "")
	}
	if s.frame.position {
		lines = append(lines, p.paint(s.theme.Muted, cut(fmt.Sprintf("%d of %d", s.cursor+1, len(s.rows)), n)))
	}
	if s.frame.hint {
		lines = append(lines, p.paint(s.theme.Muted, s.hint(n)))
	}
	return lines
}

func (s *switches) renderRow(index, n int) string {
	row := s.rows[index]
	selected := index == s.cursor
	th := s.theme
	p := s.painter()

	var b strings.Builder
	left := n
	write := func(text string, style theme.Style) {
		if left <= 0 {
			return
		}
		text = cut(text, left)
		left -= width(text)
		b.WriteString(p.paint(style, text))
	}

	marker := strings.Repeat(" ", width(cursorMarker))
	name, qualifier := th.Subject, th.Qualifier
	if selected {
		marker = cursorMarker
		name, qualifier = th.Selected, th.SelectedQualifier
	}
	box := uncheckedBox
	if row.value {
		box = checkedBox
	}
	write(marker, name)

	if s.checklist {
		// The box comes first, so the column of ticks reads at a glance.
		boxStyle := qualifier
		if row.value {
			boxStyle = th.Action
			if selected {
				boxStyle = th.SelectedAction
			}
		}
		write(box+" ", boxStyle)
		if row.note == "" {
			write(row.label, name)
			return b.String()
		}
		write(pad(row.label, s.labelWidth), name)
		write(columnGap+row.note, qualifier)
		return b.String()
	}

	write(pad(row.label, s.labelWidth), name)
	write(columnGap, "")
	write(box+" "+switchWord(row, row.value), qualifier)
	if row.value != s.initial[index] {
		write(columnGap+"· was "+switchWord(row, s.initial[index]), th.Muted)
	}
	return b.String()
}

// switchWord is the word a switch stands for in one position, "yes" or "no"
// where the caller named none.
func switchWord(row switchRow, value bool) string {
	if value {
		return firstNonEmpty(row.on, "yes")
	}
	return firstNonEmpty(row.off, "no")
}
