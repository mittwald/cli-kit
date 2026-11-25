package tui

import (
	"strconv"

	"github.com/mittwald/cli-kit/theme"
)

// sizeKind says how a child of a Box is sized.
type sizeKind int

const (
	sizeGrow sizeKind = iota
	sizeFixed
	sizeFit
)

// Item is a child of a Box and the way it is sized.
type Item struct {
	w    Widget
	kind sizeKind
	n    int
}

// Grow sizes w to share the room the other children leave with the other
// growing children, equally.
func Grow(w Widget) Item { return Item{w: w, kind: sizeGrow, n: 1} }

// GrowBy is Grow with a weight: a child of weight 2 gets twice the room of
// one of weight 1.
func GrowBy(weight int, w Widget) Item { return Item{w: w, kind: sizeGrow, n: max(weight, 1)} }

// Fixed sizes w to n cells: rows in a column, columns in a row.
func Fixed(n int, w Widget) Item { return Item{w: w, kind: sizeFixed, n: max(n, 0)} }

// Fit sizes w to what it asks for as a [Sizer]; a widget that is none gets
// one row in a column and its text width in a row, or 0.
func Fit(w Widget) Item { return Item{w: w, kind: sizeFit} }

// Box lays its children out in a column, one below the other, or in a row,
// side by side.
type Box struct {
	row    bool
	items  []Item
	hidden []Widget
	gap    int
}

// VBox returns a box that stacks its children from top to bottom.
func VBox(items ...Item) *Box { return &Box{items: items} }

// HBox returns a box that puts its children side by side from left to
// right.
func HBox(items ...Item) *Box { return &Box{row: true, items: items} }

// Gap sets the number of blank cells between children and returns the box.
func (b *Box) Gap(n int) *Box { b.gap = max(n, 0); return b }

// Add appends children.
func (b *Box) Add(items ...Item) { b.items = append(b.items, items...) }

// SetHidden hides a child, or shows it again: a hidden child takes no room,
// is not drawn and cannot have the focus.
func (b *Box) SetHidden(w Widget, hidden bool) {
	kept := b.hidden[:0]
	for _, h := range b.hidden {
		if !same(h, w) {
			kept = append(kept, h)
		}
	}
	b.hidden = kept
	if hidden {
		b.hidden = append(b.hidden, w)
	}
}

// Hidden reports whether a child is hidden.
func (b *Box) Hidden(w Widget) bool { return contains(b.hidden, w) }

// Children returns the children shown.
func (b *Box) Children() []Widget {
	var out []Widget
	for _, it := range b.visible() {
		out = append(out, it.w)
	}
	return out
}

func (b *Box) visible() []Item {
	var out []Item
	for _, it := range b.items {
		if it.w != nil && !contains(b.hidden, it.w) {
			out = append(out, it)
		}
	}
	return out
}

// fitSize is what a Fit child asks for along the axis of the box.
func (b *Box) fitSize(w Widget, width, height int) int {
	if s, ok := w.(Sizer); ok {
		sw, sh := s.Size(width, height)
		if b.row {
			return sw
		}
		return sh
	}
	if b.row {
		return 0
	}
	return 1
}

// layout splits total cells among the items.
func (b *Box) layout(items []Item, width, height int) []int {
	total := height
	if b.row {
		total = width
	}
	sizes := make([]int, len(items))
	left := total - b.gap*max(len(items)-1, 0)
	weights := 0
	for i, it := range items {
		switch it.kind {
		case sizeFixed:
			sizes[i] = min(it.n, max(left, 0))
		case sizeFit:
			sizes[i] = min(b.fitSize(it.w, width, height), max(left, 0))
		default:
			weights += it.n
			continue
		}
		left -= sizes[i]
	}
	if weights > 0 && left > 0 {
		given := 0
		last := -1
		for i, it := range items {
			if it.kind == sizeGrow {
				sizes[i] = left * it.n / weights
				given += sizes[i]
				last = i
			}
		}
		sizes[last] += left - given
	}
	return sizes
}

