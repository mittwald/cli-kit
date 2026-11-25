package tui

import (
	"strconv"
	"strings"

	"github.com/mittwald/cli-kit/theme"
)

// Viewport shows lines that may not fit and scrolls through them, with a
// scroll bar on the right. Lines wider than the viewport wrap. It knows the
// keys of a pager:
//
//	↑/↓ k/j               line up/down
//	pgup/pgdown ctrl+b/f  page up/down (also space)
//	ctrl+u/d              half a page up/down
//	g/G home/end          top/bottom
//	/                     search; n/N next/previous match
//
// With follow on, the view stays at the bottom while lines are added, as a
// log does, until it is scrolled up.
type Viewport struct {
	lines   []Line
	wrapped []Line
	// starts maps each line to the first wrapped row it starts.
	starts   []int
	width    int
	height   int
	top      int
	follow   bool
	atBottom bool
	keymap   *Keymap
	disabled bool
	// pending are the scrolls asked for since the lines changed, done in
	// order at the next draw, once the lines are laid out.
	pending []func()

	searching  bool
	query      *editor
	queryFirst int
	term       string
	matches    []int
	match      int
}

// NewViewport returns an empty viewport.
func NewViewport() *Viewport {
	v := &Viewport{keymap: NewKeymap(), query: newEditor(false)}
	k := v.keymap
	k.Bind("scroll", func() { v.ScrollBy(-1) }, "up", "k").InGroup("View")
	k.Bind("scroll", func() { v.ScrollBy(1) }, "down", "j").Hide().InGroup("View")
	k.Bind("page up", func() { v.ScrollBy(-v.page()) }, "pgup", "ctrl+b").Hide().InGroup("View")
	k.Bind("page down", func() { v.ScrollBy(v.page()) }, "pgdown", "ctrl+f", "space").Hide().InGroup("View")
	k.Bind("half page up", func() { v.ScrollBy(-max(v.height/2, 1)) }, "ctrl+u").Hide().InGroup("View")
	k.Bind("half page down", func() { v.ScrollBy(max(v.height/2, 1)) }, "ctrl+d").Hide().InGroup("View")
	k.Bind("top", v.ScrollTop, "g", "home").Hide().InGroup("View")
	k.Bind("bottom", v.ScrollBottom, "G", "end").Hide().InGroup("View")
	k.Bind("search", v.startSearch, "/").InGroup("View")
	k.Bind("next match", func() { v.nextMatch(1) }, "n").
		When(func() bool { return len(v.matches) > 0 }).InGroup("View")
	k.Bind("previous match", func() { v.nextMatch(-1) }, "N").
		When(func() bool { return len(v.matches) > 0 }).Hide().InGroup("View")
	return v
}

// Keymap returns the keys of the viewport.
func (v *Viewport) Keymap() *Keymap { return v.keymap }

// SetLines replaces the lines. The view keeps its position, or stays at the
// bottom with follow on.
func (v *Viewport) SetLines(lines []Line) {
	v.lines = lines
	v.wrapped = nil
	v.search()
}

// SetText replaces the lines with text in one style.
func (v *Viewport) SetText(text string, style theme.Style) {
	var lines []Line
	for l := range strings.SplitSeq(text, "\n") {
		lines = append(lines, Text(l, style))
	}
	v.SetLines(lines)
}

// Append adds lines at the end.
func (v *Viewport) Append(lines ...Line) {
	v.lines = append(v.lines, lines...)
	v.wrapped = nil
	v.search()
}

// SetFollow keeps the view at the bottom as lines are added.
func (v *Viewport) SetFollow(follow bool) *Viewport {
	v.follow, v.atBottom = follow, follow
	return v
}

// SetDisabled keeps the viewport from taking the focus, for text that is
// only shown.
func (v *Viewport) SetDisabled(disabled bool) *Viewport { v.disabled = disabled; return v }

// CanFocus reports whether the viewport takes the focus.
func (v *Viewport) CanFocus() bool { return !v.disabled }

// TakesText reports whether the search line takes typed characters now.
func (v *Viewport) TakesText() bool { return v.searching }

func (v *Viewport) page() int { return max(v.height-1, 1) }

func (v *Viewport) layout(width int) {
	if v.wrapped != nil && width == v.width {
		return
	}
	v.width = width
	v.wrapped = v.wrapped[:0]
	v.starts = v.starts[:0]
	for _, l := range v.lines {
		v.starts = append(v.starts, len(v.wrapped))
		v.wrapped = append(v.wrapped, WrapLine(l, width)...)
	}
	if v.wrapped == nil {
		v.wrapped = []Line{}
	}
}

func (v *Viewport) maxTop() int { return max(len(v.wrapped)-v.height, 0) }

// ScrollBy scrolls n rows down, or up for a negative n.
func (v *Viewport) ScrollBy(n int) {
	if v.later(func() { v.ScrollBy(n) }) {
		return
	}
	v.top = min(max(v.top+n, 0), v.maxTop())
	v.atBottom = v.top >= v.maxTop()
}

// ScrollTop scrolls to the first line.
func (v *Viewport) ScrollTop() { v.top, v.atBottom, v.pending = 0, false, nil }

// ScrollBottom scrolls to the last line.
func (v *Viewport) ScrollBottom() {
	if v.later(v.ScrollBottom) {
		return
	}
	v.top, v.atBottom = v.maxTop(), true
}

// later keeps fn for the next draw where the lines changed since the last
// layout, so it scrolls by the lines as wrapped, and reports whether it
// did.
func (v *Viewport) later(fn func()) bool {
	if v.wrapped != nil {
		return false
	}
	v.pending = append(v.pending, fn)
	return true
}

