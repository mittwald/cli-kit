package tui

import (
	"strconv"
	"strings"

	"github.com/mittwald/cli-kit/term"
	"github.com/mittwald/cli-kit/theme"
)

// cell is one cell of the screen. A character two cells wide takes its
// cell and the one right of it, which holds no text and width 0.
type cell struct {
	text  string
	width int
	style theme.Style
}

var blank = cell{text: " ", width: 1}

// buffer is a grid of cells: what one frame draws.
type buffer struct {
	w, h  int
	cells []cell
}

func newBuffer(w, h int) *buffer {
	w, h = max(w, 0), max(h, 0)
	b := &buffer{w: w, h: h, cells: make([]cell, w*h)}
	for i := range b.cells {
		b.cells[i] = blank
	}
	return b
}

func (b *buffer) at(x, y int) *cell { return &b.cells[y*b.w+x] }

// set puts g at x, y, taking care of the wide characters it covers.
func (b *buffer) set(x, y int, g grapheme, style theme.Style) {
	if x < 0 || y < 0 || x >= b.w || y >= b.h {
		return
	}
	// A wide character whose right half is overwritten, or whose left half
	// is, goes entirely.
	if c := b.at(x, y); c.width == 0 && x > 0 {
		*b.at(x-1, y) = cell{text: " ", width: 1, style: b.at(x-1, y).style}
	}
	if c := b.at(x, y); c.width == 2 && x+1 < b.w {
		*b.at(x+1, y) = cell{text: " ", width: 1, style: c.style}
	}
	if g.width == 2 {
		if x+1 >= b.w {
			*b.at(x, y) = cell{text: " ", width: 1, style: style}
			return
		}
		if c := b.at(x+1, y); c.width == 2 && x+2 < b.w {
			*b.at(x+2, y) = cell{text: " ", width: 1, style: c.style}
		}
		*b.at(x, y) = cell{text: g.text, width: 2, style: style}
		*b.at(x+1, y) = cell{width: 0, style: style}
		return
	}
	*b.at(x, y) = cell{text: g.text, width: 1, style: style}
}

// Lines returns the text of the buffer, one string per row, with trailing
// spaces cut off: what a test compares.
func (b *buffer) lines() []string {
	out := make([]string, b.h)
	for y := range b.h {
		var s strings.Builder
		for x := range b.w {
			s.WriteString(b.at(x, y).text)
		}
		out[y] = strings.TrimRight(s.String(), " ")
	}
	return out
}

// diff returns what turns the screen showing prev into one showing b: the
// changed cells, each run of them with one cursor move, and the styles as
// they change. With prev nil, or of another size, the whole screen is
// drawn.
func (b *buffer) diff(prev *buffer, color bool) string {
	var out strings.Builder
	full := prev == nil || prev.w != b.w || prev.h != b.h
	if full {
		out.WriteString("\x1b[H\x1b[2J")
	}
	current := theme.Style("-")
	cursorX, cursorY := -1, -1
	for y := range b.h {
		for x := 0; x < b.w; x++ {
			c := *b.at(x, y)
			if c.width == 0 {
				continue
			}
			if !full && *prev.at(x, y) == c && (c.width == 1 || *prev.at(x+1, y) == *b.at(x+1, y)) {
				continue
			}
			if cursorX != x || cursorY != y {
				out.WriteString("\x1b[" + strconv.Itoa(y+1) + ";" + strconv.Itoa(x+1) + "H")
			}
			if style := attributes(c.style, color); style != current {
				out.WriteString("\x1b[0")
				if style != "" {
					out.WriteString(";" + string(style))
				}
				out.WriteString("m")
				current = style
			}
			out.WriteString(c.text)
			cursorX, cursorY = x+c.width, y
		}
	}
	if current != "-" && current != "" {
		out.WriteString("\x1b[0m")
	}
	return out.String()
}

// attributes is style as written: whole where color is allowed, its 24-bit
// colors as the nearest of the 256 colors where the terminal takes no
// 24-bit colors, and otherwise with its colors left out and its attributes
// kept (bold, dim, italic, underline, reverse), as NO_COLOR asks.
func attributes(style theme.Style, color bool) theme.Style {
	if style == "" {
		return style
	}
	if color {
		if trueColor() || !strings.Contains(string(style), ";2;") {
			return style
		}
		return to256(style)
	}
	params := strings.Split(string(style), ";")
	var kept []string
	for i := 0; i < len(params); i++ {
		switch p := params[i]; p {
		case "1", "2", "3", "4", "7":
			kept = append(kept, p)
		case "38", "48", "58":
			// An extended color: 5;n or 2;r;g;b follow.
			if i+1 < len(params) && params[i+1] == "5" {
				i += 2
			} else if i+1 < len(params) && params[i+1] == "2" {
				i += 4
			}
		}
	}
	return theme.Style(strings.Join(kept, ";"))
}