// Draw draws the children.
func (b *Box) Draw(c Canvas) {
	items := b.visible()
	sizes := b.layout(items, c.Width(), c.Height())
	pos := 0
	for i, it := range items {
		if b.row {
			c.Sub(pos, 0, sizes[i], c.Height()).Draw(it.w)
		} else {
			c.Sub(0, pos, c.Width(), sizes[i]).Draw(it.w)
		}
		pos += sizes[i] + b.gap
	}
}

// Size is the sum of the children along the axis of the box and the
// largest of them across it.
func (b *Box) Size(width, height int) (int, int) {
	items := b.visible()
	along, across := b.gap*max(len(items)-1, 0), 0
	for _, it := range items {
		var w, h int
		if s, ok := it.w.(Sizer); ok {
			w, h = s.Size(width, height)
		} else {
			w, h = width, 1
		}
		if it.kind == sizeFixed {
			if b.row {
				w = it.n
			} else {
				h = it.n
			}
		}
		if b.row {
			along += w
			across = max(across, h)
		} else {
			along += h
			across = max(across, w)
		}
	}
	if b.row {
		return min(along, width), min(across, height)
	}
	return min(across, width), min(along, height)
}

// Panel draws a border around a widget, with a title and a footer on it.
// The border takes the theme's FocusBorder while the widget, or one inside
// it, has the focus.
type Panel struct {
	child  Widget
	title  Line
	footer Line
	border Border
	pad    int
}

// NewPanel returns a panel around child with a title.
func NewPanel(title string, child Widget) *Panel {
	p := &Panel{child: child, border: BorderRounded}
	p.SetTitle(title)
	return p
}

// SetTitle sets the title, drawn in the theme's Title style.
func (p *Panel) SetTitle(title string) { p.title = Line{{Text: title}} }

// SetTitleLine sets a title of several styles, such as a title and a count.
func (p *Panel) SetTitleLine(l Line) { p.title = l }

// SetFooter sets what the bottom edge shows on its right, such as a
// position.
func (p *Panel) SetFooter(l Line) { p.footer = l }

// SetBorder sets the characters of the border.
func (p *Panel) SetBorder(b Border) { p.border = b }

// SetPadding sets the blank columns inside the border on either side.
func (p *Panel) SetPadding(n int) { p.pad = max(n, 0) }

// Child returns the widget inside the panel.
func (p *Panel) Child() Widget { return p.child }

// Children returns the widget inside the panel.
func (p *Panel) Children() []Widget { return []Widget{p.child} }

// Draw draws the border and the widget inside it.
func (p *Panel) Draw(c Canvas) {
	th := c.Theme()
	style, titleStyle := th.Border, th.Title
	if c.HasFocus(p) {
		style, titleStyle = th.FocusBorder, theme.Join(th.Title, th.FocusBorder)
	}
	title := make(Line, len(p.title))
	for i, s := range p.title {
		if s.Style == "" {
			s.Style = titleStyle
		}
		title[i] = s
	}
	inner := c.Box(p.border, style, title, p.footer)
	inner.Sub(p.pad, 0, inner.Width()-2*p.pad, inner.Height()).Draw(p.child)
}

// Size is the size of the widget inside and the border.
func (p *Panel) Size(width, height int) (int, int) {
	w, h := width, height
	if s, ok := p.child.(Sizer); ok {
		w, h = s.Size(width-2-2*p.pad, height-2)
		w += 2 + 2*p.pad
		h += 2
	}
	return min(w, width), min(h, height)
}

// Pages shows one of several widgets at a time, by name: the screens of an
// application, such as a list and the detail of one row.
type Pages struct {
	names   []string
	pages   map[string]Widget
	current string
	// OnChange is called with the name of the page shown after Show.
	OnChange func(name string)
}

// NewPages returns an empty set of pages.
func NewPages() *Pages { return &Pages{pages: map[string]Widget{}} }

// Add adds a page; the first page added is shown first.
func (p *Pages) Add(name string, w Widget) {
	if _, ok := p.pages[name]; !ok {
		p.names = append(p.names, name)
	}
	p.pages[name] = w
	if p.current == "" {
		p.current = name
	}
}