// ScrollToLine scrolls so that line n, counted in the lines given rather
// than as wrapped, is at the top.
func (v *Viewport) ScrollToLine(n int) {
	if v.later(func() { v.ScrollToLine(n) }) {
		return
	}
	if n < 0 || n >= len(v.starts) {
		return
	}
	v.top = min(v.starts[n], v.maxTop())
	v.atBottom = v.top >= v.maxTop()
}

// Position is how far the view is scrolled, as "Top", "Bot", "All" or a
// percentage, the way less and vim show it.
func (v *Viewport) Position() string {
	switch {
	case len(v.wrapped) <= v.height:
		return "All"
	case v.top == 0:
		return "Top"
	case v.top >= v.maxTop():
		return "Bot"
	}
	return strconv.Itoa(v.top*100/v.maxTop()) + "%"
}

func (v *Viewport) startSearch() {
	v.searching = true
	v.query.set(v.term)
}

// search finds the lines that hold the search term.
func (v *Viewport) search() {
	v.matches = nil
	if v.term == "" {
		return
	}
	t := strings.ToLower(v.term)
	for i, l := range v.lines {
		if strings.Contains(strings.ToLower(l.String()), t) {
			v.matches = append(v.matches, i)
		}
	}
	v.match = min(v.match, max(len(v.matches)-1, 0))
}

func (v *Viewport) nextMatch(delta int) {
	if len(v.matches) == 0 {
		return
	}
	v.match = ((v.match+delta)%len(v.matches) + len(v.matches)) % len(v.matches)
	v.scrollToMatch(v.matches[v.match])
}

// scrollToMatch scrolls to the first wrapped row of line n that holds the
// search term, so a match far into a long line is in view.
func (v *Viewport) scrollToMatch(n int) {
	if v.later(func() { v.scrollToMatch(n) }) {
		return
	}
	if n < 0 || n >= len(v.starts) {
		return
	}
	end := len(v.wrapped)
	if n+1 < len(v.starts) {
		end = v.starts[n+1]
	}
	row := v.starts[n]
	t := strings.ToLower(v.term)
	for r := v.starts[n]; r < end; r++ {
		if strings.Contains(strings.ToLower(v.wrapped[r].String()), t) {
			row = r
			break
		}
	}
	v.top = min(row, v.maxTop())
	v.atBottom = v.top >= v.maxTop()
}

// HandleKey edits the search line while it is open.
func (v *Viewport) HandleKey(k Key) bool {
	if !v.searching {
		return false
	}
	switch k.String() {
	case "esc":
		v.searching = false
		return true
	case "enter":
		v.searching = false
		v.term = v.query.String()
		v.match = 0
		v.search()
		// Start from the first match at or below the view.
		for i, m := range v.matches {
			if m < len(v.starts) && v.starts[m] >= v.top {
				v.match = i
				break
			}
		}
		v.nextMatch(0)
		return true
	}
	took, _ := v.query.handle(k)
	return took || k.Printable()
}

// HandlePaste takes pasted text into the search line while it is open.
func (v *Viewport) HandlePaste(text string) bool {
	if !v.searching {
		return false
	}
	v.query.remember("")
	v.query.insertText(text)
	return true
}

// HandleMouse scrolls with the wheel.
func (v *Viewport) HandleMouse(m Mouse) bool {
	switch m.Button {
	case MouseWheelUp:
		v.ScrollBy(-3)
	case MouseWheelDown:
		v.ScrollBy(3)
	default:
		return false
	}
	return true
}

// Draw draws the visible lines, the scroll bar and the search line.
func (v *Viewport) Draw(c Canvas) {
	th := c.Theme()
	height := c.Height()
	if v.searching {
		height--
	}
	v.height = max(height, 1)
	v.layout(max(c.Width()-1, 1))
	pending := v.pending
	v.pending = nil
	for _, fn := range pending {
		fn()
	}
	if v.follow && v.atBottom {
		v.top = v.maxTop()
	}
	v.top = min(v.top, v.maxTop())
	term := strings.ToLower(v.term)
	for y := 0; y < height && v.top+y < len(v.wrapped); y++ {
		l := v.wrapped[v.top+y]
		if term != "" && strings.Contains(strings.ToLower(l.String()), term) {
			c.Line(0, y, highlight(l, term, th.Match))
			continue
		}
		c.Line(0, y, l)
	}
	c.Sub(0, 0, c.Width(), height).VScrollbar(c.Width()-1, v.top, v.height, len(v.wrapped))
	if v.searching {
		x := c.Text(0, height, "/", th.Key)
		drawLine(c, x, height, v.query, &v.queryFirst, "", c.Focused(v))
	}
}

// highlight paints every occurrence of term, lower case, in l with style.
func highlight(l Line, term string, style theme.Style) Line {
	text := l.String()
	lower := strings.ToLower(text)
	if len(lower) != len(text) {
		return l
	}
	marked := make([]bool, len(text))
	for i := 0; ; {
		j := strings.Index(lower[i:], term)
		if j < 0 {
			break
		}
		for k := i + j; k < i+j+len(term); k++ {
			marked[k] = true
		}
		i += j + max(len(term), 1)
	}
	var out Line
	pos := 0
	for _, s := range l {
		start := pos
		for start < pos+len(s.Text) {
			end := start
			for end < pos+len(s.Text) && marked[end] == marked[start] {
				end++
			}
			st := s.Style
			if marked[start] {
				st = theme.Join(s.Style, style)
			}
			out = append(out, Span{Text: text[start:end], Style: st})
			start = end
		}
		pos += len(s.Text)
	}
	return out
}