// to256 is style with each 24-bit color as the nearest of the 256 colors.
func to256(style theme.Style) theme.Style {
	params := strings.Split(string(style), ";")
	out := make([]string, 0, len(params))
	for i := 0; i < len(params); i++ {
		p := params[i]
		if (p == "38" || p == "48" || p == "58") && i+4 < len(params) && params[i+1] == "2" {
			c, skip := extended(params[i+1:])
			if c.known && skip == 4 {
				out = append(out, p, "5", strconv.Itoa(term.Nearest256(c.c.r, c.c.g, c.c.b)))
				i += skip
				continue
			}
		}
		out = append(out, p)
	}
	return theme.Style(strings.Join(out, ";"))
}

// frame is what every canvas of one draw shares.
type frame struct {
	app   *App
	buf   *buffer
	theme Theme
	// focus is the widget with the focus and focusPath it and every widget
	// that holds it.
	focus     Widget
	focusPath []Widget
	// cursorX and cursorY place the terminal's cursor, -1 to hide it.
	cursorX, cursorY int
	// regions are where widgets were drawn, the last drawn on top, for
	// the mouse.
	regions []region
	// readable caches what readable makes of a style.
	readable map[theme.Style]theme.Style
}

// region is the rectangle a widget was drawn into.
type region struct {
	w           Widget
	x, y, w2, h int
}

// Canvas is a rectangle of the screen a widget draws into. Drawing outside
// it is clipped. Coordinates are relative to its top left corner.
type Canvas struct {
	f          *frame
	x, y, w, h int
	// base is the style everything drawn in the canvas starts from, such
	// as the background of a dialog.
	base theme.Style
}

// Width is how many cells wide the canvas is.
func (c Canvas) Width() int { return c.w }

// Height is how many rows high the canvas is.
func (c Canvas) Height() int { return c.h }

// App is the application the canvas belongs to.
func (c Canvas) App() *App { return c.f.app }

// Theme is the theme to draw with.
func (c Canvas) Theme() Theme { return c.f.theme }

// Sub returns the part of c at x, y of size w×h, clipped to c.
func (c Canvas) Sub(x, y, w, h int) Canvas {
	x0, y0 := min(max(x, 0), c.w), min(max(y, 0), c.h)
	x1, y1 := min(max(x+w, x0), c.w), min(max(y+h, y0), c.h)
	return Canvas{f: c.f, x: c.x + x0, y: c.y + y0, w: x1 - x0, h: y1 - y0, base: c.base}
}

// WithStyle returns c with base as the style everything drawn in it starts
// from: spans drawn into it add their own style on top.
func (c Canvas) WithStyle(base theme.Style) Canvas {
	c.base = theme.Join(c.base, base)
	return c
}

// style is s drawn on the base of c, its foreground made readable on its
// background.
func (c Canvas) style(s theme.Style) theme.Style {
	if c.base != "" {
		s = theme.Join(c.base, s)
	}
	if r, ok := c.f.readable[s]; ok {
		return r
	}
	r := readable(s)
	if c.f.readable == nil {
		c.f.readable = map[theme.Style]theme.Style{}
	}
	c.f.readable[s] = r
	return r
}

// Text draws s at x, y in style and returns how many cells it took. It is
// clipped at the right edge of the canvas; control characters are left out.
func (c Canvas) Text(x, y int, s string, style theme.Style) int {
	if y < 0 || y >= c.h {
		return 0
	}
	used := 0
	st := c.style(style)
	for _, g := range graphemes(s) {
		if g.width == 0 {
			// A character of no width, such as a zero-width space, takes
			// no cell: the terminal would not move past one.
			continue
		}
		if x+used+g.width > c.w {
			break
		}
		if x+used >= 0 {
			c.f.buf.set(c.x+x+used, c.y+y, g, st)
		}
		used += g.width
	}
	return used
}

// Line draws a line at x, y and returns how many cells it took.
func (c Canvas) Line(x, y int, l Line) int {
	used := 0
	for _, s := range l {
		drawn := c.Text(x+used, y, s.Text, s.Style)
		used += drawn
		if drawn < textWidth(s.Text) {
			// Clipped at the right edge: nothing after it is drawn
			// where the cut-off part would have been.
			break
		}
	}
	return used
}

// Fill paints every cell of the canvas blank in style.
func (c Canvas) Fill(style theme.Style) {
	st := c.style(style)
	for y := range c.h {
		for x := range c.w {
			c.f.buf.set(c.x+x, c.y+y, grapheme{text: " ", width: 1}, st)
		}
	}
}

// FillRow paints row y blank in style, from x to the right edge.
func (c Canvas) FillRow(x, y int, style theme.Style) {
	if y < 0 || y >= c.h {
		return
	}
	st := c.style(style)
	for i := max(x, 0); i < c.w; i++ {
		c.f.buf.set(c.x+i, c.y+y, grapheme{text: " ", width: 1}, st)
	}
}

// Border is the set of characters a box is drawn with.
type Border struct {
	TopLeft, Top, TopRight, Left, Right, BottomLeft, Bottom, BottomRight string
}

