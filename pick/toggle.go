package pick

import (
	"os"
	"strings"

	"github.com/mittwald/cli-kit/theme"
)

// Toggle is one switch: what it is called, the two words its positions stand
// for, and where it starts. On and Off are the caller's vocabulary, such as
// "enabled" and "disabled"; they default to "yes" and "no".
type Toggle struct {
	Label string
	On    string
	Off   string
	Value bool
}

// ToggleOptions configures one set of switches.
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
// ticking it means, and whether it starts ticked.
type Box struct {
	Label   string
	Note    string
	Checked bool
}

// ChecklistOptions configures one checklist.
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
	nested     bool
	checklist  bool
	color      bool
	theme      theme.Theme
}

func newSwitches(opts switchOptions, color bool) *switches {
	s := &switches{
		title:     opts.title,
		subject:   opts.subject,
		note:      opts.note,
		rows:      append([]switchRow(nil), opts.rows...),
		initial:   make([]bool, len(opts.rows)),
		nested:    opts.nested,
		checklist: opts.checklist,
		color:     color,
		theme:     theme.Active(),
	}
	for i, row := range opts.rows {
		s.initial[i] = row.value
		s.labelWidth = max(s.labelWidth, width(row.label))
	}
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

// resize is what makes switches a screenState; the set is short enough that
// there is nothing to refit.
func (s *switches) resize(int, int) {}

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
	case keyDown:
		s.cursor = min(s.cursor+1, len(s.rows)-1)
	case keyHome, keyPageUp:
		s.cursor = 0
	case keyEnd, keyPageDown:
		s.cursor = len(s.rows) - 1
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
	lines := make([]string, 0, len(s.rows)+6)
	lines = append(lines, drawParts(titleParts(s.title, s.subject, s.theme), n, p))
	if s.note != "" {
		lines = append(lines, p.paint(s.theme.Muted, cut(s.note, n)))
	}
	lines = append(lines, "")
	for i := range s.rows {
		lines = append(lines, s.renderRow(i, n))
	}
	lines = append(lines, "")
	lines = append(lines, p.paint(s.theme.Muted, s.hint(n)))
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
