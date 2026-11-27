package tui

import (
	"slices"
	"strings"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// formField is a labeled field of a form.
type formField struct {
	label string
	w     Widget
	hint  string
}

// Form lays fields out under each other, each with its label in a column
// on the left and, for a field whose Err reports an error, the error under
// it. Tab and Shift-Tab move between the fields, Ctrl-S submits.
type Form struct {
	fields  []formField
	buttons []*Button
	keymap  *Keymap

	// OnSubmit is called on Ctrl-S, and by a button that calls Submit,
	// when no field reports an error.
	OnSubmit func()
}

// NewForm returns an empty form.
func NewForm() *Form {
	f := &Form{keymap: NewKeymap()}
	f.keymap.Bind("submit", f.Submit, "ctrl+s").When(func() bool { return f.OnSubmit != nil }).InGroup("Form")
	return f
}

// Keymap returns the keys of the form.
func (f *Form) Keymap() *Keymap { return f.keymap }

// Add adds a field with its label.
func (f *Form) Add(label string, w Widget) *Form {
	f.fields = append(f.fields, formField{label: term.PlainLine(label), w: w})
	return f
}

// AddHint adds a field with its label and a muted line under it that says
// what it takes.
func (f *Form) AddHint(label string, w Widget, hint string) *Form {
	f.fields = append(f.fields, formField{label: term.PlainLine(label), w: w, hint: term.PlainLine(hint)})
	return f
}

// AddButtons adds buttons in a row at the bottom.
func (f *Form) AddButtons(buttons ...*Button) *Form {
	f.buttons = append(f.buttons, buttons...)
	return f
}

// Err returns the first error a field reports, nil when there is none.
func (f *Form) Err() error {
	for _, fd := range f.fields {
		if e, ok := fd.w.(interface{ Err() error }); ok && e.Err() != nil {
			return e.Err()
		}
	}
	return nil
}

// Submit calls OnSubmit unless a field reports an error.
func (f *Form) Submit() {
	if f.OnSubmit != nil && f.Err() == nil {
		f.OnSubmit()
	}
}

// Children returns the fields and the buttons.
func (f *Form) Children() []Widget {
	var out []Widget
	for _, fd := range f.fields {
		out = append(out, fd.w)
	}
	for _, b := range f.buttons {
		out = append(out, b)
	}
	return out
}

func (f *Form) labelWidth() int {
	w := 0
	for _, fd := range f.fields {
		w = max(w, textWidth(fd.label))
	}
	return w
}

// fieldHeight is how many rows a field takes, its error and hint
// included.
func fieldHeight(fd formField, width, height int) (field, total int) {
	field = 1
	if s, ok := fd.w.(Sizer); ok {
		_, field = s.Size(width, height)
		field = max(field, 1)
	}
	total = field
	if e, ok := fd.w.(interface{ Err() error }); ok && e.Err() != nil {
		total++
	}
	if fd.hint != "" {
		total++
	}
	return field, total
}

// Draw draws the fields, their labels, errors and hints, and the buttons.
func (f *Form) Draw(c Canvas) {
	th := c.Theme()
	lw := f.labelWidth()
	x := lw + 2
	if lw == 0 {
		x = 0
	}
	width := c.Width() - x
	y := 0
	for _, fd := range f.fields {
		fh, total := fieldHeight(fd, width, c.Height()-y)
		style := th.Muted
		if c.HasFocus(fd.w) {
			style = th.Title
		}
		c.Text(lw-textWidth(fd.label), y, fd.label, style)
		c.Sub(x, y, width, fh).Draw(fd.w)
		row := y + fh
		if e, ok := fd.w.(interface{ Err() error }); ok && e.Err() != nil {
			c.Text(x, row, term.PlainLine(e.Err().Error()), th.Error)
			row++
		}
		if fd.hint != "" {
			c.Text(x, row, fd.hint, th.Muted)
		}
		y += total + 1
	}
	if len(f.buttons) > 0 {
		bx := x
		for _, b := range f.buttons {
			bw, _ := b.Size(width, 1)
			c.Sub(bx, y, bw, 1).Draw(b)
			bx += bw + 2
		}
	}
}

// Size is the fields with their labels, a blank line between them, and the
// buttons.
func (f *Form) Size(width, height int) (int, int) {
	lw := f.labelWidth()
	inner := width - lw - 2
	w, h := 0, 0
	for _, fd := range f.fields {
		fw := inner
		if s, ok := fd.w.(Sizer); ok {
			fw, _ = s.Size(inner, height)
		}
		w = max(w, fw)
		_, total := fieldHeight(fd, inner, height)
		h += total + 1
	}
	if len(f.buttons) > 0 {
		bw := 0
		for _, b := range f.buttons {
			x, _ := b.Size(inner, 1)
			bw += x + 2
		}
		w = max(w, bw)
		h++
	} else if h > 0 {
		h--
	}
	return min(w+lw+2, width), min(h, height)
}

// Dialog is a body and a row of buttons under a title bar, for a layer
// pushed with App.Push.
type Dialog struct {
	title   string
	body    Widget
	buttons []*Button
	footer  string
	width   int
}

// NewDialog returns a dialog.
func NewDialog(title string, body Widget, buttons ...*Button) *Dialog {
	return &Dialog{title: term.PlainLine(title), body: body, buttons: buttons}
}

// SetFooter sets the muted text on the bottom edge, such as the keys.
func (d *Dialog) SetFooter(s string) *Dialog { d.footer = term.PlainLine(s); return d }

// SetWidth sets the width of the dialog in cells, where the body would
// otherwise decide.
func (d *Dialog) SetWidth(w int) *Dialog { d.width = w; return d }

// Children returns the body and the buttons.
func (d *Dialog) Children() []Widget {
	out := []Widget{d.body}
	for _, b := range d.buttons {
		out = append(out, b)
	}
	return out
}

// Draw draws the title bar, the body and the buttons, right-aligned.
func (d *Dialog) Draw(c Canvas) {
	th := c.Theme()
	var footer Line
	if d.footer != "" {
		footer = Text(d.footer, th.Muted)
	}
	// The row under the bar and the one at the bottom stay blank, as the
	// columns on either side do.
	inner := c.Card(Line{{Text: d.title}}, footer, true)
	inner = inner.Sub(2, 1, inner.Width()-4, inner.Height()-2)
	bodyH := inner.Height()
	if len(d.buttons) > 0 {
		bodyH -= 2
	}
	inner.Sub(0, 0, inner.Width(), bodyH).Draw(d.body)
	if len(d.buttons) == 0 {
		return
	}
	x := inner.Width()
	for _, b := range slices.Backward(d.buttons) {
		bw, _ := b.Size(inner.Width(), 1)
		x -= bw
		inner.Sub(x, inner.Height()-1, bw, 1).Draw(b)
		x -= 2
	}
}

// Size is the body, the buttons and the box.
func (d *Dialog) Size(width, height int) (int, int) {
	w, h := min(max(width/2, 40), width), 1
	inner := width - 4
	if d.width > 0 {
		// The body wraps at the width the dialog will have.
		inner = min(d.width, width) - 4
	}
	if s, ok := d.body.(Sizer); ok {
		w, h = s.Size(inner, height-5)
	}
	bw := 0
	for _, b := range d.buttons {
		x, _ := b.Size(width, 1)
		bw += x + 2
	}
	w = max(w, bw, textWidth(d.title)+4, textWidth(d.footer)+4)
	if d.width > 0 {
		w = d.width - 4
	}
	if len(d.buttons) > 0 {
		h += 2
	}
	return min(w+4, width), min(h+3, height)
}

// Confirm asks a yes/no question in a dialog: Enter on Yes or "y" answers
// yes, Esc or "n" no. onYes is called for yes, nothing for no.
func (a *App) Confirm(title, message string, onYes func()) {
	body := NewLabel(message, "").SetWrap(true)
	var outer *Keyed
	yes := NewButton("Yes", func() { a.Close(outer); onYes() })
	no := NewButton("No", func() { a.Close(outer) })
	d := NewDialog(title, body, yes, no).SetFooter("y yes · n no")
	d.width = min(max(textWidth(message)+4, 40), 72)
	outer = NewKeyed(d)
	outer.keymap.Bind("yes", yes.Press, "y")
	outer.keymap.Bind("no", no.Press, "n")
	a.Push(outer, LayerOptions{Focus: yes})
}

// Choose shows options in a layer to pick one from, filtering as you type
// after "/". onChoose is called with the index of the option picked.
func (a *App) Choose(title string, options []string, selected int, onChoose func(i int)) {
	type option struct {
		i    int
		text string
	}
	items := make([]option, len(options))
	w := textWidth(title) + 4
	for i, o := range options {
		items[i] = option{i, term.PlainLine(o)}
		w = max(w, textWidth(items[i].text)+6)
	}
	list := NewList(func(o option) []Span {
		mark := "  "
		if o.i == selected {
			mark = "• "
		}
		return []Span{{Text: mark + o.text}}
	})
	list.SetItems(items)
	list.SetCursor(selected)
	panel := NewPanel(title, list)
	list.OnSelect = func(o option) {
		a.Close(panel)
		onChoose(o.i)
	}
	a.Push(panel, LayerOptions{Width: w + 2, Height: min(len(options), 20) + 3, Focus: list})
}

// Prompt asks for a line of text in a dialog. Enter submits it, Esc
// cancels. onSubmit is called with the text.
func (a *App) Prompt(title, label, value string, onSubmit func(text string)) *Input {
	in := NewInput()
	in.SetValue(value)
	form := NewForm().Add(label, in)
	var d *Dialog
	submit := func() {
		if in.Err() != nil {
			return
		}
		a.Close(d)
		onSubmit(in.Value())
	}
	in.OnSubmit = func(string) { submit() }
	d = NewDialog(title, form, NewButton("OK", submit), NewButton("Cancel", func() { a.Close(d) })).
		SetFooter("enter ok · esc cancel")
	d.width = 64
	a.Push(d, LayerOptions{Focus: in})
	return in
}

// EditText edits text of several lines in a dialog: Ctrl-S saves, Esc
// cancels. onSave is called with the text.
func (a *App) EditText(title, value string, onSave func(text string)) *TextArea {
	ta := NewTextArea()
	ta.SetValue(value)
	var d *Dialog
	save := func() { a.Close(d); onSave(ta.Value()) }
	form := NewForm().Add("", &sized{Widget: ta, h: 12})
	form.OnSubmit = save
	d = NewDialog(title, form, NewButton("Save", save), NewButton("Cancel", func() { a.Close(d) })).
		SetFooter("ctrl+s save · esc cancel")
	d.width = 72
	a.Push(d, LayerOptions{Focus: ta})
	return ta
}

// sized gives a widget a fixed height as a Sizer.
type sized struct {
	Widget
	h int
}

func (s *sized) Children() []Widget { return []Widget{s.Widget} }

func (s *sized) Draw(c Canvas) { c.Draw(s.Widget) }

func (s *sized) Size(width, height int) (int, int) { return width, min(s.h, height) }

// MenuItem is an entry of a menu.
type MenuItem struct {
	// Key is the key that picks the item at once, such as "a"; optional.
	Key string
	// Label says what the item does.
	Label string
	// Action is what it does.
	Action func()
	// Disabled shows the item muted and keeps it from being picked.
	Disabled bool
	// Separator draws a line instead of an item.
	Separator bool
}

// menu is the layer Menu pushes.
type menu struct {
	app    *App
	items  []MenuItem
	cursor int
	top    int
	keymap *Keymap
}

// Menu shows actions to pick one from, as a context menu: ↑/↓ and Enter,
// or the key of an item. The menu closes before the action runs.
func (a *App) Menu(title string, items []MenuItem) {
	m := &menu{app: a, items: items, keymap: NewKeymap()}
	m.cursor = -1
	m.step(1)
	m.keymap.Bind("move", func() { m.step(-1) }, "up", "k").InGroup("Menu")
	m.keymap.Bind("move", func() { m.step(1) }, "down", "j").Hide().InGroup("Menu")
	m.keymap.Bind("pick", func() { m.pick(m.cursor) }, "enter").InGroup("Menu")
	for i, it := range items {
		if it.Key != "" && !it.Disabled && !it.Separator {
			m.keymap.Bind(it.Label, func() { m.pick(i) }, it.Key).Hide().InGroup("Menu")
		}
	}
	w := textWidth(title) + 4
	for _, it := range items {
		w = max(w, textWidth(it.Label)+textWidth(it.Key)+6)
	}
	panel := NewPanel(title, m)
	a.Push(panel, LayerOptions{Width: w + 2, Height: len(items) + 2, Focus: m})
}

func (m *menu) step(delta int) {
	for i := 1; i <= len(m.items); i++ {
		j := ((m.cursor+delta*i)%len(m.items) + len(m.items)) % len(m.items)
		if !m.items[j].Disabled && !m.items[j].Separator {
			m.cursor = j
			return
		}
	}
}

func (m *menu) pick(i int) {
	if i < 0 || i >= len(m.items) || m.items[i].Disabled || m.items[i].Separator {
		return
	}
	m.app.Pop()
	if m.items[i].Action != nil {
		m.items[i].Action()
	}
}

func (m *menu) Keymap() *Keymap    { return m.keymap }
func (m *menu) CanFocus() bool     { return true }
func (m *menu) HandleKey(Key) bool { return false }

func (m *menu) HandleMouse(ev Mouse) bool {
	if ev.Button == MouseLeft && ev.Action == MousePress && m.top+ev.Y < len(m.items) {
		m.pick(m.top + ev.Y)
		return true
	}
	return false
}

func (m *menu) Draw(c Canvas) {
	th := c.Theme()
	// Scroll so the item under the cursor is in view.
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+c.Height() {
		m.top = m.cursor - c.Height() + 1
	}
	m.top = min(max(m.top, 0), max(len(m.items)-c.Height(), 0))
	for y := 0; y < c.Height() && m.top+y < len(m.items); y++ {
		i := m.top + y
		it := m.items[i]
		if it.Separator {
			c.Text(0, y, strings.Repeat("─", c.Width()), th.Border)
			continue
		}
		style := th.Text
		if it.Disabled {
			style = th.Muted
		}
		if i == m.cursor {
			// The item keeps its own color on the cursor's background.
			style = theme.Join(th.Cursor, style, background(th.Cursor))
			c.WithStyle(th.Cursor).FillRow(0, y, "")
		}
		c.Text(1, y, it.Label, style)
		if it.Key != "" {
			k := KeyLabel(it.Key)
			c.Text(c.Width()-1-textWidth(k), y, k, theme.Join(style, th.Key))
		}
	}
}
