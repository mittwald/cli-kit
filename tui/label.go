package tui

import (
	"strings"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// Align is the horizontal alignment of text.
type Align int

// Alignments.
const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
)

// Label shows lines of text. It takes no focus.
type Label struct {
	lines []Line
	wrap  bool
	align Align
	// fn, when set, makes the lines anew for every frame.
	fn func() []Line
}

// NewLabel returns a label of text in one style. The text is made safe to
// show with term.Plain, so text from elsewhere can be passed as it is.
func NewLabel(text string, style theme.Style) *Label {
	l := &Label{}
	l.SetText(text, style)
	return l
}

// NewLines returns a label of styled lines.
func NewLines(lines ...Line) *Label { return &Label{lines: lines} }

// NewDynamic returns a label whose lines fn makes for every frame, such as
// a header with a count.
func NewDynamic(fn func() []Line) *Label { return &Label{fn: fn} }

// SetText replaces the text.
func (l *Label) SetText(text string, style theme.Style) {
	l.lines = nil
	for line := range strings.SplitSeq(term.Plain(text), "\n") {
		l.lines = append(l.lines, Text(line, style))
	}
}

// SetLines replaces the lines.
func (l *Label) SetLines(lines ...Line) { l.lines = lines }

// SetWrap makes lines wider than the label wrap rather than be cut.
func (l *Label) SetWrap(wrap bool) *Label { l.wrap = wrap; return l }

// SetAlign aligns the lines.
func (l *Label) SetAlign(a Align) *Label { l.align = a; return l }

func (l *Label) content(width int) []Line {
	lines := l.lines
	if l.fn != nil {
		lines = l.fn()
	}
	if !l.wrap {
		return lines
	}
	var out []Line
	for _, line := range lines {
		out = append(out, WrapLine(line, width)...)
	}
	return out
}

// Draw draws the lines, cut or wrapped to the width.
func (l *Label) Draw(c Canvas) {
	for y, line := range l.content(c.Width()) {
		if y >= c.Height() {
			break
		}
		line = TruncateLine(line, c.Width())
		x := 0
		switch l.align {
		case AlignCenter:
			x = (c.Width() - line.Width()) / 2
		case AlignRight:
			x = c.Width() - line.Width()
		}
		c.Line(x, y, line)
	}
}

// Size is the widest line and the number of lines.
func (l *Label) Size(width, height int) (int, int) {
	lines := l.content(width)
	w := 0
	for _, line := range lines {
		w = max(w, line.Width())
	}
	return min(w, width), min(len(lines), height)
}

// Legend shows the keys that apply now and what they do, on one line: the
// focused widget's first, then those of the widgets around it and of the
// application. What does not fit is left out, behind "? more" when the help
// is bound.
type Legend struct{}

// NewLegend returns a legend.
func NewLegend() *Legend { return &Legend{} }

// Draw draws the keys.
func (*Legend) Draw(c Canvas) {
	th := c.Theme()
	app := c.App()
	if app == nil {
		return
	}
	bindings := app.ActiveBindings()
	var help *Binding
	var shown []*Binding
	for _, b := range bindings {
		if b.Hidden {
			continue
		}
		if b.Help == "help" && b.Group == "General" {
			help = b
			continue
		}
		shown = append(shown, b)
	}
	item := func(b *Binding) Line {
		return Line{{Text: b.Label(), Style: th.Key}, {Text: " " + b.Help, Style: th.KeyHelp}}
	}
	var tail Line
	if help != nil {
		tail = item(help)
	}
	x := 0
	room := c.Width()
	for i, b := range shown {
		l := item(b)
		need := l.Width()
		if x > 0 {
			need += 2
		}
		rest := 0
		if tail != nil && i < len(shown) {
			rest = tail.Width() + 2
		}
		if x+need+rest > room {
			if tail != nil {
				tail = Line{{Text: "?", Style: th.Key}, {Text: " more", Style: th.KeyHelp}}
			}
			break
		}
		if x > 0 {
			x += 2
		}
		x += c.Line(x, 0, l)
	}
	if tail != nil {
		if x > 0 {
			x += 2
		}
		c.Line(x, 0, tail)
	}
}

// Size is one line.
func (*Legend) Size(width, _ int) (int, int) { return width, 1 }