// The borders a box can have.
var (
	BorderRounded = Border{"╭", "─", "╮", "│", "│", "╰", "─", "╯"}
	BorderSingle  = Border{"┌", "─", "┐", "│", "│", "└", "─", "┘"}
	BorderDouble  = Border{"╔", "═", "╗", "║", "║", "╚", "═", "╝"}
	BorderThick   = Border{"┏", "━", "┓", "┃", "┃", "┗", "━", "┛"}
	BorderASCII   = Border{"+", "-", "+", "|", "|", "+", "-", "+"}
)

// Card draws a title bar across the top row of the canvas in the theme's
// Bar style and returns the canvas below it. The first span of title is a
// badge, in the theme's Badge style where focused and BadgeBlurred where
// not, unless it has a style of its own; the rest of title follows it, and
// footer sits on the right of the bar.
func (c Canvas) Card(title, footer Line, focused bool) Canvas {
	if c.w < 1 || c.h < 1 {
		return c.Sub(0, 0, 0, 0)
	}
	th := c.Theme()
	bar := c.Sub(0, 0, c.w, 1).WithStyle(th.Bar)
	bar.Fill("")
	var line Line
	if len(title) > 0 {
		head := title[0]
		if head.Style == "" {
			head.Style = th.BadgeBlurred
			if focused {
				head.Style = th.Badge
			}
		}
		head.Text = " " + head.Text + " "
		line = Line{head}
		if len(title) > 1 {
			line = append(append(line, Span{Text: " "}), title[1:]...)
		}
	}
	fw := 0
	if len(footer) > 0 {
		fw = min(footer.Width(), max(c.w-2, 0))
		bar.Line(c.w-1-fw, 0, TruncateLine(footer, fw))
	}
	room := c.w - fw
	if fw > 0 {
		room -= 2
	}
	bar.Line(0, 0, TruncateLine(line, max(room, 0)))
	return c.Sub(0, 1, c.w, c.h-1)
}

// Box draws border around the canvas in style, with title on its top edge
// and footer on its bottom edge, right-aligned, and returns the canvas
// inside it.
func (c Canvas) Box(border Border, style theme.Style, title, footer Line) Canvas {
	if c.w < 2 || c.h < 2 {
		return c.Sub(0, 0, 0, 0)
	}
	c.Text(0, 0, border.TopLeft, style)
	c.Text(c.w-1, 0, border.TopRight, style)
	c.Text(0, c.h-1, border.BottomLeft, style)
	c.Text(c.w-1, c.h-1, border.BottomRight, style)
	for x := 1; x < c.w-1; x++ {
		c.Text(x, 0, border.Top, style)
		c.Text(x, c.h-1, border.Bottom, style)
	}
	for y := 1; y < c.h-1; y++ {
		c.Text(0, y, border.Left, style)
		c.Text(c.w-1, y, border.Right, style)
	}
	if room := c.w - 4; room > 0 && len(title) > 0 {
		title = TruncateLine(title, room)
		c.Text(1, 0, " ", style)
		used := c.Line(2, 0, title)
		c.Text(2+used, 0, " ", style)
	}
	if room := c.w - 4; room > 0 && len(footer) > 0 {
		footer = TruncateLine(footer, room)
		w := footer.Width()
		c.Text(c.w-3-w, c.h-1, " ", style)
		c.Line(c.w-2-w, c.h-1, footer)
		c.Text(c.w-2, c.h-1, " ", style)
	}
	return c.Sub(1, 1, c.w-2, c.h-2)
}

// Draw draws w into the canvas and remembers where, for the mouse.
func (c Canvas) Draw(w Widget) {
	if w == nil || c.w <= 0 || c.h <= 0 {
		return
	}
	c.f.regions = append(c.f.regions, region{w: w, x: c.x, y: c.y, w2: c.w, h: c.h})
	w.Draw(c)
}

// Focused reports whether w has the focus.
func (c Canvas) Focused(w Widget) bool { return same(c.f.focus, w) }

// HasFocus reports whether w or a widget inside it has the focus.
func (c Canvas) HasFocus(w Widget) bool { return contains(c.f.focusPath, w) }

// ShowCursor puts the terminal's cursor at x, y of the canvas, where a text
// field wants it. The cursor is hidden in every frame that does not ask for
// it.
func (c Canvas) ShowCursor(x, y int) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return
	}
	c.f.cursorX, c.f.cursorY = c.x+x, c.y+y
}

// VScrollbar draws a scroll bar into the column x of the canvas, for a view
// of height rows starting at offset of total rows. Nothing is drawn when
// everything fits.
func (c Canvas) VScrollbar(x, offset, height, total int) {
	if total <= height || c.h <= 0 {
		return
	}
	th := c.f.theme
	size := max(c.h*height/total, 1)
	pos := 0
	if total > height {
		pos = (c.h - size) * offset / (total - height)
	}
	for y := range c.h {
		if y >= pos && y < pos+size {
			c.Text(x, y, "┃", th.ScrollThumb)
		} else {
			c.Text(x, y, "│", th.Scrollbar)
		}
	}
}
