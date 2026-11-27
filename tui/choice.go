package tui

import (
	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// Toggle is a yes/no value: a checkbox, "[x] Label", or a switch,
// "Label  ON". Space or Enter flips it.
type Toggle struct {
	label    string
	on       bool
	isSwitch bool
	disabled bool
	keymap   *Keymap

	// OnChange is called with the new value after every flip.
	OnChange func(on bool)
}

// NewCheckbox returns a toggle drawn as a checkbox.
func NewCheckbox(label string, on bool) *Toggle { return newToggle(label, on, false) }

// NewSwitch returns a toggle drawn as a switch.
func NewSwitch(label string, on bool) *Toggle { return newToggle(label, on, true) }

func newToggle(label string, on, isSwitch bool) *Toggle {
	t := &Toggle{label: term.PlainLine(label), on: on, isSwitch: isSwitch, keymap: NewKeymap()}
	t.keymap.Bind("toggle", t.Flip, "space", "enter")
	return t
}

// Keymap returns the keys of the toggle.
func (t *Toggle) Keymap() *Keymap { return t.keymap }

// Value reports whether the toggle is on.
func (t *Toggle) Value() bool { return t.on }

// SetValue sets the value without calling OnChange.
func (t *Toggle) SetValue(on bool) { t.on = on }

// SetLabel sets the label.
func (t *Toggle) SetLabel(label string) { t.label = term.PlainLine(label) }

// SetDisabled keeps the toggle from taking the focus.
func (t *Toggle) SetDisabled(disabled bool) *Toggle { t.disabled = disabled; return t }

// Flip turns the toggle on or off and calls OnChange.
func (t *Toggle) Flip() {
	t.on = !t.on
	if t.OnChange != nil {
		t.OnChange(t.on)
	}
}

// CanFocus reports whether the toggle is enabled.
func (t *Toggle) CanFocus() bool { return !t.disabled }

// HandleKey takes no other keys.
func (t *Toggle) HandleKey(Key) bool { return false }

// HandleMouse flips the toggle on a click.
func (t *Toggle) HandleMouse(m Mouse) bool {
	if t.disabled {
		return false
	}
	if m.Button == MouseLeft && m.Action == MousePress {
		t.Flip()
		return true
	}
	return false
}

// Draw draws the toggle.
func (t *Toggle) Draw(c Canvas) {
	th := c.Theme()
	label := th.Text
	if c.Focused(t) {
		label = th.Cursor
	}
	if t.disabled {
		label = th.Muted
	}
	if t.isSwitch {
		x := c.Text(0, 0, t.label, label)
		if t.on {
			c.Text(x+1, 0, " ON ", theme.Join(th.On, theme.Reverse))
		} else {
			c.Text(x+1, 0, " OFF ", th.Off)
		}
		return
	}
	box, style := "[ ]", th.Off
	if t.on {
		box, style = "[x]", th.On
	}
	x := c.Text(0, 0, box, style)
	c.Text(x+1, 0, t.label, label)
}

// Size is the width of the label and the box, and one line.
func (t *Toggle) Size(width, _ int) (int, int) {
	return min(textWidth(t.label)+6, width), 1
}

// choices is what RadioGroup and CheckList share: options, a cursor and a
// window of them in view.
type choices struct {
	options    []string
	cursor     int
	top        int
	height     int
	horizontal bool
	disabled   bool
	keymap     *Keymap
}

func newChoices(options []string) choices {
	clean := make([]string, len(options))
	for i, o := range options {
		clean[i] = term.PlainLine(o)
	}
	return choices{options: clean, keymap: NewKeymap()}
}

func (c *choices) move(n int) {
	if len(c.options) == 0 {
		return
	}
	c.cursor = min(max(c.cursor+n, 0), len(c.options)-1)
}

func (c *choices) bindMoves() {
	k := c.keymap
	k.Bind("move", func() { c.move(-1) }, "up", "k").When(func() bool { return !c.horizontal }).InGroup("Choice")
	k.Bind("move", func() { c.move(1) }, "down", "j").When(func() bool { return !c.horizontal }).Hide().InGroup("Choice")
	k.Bind("move", func() { c.move(-1) }, "left", "h").When(func() bool { return c.horizontal }).InGroup("Choice")
	k.Bind("move", func() { c.move(1) }, "right", "l").When(func() bool { return c.horizontal }).Hide().InGroup("Choice")
	k.Bind("first", func() { c.cursor = 0 }, "home", "g").Hide().InGroup("Choice")
	k.Bind("last", func() { c.cursor = max(len(c.options)-1, 0) }, "end", "G").Hide().InGroup("Choice")
}

// draw draws the options with mark giving each its box.
func (c *choices) draw(cv Canvas, focused bool, mark func(i int) (string, theme.Style)) {
	th := cv.Theme()
	c.height = max(cv.Height(), 1)
	if c.horizontal {
		// Scroll the row so the option under the cursor is in view, and
		// no further than that, so options come back as room grows.
		span := func(from int) int {
			w := 0
			for _, o := range c.options[from : c.cursor+1] {
				w += textWidth(o) + 6
			}
			return w - 2
		}
		c.top = min(c.top, c.cursor)
		for c.top < c.cursor && span(c.top) > cv.Width() {
			c.top++
		}
		for c.top > 0 && span(c.top-1) <= cv.Width() {
			c.top--
		}
		x := 0
		for i := c.top; i < len(c.options); i++ {
			o := c.options[i]
			box, style := mark(i)
			label := th.Text
			if i == c.cursor && focused {
				label = th.Cursor
			}
			x += cv.Text(x, 0, box, style) + 1
			x += cv.Text(x, 0, o, label) + 2
		}
		return
	}
	if c.cursor < c.top {
		c.top = c.cursor
	}
	if c.cursor >= c.top+c.height {
		c.top = c.cursor - c.height + 1
	}
	c.top = min(c.top, max(len(c.options)-c.height, 0))
	for y := 0; y < c.height && c.top+y < len(c.options); y++ {
		i := c.top + y
		box, style := mark(i)
		label := th.Text
		if i == c.cursor && focused {
			label = th.Cursor
		}
		x := cv.Text(0, y, box, style) + 1
		cv.Sub(0, 0, cv.Width()-1, cv.Height()).Text(x, y, c.options[i], label)
	}
	cv.VScrollbar(cv.Width()-1, c.top, c.height, len(c.options))
}

func (c *choices) size(width, height int) (int, int) {
	if c.horizontal {
		w := 0
		for _, o := range c.options {
			w += textWidth(o) + 6
		}
		return min(w, width), 1
	}
	w := 0
	for _, o := range c.options {
		w = max(w, textWidth(o)+6)
	}
	return min(w, width), min(len(c.options), height)
}

func (c *choices) click(m Mouse) (int, bool) {
	if m.Button != MouseLeft || m.Action != MousePress {
		return 0, false
	}
	if c.horizontal {
		x := 0
		for i := c.top; i < len(c.options); i++ {
			x += textWidth(c.options[i]) + 6
			if m.X < x {
				return i, true
			}
		}
		return 0, false
	}
	i := c.top + m.Y
	return i, i < len(c.options)
}

// RadioGroup chooses one of several options, as radio buttons in a column
// or a row. Space or Enter chooses the option under the cursor.
type RadioGroup struct {
	choices
	selected int

	// OnChange is called with the index of the option chosen.
	OnChange func(i int)
}

// NewRadioGroup returns a radio group with the option of index selected
// chosen; -1 for none.
func NewRadioGroup(options []string, selected int) *RadioGroup {
	r := &RadioGroup{choices: newChoices(options), selected: selected}
	r.cursor = max(selected, 0)
	r.bindMoves()
	r.keymap.Bind("choose", func() { r.Select(r.cursor) }, "space", "enter").InGroup("Choice")
	return r
}

// SetHorizontal lays the options out in a row.
func (r *RadioGroup) SetHorizontal(h bool) *RadioGroup { r.horizontal = h; return r }

// SetDisabled keeps the group from taking the focus.
func (r *RadioGroup) SetDisabled(disabled bool) *RadioGroup { r.disabled = disabled; return r }

// Keymap returns the keys of the group.
func (r *RadioGroup) Keymap() *Keymap { return r.keymap }

// Selected returns the index of the option chosen, -1 for none.
func (r *RadioGroup) Selected() int { return r.selected }

// Value returns the option chosen, "" for none.
func (r *RadioGroup) Value() string {
	if r.selected < 0 || r.selected >= len(r.options) {
		return ""
	}
	return r.options[r.selected]
}

// Select chooses the option of index i and calls OnChange.
func (r *RadioGroup) Select(i int) {
	if i < 0 || i >= len(r.options) {
		return
	}
	r.selected, r.cursor = i, i
	if r.OnChange != nil {
		r.OnChange(i)
	}
}

// SetSelected chooses the option of index i without calling OnChange.
func (r *RadioGroup) SetSelected(i int) { r.selected, r.cursor = i, max(i, 0) }

// CanFocus reports whether the group is enabled.
func (r *RadioGroup) CanFocus() bool { return !r.disabled && len(r.options) > 0 }

// HandleKey takes no other keys.
func (r *RadioGroup) HandleKey(Key) bool { return false }

// HandleMouse chooses the option clicked.
func (r *RadioGroup) HandleMouse(m Mouse) bool {
	if r.disabled {
		return false
	}
	i, ok := r.click(m)
	if ok {
		r.Select(i)
	}
	return ok
}

// Draw draws the options.
func (r *RadioGroup) Draw(c Canvas) {
	th := c.Theme()
	r.draw(c, c.Focused(r), func(i int) (string, theme.Style) {
		if i == r.selected {
			return "(•)", th.On
		}
		return "( )", th.Off
	})
}

// Size is the options laid out.
func (r *RadioGroup) Size(width, height int) (int, int) { return r.size(width, height) }

// CheckList chooses any number of several options, as checkboxes. Space or
// Enter checks the option under the cursor, "a" checks all, "n" none.
type CheckList struct {
	choices
	checked []bool

	// OnChange is called with the index of the option checked or
	// unchecked; with -1 after "a" and "n".
	OnChange func(i int)
}

// NewCheckList returns a check list with nothing checked.
func NewCheckList(options []string) *CheckList {
	l := &CheckList{choices: newChoices(options), checked: make([]bool, len(options))}
	l.bindMoves()
	l.keymap.Bind("check", func() { l.Toggle(l.cursor) }, "space", "enter").InGroup("Choice")
	l.keymap.Bind("all", func() { l.setAll(true) }, "a").InGroup("Choice")
	l.keymap.Bind("none", func() { l.setAll(false) }, "n").InGroup("Choice")
	return l
}

// SetHorizontal lays the options out in a row.
func (l *CheckList) SetHorizontal(h bool) *CheckList { l.horizontal = h; return l }

// SetDisabled keeps the list from taking the focus.
func (l *CheckList) SetDisabled(disabled bool) *CheckList { l.disabled = disabled; return l }

// Keymap returns the keys of the list.
func (l *CheckList) Keymap() *Keymap { return l.keymap }

// Checked reports which options are checked.
func (l *CheckList) Checked() []bool { return append([]bool(nil), l.checked...) }

// Values returns the options checked.
func (l *CheckList) Values() []string {
	var out []string
	for i, on := range l.checked {
		if on {
			out = append(out, l.options[i])
		}
	}
	return out
}

// SetChecked checks or unchecks the option of index i without calling
// OnChange.
func (l *CheckList) SetChecked(i int, on bool) {
	if i >= 0 && i < len(l.checked) {
		l.checked[i] = on
	}
}

// Toggle checks or unchecks the option of index i and calls OnChange.
func (l *CheckList) Toggle(i int) {
	if i < 0 || i >= len(l.checked) {
		return
	}
	l.checked[i] = !l.checked[i]
	l.cursor = i
	if l.OnChange != nil {
		l.OnChange(i)
	}
}

func (l *CheckList) setAll(on bool) {
	for i := range l.checked {
		l.checked[i] = on
	}
	if l.OnChange != nil {
		l.OnChange(-1)
	}
}

// CanFocus reports whether the list is enabled.
func (l *CheckList) CanFocus() bool { return !l.disabled && len(l.options) > 0 }

// HandleKey takes no other keys.
func (l *CheckList) HandleKey(Key) bool { return false }

// HandleMouse checks the option clicked.
func (l *CheckList) HandleMouse(m Mouse) bool {
	if l.disabled {
		return false
	}
	i, ok := l.click(m)
	if ok {
		l.Toggle(i)
	}
	return ok
}

// Draw draws the options.
func (l *CheckList) Draw(c Canvas) {
	th := c.Theme()
	l.draw(c, c.Focused(l), func(i int) (string, theme.Style) {
		if l.checked[i] {
			return "[x]", th.On
		}
		return "[ ]", th.Off
	})
}

// Size is the options laid out.
func (l *CheckList) Size(width, height int) (int, int) { return l.size(width, height) }

// Button does something when pressed with Enter, Space or a click.
type Button struct {
	label    string
	disabled bool
	keymap   *Keymap

	// OnPress is called when the button is pressed.
	OnPress func()
}

// NewButton returns a button.
func NewButton(label string, onPress func()) *Button {
	b := &Button{label: term.PlainLine(label), OnPress: onPress, keymap: NewKeymap()}
	b.keymap.Bind("press", b.Press, "enter", "space")
	return b
}

// Keymap returns the keys of the button.
func (b *Button) Keymap() *Keymap { return b.keymap }

// SetDisabled keeps the button from taking the focus.
func (b *Button) SetDisabled(disabled bool) *Button { b.disabled = disabled; return b }

// SetLabel sets the label.
func (b *Button) SetLabel(label string) { b.label = term.PlainLine(label) }

// Press calls OnPress.
func (b *Button) Press() {
	if b.OnPress != nil && !b.disabled {
		b.OnPress()
	}
}

// CanFocus reports whether the button is enabled.
func (b *Button) CanFocus() bool { return !b.disabled }

// HandleKey takes no other keys.
func (b *Button) HandleKey(Key) bool { return false }

// HandleMouse presses the button on a click.
func (b *Button) HandleMouse(m Mouse) bool {
	if b.disabled {
		return false
	}
	if m.Button == MouseLeft && m.Action == MousePress {
		b.Press()
		return true
	}
	return false
}

// Draw draws the button.
func (b *Button) Draw(c Canvas) {
	th := c.Theme()
	style := th.BadgeBlurred
	switch {
	case b.disabled:
		style = th.Muted
	case c.Focused(b):
		style = th.Badge
	}
	c.Text(0, 0, "  "+b.label+"  ", style)
}

// Size is the width of the label and the padding around it, and one line.
func (b *Button) Size(width, _ int) (int, int) { return min(textWidth(b.label)+4, width), 1 }

// Select is a field that shows one of several options and, on Enter or
// Space, opens a list to choose another from, which filters as you type.
type Select struct {
	options  []string
	selected int
	disabled bool
	app      *App
	keymap   *Keymap
	title    string

	// OnChange is called with the index of the option chosen.
	OnChange func(i int)
}

// NewSelect returns a select with the option of index selected shown; -1
// for none.
func NewSelect(title string, options []string, selected int) *Select {
	s := &Select{title: term.PlainLine(title), selected: selected, keymap: NewKeymap()}
	for _, o := range options {
		s.options = append(s.options, term.PlainLine(o))
	}
	s.keymap.Bind("choose", s.Open, "enter", "space")
	return s
}

// Keymap returns the keys of the select.
func (s *Select) Keymap() *Keymap { return s.keymap }

// SetDisabled keeps the select from taking the focus.
func (s *Select) SetDisabled(disabled bool) *Select { s.disabled = disabled; return s }

// Selected returns the index of the option shown, -1 for none.
func (s *Select) Selected() int { return s.selected }

// Value returns the option shown, "" for none.
func (s *Select) Value() string {
	if s.selected < 0 || s.selected >= len(s.options) {
		return ""
	}
	return s.options[s.selected]
}

// SetSelected shows the option of index i without calling OnChange.
func (s *Select) SetSelected(i int) { s.selected = i }

// Open opens the list of options in a layer.
func (s *Select) Open() {
	if s.app == nil || s.disabled {
		return
	}
	s.app.Choose(s.title, s.options, s.selected, func(i int) {
		s.selected = i
		if s.OnChange != nil {
			s.OnChange(i)
		}
	})
}

// CanFocus reports whether the select is enabled.
func (s *Select) CanFocus() bool { return !s.disabled }

// HandleKey takes no other keys.
func (s *Select) HandleKey(Key) bool { return false }

// HandleMouse opens the list on a click.
func (s *Select) HandleMouse(m Mouse) bool {
	if s.disabled {
		return false
	}
	if m.Button == MouseLeft && m.Action == MousePress {
		s.Open()
		return true
	}
	return false
}

// Draw draws the option shown and an arrow.
func (s *Select) Draw(c Canvas) {
	s.app = c.App()
	th := c.Theme()
	style := th.Field
	if c.Focused(s) {
		style = th.FieldFocused
	}
	c.FillRow(0, 0, style)
	value := s.Value()
	if value == "" {
		c.Text(0, 0, "—", theme.Join(style, th.Muted))
	} else {
		c.Text(0, 0, truncate(value, c.Width()-2), style)
	}
	c.Text(c.Width()-1, 0, "▾", style)
}

// Size is the widest option and the arrow, and one line.
func (s *Select) Size(width, _ int) (int, int) {
	w := 3
	for _, o := range s.options {
		w = max(w, textWidth(o)+2)
	}
	return min(w, width), 1
}