// Show shows the page of name.
func (p *Pages) Show(name string) {
	if _, ok := p.pages[name]; !ok || name == p.current {
		return
	}
	p.current = name
	if p.OnChange != nil {
		p.OnChange(name)
	}
}

// Current returns the name of the page shown.
func (p *Pages) Current() string { return p.current }

// Names returns the names of the pages in the order they were added.
func (p *Pages) Names() []string { return append([]string(nil), p.names...) }

// Page returns the widget of a page.
func (p *Pages) Page(name string) Widget { return p.pages[name] }

// Children returns the page shown.
func (p *Pages) Children() []Widget {
	if w := p.pages[p.current]; w != nil {
		return []Widget{w}
	}
	return nil
}

// Draw draws the page shown.
func (p *Pages) Draw(c Canvas) { c.Draw(p.pages[p.current]) }

// Tabs shows pages with a row of their titles above them. Ctrl-Right and
// Ctrl-Left, or "]" and "[" where no text field has the focus, switch
// between them, and so do Alt-1 to Alt-9.
type Tabs struct {
	*Pages
	titles map[string]string
	keymap *Keymap
}

// NewTabs returns empty tabs.
func NewTabs() *Tabs {
	t := &Tabs{Pages: NewPages(), titles: map[string]string{}, keymap: NewKeymap()}
	t.keymap.Bind("next tab", func() { t.step(1) }, "]", "ctrl+right").InGroup("Tabs")
	t.keymap.Bind("previous tab", func() { t.step(-1) }, "[", "ctrl+left").InGroup("Tabs")
	for i := 1; i <= 9; i++ {
		n := strconv.Itoa(i)
		t.keymap.Bind("tab "+n, func() {
			if i <= len(t.names) {
				t.Show(t.names[i-1])
			}
		}, "alt+"+n).Hide().InGroup("Tabs")
	}
	return t
}

// AddTab adds a page with the title its tab shows.
func (t *Tabs) AddTab(name, title string, w Widget) {
	t.Add(name, w)
	t.titles[name] = title
}

// Keymap returns the keys that switch tabs.
func (t *Tabs) Keymap() *Keymap { return t.keymap }

func (t *Tabs) step(delta int) {
	for i, n := range t.names {
		if n == t.current {
			j := ((i+delta)%len(t.names) + len(t.names)) % len(t.names)
			t.Show(t.names[j])
			return
		}
	}
}

// Draw draws the tab row and the page shown.
func (t *Tabs) Draw(c Canvas) {
	th := c.Theme()
	x := 0
	for i, n := range t.names {
		title := t.titles[n]
		if title == "" {
			title = n
		}
		if i > 0 {
			x += c.Text(x, 0, " │ ", th.Border)
		}
		style := th.Muted
		if n == t.current {
			style = theme.Join(th.Accent, theme.Bold)
		}
		x += c.Text(x, 0, title, style)
	}
	c.Sub(0, 1, c.Width(), c.Height()-1).Draw(t.pages[t.current])
}

// Spacer is an empty widget that takes room.
type Spacer struct{}

// Draw draws nothing.
func (Spacer) Draw(Canvas) {}

// Padded draws a widget with blank cells around it.
type Padded struct {
	child                    Widget
	top, right, bottom, left int
}

// Pad returns child with blank cells around it, in CSS order: top, right,
// bottom, left.
func Pad(child Widget, top, right, bottom, left int) *Padded {
	return &Padded{child: child, top: top, right: right, bottom: bottom, left: left}
}

// Children returns the widget inside.
func (p *Padded) Children() []Widget { return []Widget{p.child} }

// Draw draws the widget inside the padding.
func (p *Padded) Draw(c Canvas) {
	c.Sub(p.left, p.top, c.Width()-p.left-p.right, c.Height()-p.top-p.bottom).Draw(p.child)
}

// Size is the size of the widget inside and the padding.
func (p *Padded) Size(width, height int) (int, int) {
	w, h := width, 1
	if s, ok := p.child.(Sizer); ok {
		w, h = s.Size(width-p.left-p.right, height-p.top-p.bottom)
	}
	return min(w+p.left+p.right, width), min(h+p.top+p.bottom, height)
}