// StatusBar is the line at the bottom of an application: a notice from
// App.Notify, or what App.Busy says it is busy with, or else text of its
// own on the left, and text on the right. A notice fills the whole bar
// with the color of its level, its label in front, as a warning stands out
// at the bottom of a timesheet.
type StatusBar struct {
	left, right func() Line
	// top makes it a title bar: no notices, in the theme's TitleBar style.
	top bool
}

// NewStatusBar returns a status bar. left and right make the text on either
// side for every frame; either may be nil.
func NewStatusBar(left, right func() Line) *StatusBar {
	return &StatusBar{left: left, right: right}
}

// NewTitleBar returns the line at the top of an application, in the
// theme's TitleBar style: the name of the tool on the left, say, and where
// it is connected to on the right. left and right make the text on either
// side for every frame; either may be nil.
func NewTitleBar(left, right func() Line) *StatusBar {
	return &StatusBar{left: left, right: right, top: true}
}

// Draw draws the bar.
func (s *StatusBar) Draw(c Canvas) {
	th := c.Theme()
	style := th.StatusBar
	if s.top {
		style = th.TitleBar
	}
	var left Line
	if app := c.App(); app != nil && !s.top {
		notice, busy := app.Status()
		switch {
		case notice != nil:
			color, label := map[Level]theme.Style{
				LevelInfo: th.Info, LevelSuccess: th.Success,
				LevelWarning: th.Warning, LevelError: th.Error,
			}[notice.Level], notice.Level.String()
			style = Solid(color)
			c = c.WithStyle(style)
			c.Fill("")
			c.Line(0, 0, TruncateLine(Line{
				{Text: " " + label + strings.Repeat(" ", max(6-len(label), 1)), Style: theme.Bold},
				{Text: term.PlainLine(notice.Text), Style: "22"},
			}, c.Width()))
			return
		case busy != "":
			left = Text(" "+busy, th.Accent)
		}
	}
	c = c.WithStyle(style)
	c.Fill("")
	var right Line
	if s.right != nil {
		right = s.right()
	}
	rw := min(right.Width(), max(c.Width()-1, 0))
	c.Line(c.Width()-rw-1, 0, TruncateLine(right, rw))
	room := c.Width() - rw - 2
	if left == nil && s.left != nil {
		left = append(Line{{Text: " "}}, s.left()...)
	}
	c.Line(0, 0, TruncateLine(left, max(room, 0)))
}

// Size is one line.
func (*StatusBar) Size(width, _ int) (int, int) { return width, 1 }

// help lists every binding that applies, grouped, in a layer.
type help struct {
	view *Viewport
}

func newHelp(a *App) *help {
	th := a.Theme()
	groups := map[string][]*Binding{}
	var order []string
	for _, b := range a.ActiveBindings() {
		g := b.Group
		if g == "" {
			g = "Keys"
		}
		if _, ok := groups[g]; !ok {
			order = append(order, g)
		}
		groups[g] = append(groups[g], b)
	}
	keyWidth := 0
	for _, bs := range groups {
		for _, b := range bs {
			keyWidth = max(keyWidth, textWidth(strings.Join(b.Keys, " ")))
		}
	}
	keyWidth = min(keyWidth, 24)
	var lines []Line
	for i, g := range order {
		if i > 0 {
			lines = append(lines, nil)
		}
		lines = append(lines, Text(g, th.Title))
		for _, b := range groups[g] {
			keys := truncate(strings.Join(b.Keys, " "), keyWidth)
			pad := strings.Repeat(" ", keyWidth-textWidth(keys))
			lines = append(lines, Line{{Text: "  " + keys + pad + "  ", Style: th.Key}, {Text: b.Help}})
		}
	}
	h := &help{view: NewViewport()}
	h.view.SetLines(lines)
	h.view.Keymap().Bind("close", a.Pop, "q", "?")
	return h
}

func (h *help) Children() []Widget { return []Widget{h.view} }

func (h *help) Draw(c Canvas) {
	th := c.Theme()
	inner := c.Card(Line{{Text: "Keys"}}, Text("esc close", th.Muted), true)
	inner.Sub(2, 1, inner.Width()-2, inner.Height()-1).Draw(h.view)
}

func (h *help) Size(width, height int) (int, int) {
	w := 0
	for _, l := range h.view.lines {
		w = max(w, l.Width())
	}
	return min(w+5, width), min(len(h.view.lines)+3, height)
}
