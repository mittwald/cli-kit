package tui

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ErrNotATerminal is what [App.Run] returns when its input or output is not
// a terminal.
var ErrNotATerminal = errors.New("tui: not a terminal")

// ErrInputClosed is what [App.Run] returns when the terminal went away
// before the application quit.
var ErrInputClosed = errors.New("tui: input closed")

// escapeGrace is how long the rest of a split escape sequence may take
// before a pending ESC counts as the Esc key.
const escapeGrace = 40 * time.Millisecond

// noticeTime is how long a notice stays in the status bar.
const noticeTime = 4 * time.Second

// tickInterval is how often the screen is redrawn while something moves on
// it: a spinner, or a notice that is to go.
const tickInterval = 100 * time.Millisecond

// Level is the severity of a notice.
type Level int

// The levels of a notice.
const (
	LevelInfo Level = iota
	LevelSuccess
	LevelWarning
	LevelError
)

// String is the label a notice of the level carries in the status bar:
// INFO, OK, WARN or ERROR, as package notice labels its notices.
func (l Level) String() string {
	switch l {
	case LevelSuccess:
		return "OK"
	case LevelWarning:
		return "WARN"
	case LevelError:
		return "ERROR"
	}
	return "INFO"
}

// Notice is a message shown in the status bar for a while.
type Notice struct {
	Level Level
	Text  string
	until time.Time
}

// LayerOptions shape a layer pushed with [App.Push].
type LayerOptions struct {
	// Width and Height are the size of the layer in cells. Zero takes what
	// the widget asks for as a [Sizer], or else two thirds of the screen.
	// The layer is centered and never larger than the screen.
	Width, Height int
	// NoEscape keeps Esc from closing the layer, for a layer that binds Esc
	// itself or must be answered.
	NoEscape bool
	// Focus is the widget that takes the focus, the first one that can
	// when nil.
	Focus Focusable
	// OnClose is called once the layer is closed, however that happens.
	OnClose func()
}

// layer is the root widget or a widget pushed over it.
type layer struct {
	w     Widget
	opts  LayerOptions
	focus Focusable
}

// Option configures an App.
type Option func(*App)

// WithTheme sets the styles the widgets draw with; DefaultTheme otherwise,
// or PlainTheme where the terminal takes no colors.
func WithTheme(t Theme) Option { return func(a *App) { a.theme, a.themed = t, true } }

// WithMouse turns the mouse on: clicks focus widgets and reach Clickers,
// the wheel scrolls. Text can then be selected with Shift held, in most
// terminals.
func WithMouse() Option { return func(a *App) { a.mouse = true } }

// WithTerminal makes the application run on in and out rather than on
// os.Stdin and os.Stdout. Both must be the same terminal.
func WithTerminal(in, out *os.File) Option {
	return func(a *App) { a.in, a.out = in, out }
}

// WithoutHelp leaves out the "?" binding that shows the help.
func WithoutHelp() Option { return func(a *App) { a.noHelp = true } }

// App runs a full-screen application: it takes the terminal over, draws
// the widget tree for every change and hands keys, pastes and the mouse to
// the widgets.
//
// Everything about the application, its widgets included, belongs to the
// goroutine that runs it. Work that blocks, such as a request, goes through
// [App.Go]; other goroutines reach the application through [App.Post].
type App struct {
	root   *layer
	layers []*layer
	keymap *Keymap
	theme  Theme
	themed bool
	mouse  bool
	noHelp bool
	in     *os.File
	out    *os.File

	// queue holds what Post handed over, and wake says it holds something.
	queueMu sync.Mutex
	queue   []func()
	wake    chan struct{}
	pending sync.WaitGroup
	panics  chan any

	quit    bool
	err     error
	width   int
	height  int
	prev    *buffer
	regions []region
	input   []byte
	// skipPaste drops input up to the end of a paste that was too long.
	skipPaste bool

	notice  *Notice
	busy    []string
	started time.Time
	// redraw asks for the whole screen to be drawn again.
	redraw bool
	// suspend is set by Suspend for the loop to act on.
	suspend bool
}

// New returns an application that shows root.
func New(root Widget, opts ...Option) *App {
	a := &App{
		root:    &layer{w: root},
		keymap:  NewKeymap(),
		theme:   DefaultTheme(),
		wake:    make(chan struct{}, 1),
		panics:  make(chan any, 1),
		started: time.Now(),
	}
	a.layers = []*layer{a.root}
	for _, o := range opts {
		o(a)
	}
	a.keymap.Bind("quit", a.Quit, "ctrl+c").Hide().InGroup("General")
	if !a.noHelp {
		a.keymap.Bind("help", a.ShowHelp, "?", "f1").InGroup("General")
	}
	return a
}

// Keymap returns the bindings of the whole application. They apply when
// no widget on the focus path took the key, and not while a layer is
// pushed. Ctrl-C quits and "?" shows the help unless WithoutHelp was given.
func (a *App) Keymap() *Keymap { return a.keymap }

// Theme returns the styles the widgets draw with.
func (a *App) Theme() Theme { return a.theme }

// Root returns the widget the application shows.
func (a *App) Root() Widget { return a.root.w }

// SetRoot shows w instead of the root widget, with the focus on its first
// focusable widget.
func (a *App) SetRoot(w Widget) {
	a.root.w = w
	// The widget that had the focus learns it lost it, then the new tree
	// gets its first focusable widget.
	if a.top() == a.root {
		notifyFocus(a.root.focus, false)
	}
	a.root.focus = nil
	a.ensureFocus(a.root)
}

// Size is the size of the screen in cells.
func (a *App) Size() (width, height int) { return a.width, a.height }

// Quit ends Run once the current event is handled.
func (a *App) Quit() { a.quit = true }

// QuitWith ends Run, which then returns err.
func (a *App) QuitWith(err error) { a.quit, a.err = true, err }

// Post runs fn on the application's goroutine. It may be called from any
// goroutine; fn may change widgets, and the screen is redrawn after it.
func (a *App) Post(fn func()) {
	a.queueMu.Lock()
	a.queue = append(a.queue, fn)
	a.queueMu.Unlock()
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// runPosted runs what Post handed over, also what those functions post.
func (a *App) runPosted() {
	for {
		a.queueMu.Lock()
		fns := a.queue
		a.queue = nil
		a.queueMu.Unlock()
		if len(fns) == 0 {
			return
		}
		for _, fn := range fns {
			fn()
		}
	}
}

// Go runs work on a goroutine of its own and the function it returns back
// on the application's goroutine, where it may change widgets:
//
//	app.Go(func() func() {
//		tickets, err := client.List(ctx, query) // blocks
//		return func() { list.SetItems(tickets); ... }
//	})
//
// A panic in work is raised again on the application's goroutine, where
// Run puts the terminal back before the program ends.
func (a *App) Go(work func() func()) {
	a.pending.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				select {
				case a.panics <- r:
				default:
				}
				a.Post(func() {})
			}
		}()
		if done := work(); done != nil {
			a.Post(done)
		}
	})
}

// Busy shows text with a spinner in the status bar until the function it
// returns is called: around work started with Go.
func (a *App) Busy(text string) (done func()) {
	a.busy = append(a.busy, text)
	var once sync.Once
	return func() {
		once.Do(func() {
			for i, t := range a.busy {
				if t == text {
					a.busy = append(a.busy[:i], a.busy[i+1:]...)
					break
				}
			}
		})
	}
}

// Notify shows text in the status bar for a few seconds.
func (a *App) Notify(level Level, text string) {
	a.notice = &Notice{Level: level, Text: text, until: time.Now().Add(noticeTime)}
}

// Notifyf is Notify with a format.
func (a *App) Notifyf(level Level, format string, args ...any) {
	a.Notify(level, fmt.Sprintf(format, args...))
}

// Status returns the notice to show now, if any, and what the application
// is busy with, with a spinner frame for it: what a [StatusBar] draws.
func (a *App) Status() (notice *Notice, busy string) {
	if a.notice != nil && time.Now().After(a.notice.until) {
		a.notice = nil
	}
	if len(a.busy) > 0 {
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := int(time.Since(a.started)/tickInterval) % len(frames)
		busy = frames[i] + " " + a.busy[len(a.busy)-1]
	}
	return a.notice, busy
}

// top is the layer that takes the keys.
func (a *App) top() *layer { return a.layers[len(a.layers)-1] }

// Push shows w over everything else, until Esc or Pop closes it. Keys go
// to the layer only.
func (a *App) Push(w Widget, opts LayerOptions) {
	notifyFocus(a.top().focus, false)
	l := &layer{w: w, opts: opts}
	a.layers = append(a.layers, l)
	if opts.Focus != nil {
		a.setFocus(l, opts.Focus)
	}
	a.ensureFocus(l)
}

// notifyFocus tells w, where it wants to know, that it gained or lost the
// focus.
func notifyFocus(w Focusable, focused bool) {
	if f, ok := w.(Focuser); ok {
		f.FocusChanged(focused)
	}
}

// Pop closes the topmost layer. The root widget is never closed.
func (a *App) Pop() {
	if len(a.layers) <= 1 {
		return
	}
	l := a.top()
	notifyFocus(l.focus, false)
	a.layers = a.layers[:len(a.layers)-1]
	below := a.top()
	kept := below.focus
	if !contains(focusables(below.w), kept) {
		// The widget was told it lost the focus when the layer was pushed.
		below.focus = nil
	}
	a.ensureFocus(below)
	// ensureFocus told a new widget it has the focus; one that kept it is
	// told here.
	if same(kept, below.focus) {
		notifyFocus(below.focus, true)
	}
	if l.opts.OnClose != nil {
		l.opts.OnClose()
	}
}

// Close closes the layer showing w, and the layers above it.
func (a *App) Close(w Widget) {
	for i := len(a.layers) - 1; i > 0; i-- {
		if same(a.layers[i].w, w) {
			for len(a.layers) > i {
				a.Pop()
			}
			return
		}
	}
}

// Layers reports how many layers are pushed over the root widget.
func (a *App) Layers() int { return len(a.layers) - 1 }

// Focused returns the widget with the focus, nil if none can take it.
func (a *App) Focused() Focusable {
	l := a.top()
	a.ensureFocus(l)
	return l.focus
}

// Focus gives the focus to w, which must be part of the topmost layer.
func (a *App) Focus(w Focusable) {
	a.setFocus(a.top(), w)
}

func (a *App) setFocus(l *layer, w Focusable) {
	if same(l.focus, w) {
		return
	}
	// Only the focus of the topmost layer is felt; a layer below learns it
	// has the focus once it is the topmost again.
	active := l == a.top()
	if active {
		notifyFocus(l.focus, false)
	}
	l.focus = w
	if active {
		notifyFocus(w, true)
	}
}

// ensureFocus moves the focus of l to its first focusable widget when the
// widget that had it went or cannot take it any more.
func (a *App) ensureFocus(l *layer) {
	list := focusables(l.w)
	if contains(list, l.focus) {
		return
	}
	if len(list) == 0 {
		a.setFocus(l, nil)
		return
	}
	a.setFocus(l, list[0])
}

// FocusNext moves the focus to the next focusable widget of the topmost
// layer, by delta steps: 1 forward, -1 back.
func (a *App) FocusNext(delta int) {
	l := a.top()
	list := focusables(l.w)
	if len(list) == 0 {
		return
	}
	i := 0
	for j, f := range list {
		if same(f, l.focus) {
			i = j
			break
		}
	}
	i = ((i+delta)%len(list) + len(list)) % len(list)
	a.setFocus(l, list[i])
}

// focusPath is the widgets from the root of the topmost layer down to the
// focused one.
func (a *App) focusPath() []Widget {
	l := a.top()
	a.ensureFocus(l)
	if l.focus == nil {
		return []Widget{l.w}
	}
	if p := pathTo(l.w, l.focus); p != nil {
		return p
	}
	return []Widget{l.w}
}

// takesText reports whether w takes typed characters as text.
func takesText(w Widget) bool {
	t, ok := w.(interface{ TakesText() bool })
	return ok && t.TakesText()
}

// printableKey reports whether a key name types a character.
func printableKey(name string) bool {
	return utf8.RuneCountInString(name) == 1 || name == "space"
}

// ActiveBindings returns the bindings that apply now, the focused widget's
// first, each key once: what the legend shows. While a text field has the
// focus, the bindings of its parents to plain characters are left out,
// since the field takes those keys.
func (a *App) ActiveBindings() []*Binding {
	path := a.focusPath()
	focused := path[len(path)-1]
	text := takesText(focused)
	seen := map[string]bool{}
	var out []*Binding
	add := func(km *Keymap) {
		for _, b := range km.Bindings() {
			var keys []string
			for _, k := range b.Keys {
				if !seen[k] && (!text || !printableKey(k)) {
					keys = append(keys, k)
				}
			}
			if len(keys) == 0 {
				continue
			}
			for _, k := range b.Keys {
				seen[k] = true
			}
			if len(keys) < len(b.Keys) {
				// The keys the field takes are left out of the label.
				short := *b
				short.Keys = keys
				b = &short
			}
			out = append(out, b)
		}
	}
	for _, w := range slices.Backward(path) {
		if b, ok := w.(Bindings); ok {
			add(b.Keymap())
		}
	}
	if len(a.layers) == 1 {
		add(a.keymap)
	} else {
		for _, b := range a.keymap.Bindings() {
			if b.Keys[0] == "ctrl+c" {
				add(&Keymap{bindings: []*Binding{b}})
			}
		}
	}
	return out
}

// HandleKey hands k to the widgets as a key typed: the bindings on the
// focus path from the focused widget up, the focused widget, Tab and Esc
// of the layer, and the application's bindings. It reports whether
// anything took the key.
func (a *App) HandleKey(k Key) bool {
	path := a.focusPath()
	focused := path[len(path)-1]
	name := k.String()
	// A widget that takes text now, such as a field or a list whose filter
	// is open, gets the key before its own bindings, so a binding to "j"
	// does not take the letter typed.
	own := func() bool {
		// A widget that cannot take the focus, standing in for the focus
		// as the root of a layer, takes no keys of its own either.
		if f, ok := focused.(Focusable); ok && !f.CanFocus() {
			return false
		}
		if b, ok := focused.(Bindings); ok {
			if bind := b.Keymap().Lookup(k); bind != nil {
				bind.Action()
				return true
			}
		}
		return false
	}
	typed := func() bool {
		f, ok := focused.(Focusable)
		return ok && f.CanFocus() && f.HandleKey(k)
	}
	if takesText(focused) {
		if typed() || own() {
			return true
		}
	} else if own() || typed() {
		return true
	}
	for i := len(path) - 2; i >= 0; i-- {
		if b, ok := path[i].(Bindings); ok {
			if bind := b.Keymap().Lookup(k); bind != nil {
				bind.Action()
				return true
			}
		}
	}
	switch name {
	case "tab":
		a.FocusNext(1)
		return true
	case "shift+tab":
		a.FocusNext(-1)
		return true
	case "esc":
		if len(a.layers) > 1 && !a.top().opts.NoEscape {
			a.Pop()
			return true
		}
	}
	if len(a.layers) > 1 && name != "ctrl+c" {
		return false
	}
	if bind := a.keymap.Lookup(k); bind != nil {
		bind.Action()
		return true
	}
	return false
}

// handlePaste hands pasted text to the focused widget, or, where it takes no
// paste, as keys.
func (a *App) handlePaste(text string) {
	if p, ok := a.Focused().(Paster); ok && p.HandlePaste(text) {
		return
	}
	for _, r := range text {
		switch r {
		case '\n':
			a.HandleKey(Key{Code: KeyEnter})
		case '\t':
			a.HandleKey(Key{Code: KeyTab})
		default:
			a.HandleKey(Key{Code: KeyRune, Rune: r})
		}
	}
}

// handleMouse gives m to the widget under it, the topmost first. Only the
// widgets of the topmost layer take the mouse: a click beside a dialog
// reaches nothing.
func (a *App) handleMouse(m Mouse) {
	top := a.top().w
	for _, r := range slices.Backward(a.regions) {
		if m.X < r.x || m.Y < r.y || m.X >= r.x+r.w2 || m.Y >= r.y+r.h {
			continue
		}
		if pathTo(top, r.w) == nil {
			continue
		}
		if m.Action == MousePress && m.Button == MouseLeft {
			if f, ok := r.w.(Focusable); ok && f.CanFocus() {
				a.setFocus(a.top(), f)
			}
		}
		if c, ok := r.w.(Clicker); ok {
			rel := m
			rel.X, rel.Y = m.X-r.x, m.Y-r.y
			if c.HandleMouse(rel) {
				return
			}
		}
	}
}

// handle hands one decoded event to the application.
func (a *App) handle(ev any) {
	switch ev := ev.(type) {
	case Key:
		a.HandleKey(ev)
	case Paste:
		a.handlePaste(ev.Text)
	case Mouse:
		a.handleMouse(ev)
	}
}

// feed decodes the bytes read and hands their events over. Bytes that
// start a sequence not complete yet are kept for the next read; more=false
// decodes them as they are.
func (a *App) feed(data []byte, more bool) {
	a.input = append(a.input, data...)
	for len(a.input) > 0 {
		if a.skipPaste {
			end := strings.Index(string(a.input), pasteEnd)
			if end < 0 {
				// The end marker may be split across reads: its start is
				// kept.
				keep := min(len(a.input), len(pasteEnd)-1)
				a.input = append([]byte(nil), a.input[len(a.input)-keep:]...)
				return
			}
			a.input = a.input[end+len(pasteEnd):]
			a.skipPaste = false
			continue
		}
		if !more && pasteOpen(a.input) {
			// A paste that pauses is not cut short: it waits for its end,
			// up to the size a paste may have.
			return
		}
		ev, n := decode(a.input, more)
		if n == 0 {
			return
		}
		a.input = a.input[n:]
		if p, ok := ev.(Paste); ok && p.cut {
			a.skipPaste = true
		}
		if ev != nil {
			a.handle(ev)
		}
	}
	a.input = nil
}

// pasteOpen reports whether buf starts a bracketed paste whose end has
// not arrived yet, and that may still grow.
func pasteOpen(buf []byte) bool {
	s := string(buf)
	return strings.HasPrefix(s, pasteStart) && !strings.Contains(s, pasteEnd) && len(buf) < maxPaste
}

// layerRect is where a layer goes on a screen of w×h.
func layerRect(l *layer, w, h int) (x, y, lw, lh int) {
	lw, lh = l.opts.Width, l.opts.Height
	if s, ok := l.w.(Sizer); ok && (lw == 0 || lh == 0) {
		sw, sh := s.Size(w-2, h-2)
		if lw == 0 {
			lw = sw
		}
		if lh == 0 {
			lh = sh
		}
	}
	if lw <= 0 {
		lw = w * 2 / 3
	}
	if lh <= 0 {
		lh = h * 2 / 3
	}
	lw, lh = min(lw, w), min(lh, h)
	return (w - lw) / 2, (h - lh) / 3, lw, lh
}

// render draws a frame of the whole application.
func (a *App) render(w, h int) (*buffer, *frame) {
	buf := newBuffer(w, h)
	path := a.focusPath()
	f := &frame{buf: buf, theme: a.theme, cursorX: -1, cursorY: -1}
	f.focusPath = path
	f.focus = path[len(path)-1]
	f.app = a
	app := Canvas{f: f, w: w, h: h}
	for i, l := range a.layers {
		if i == 0 {
			app.Draw(l.w)
			continue
		}
		x, y, lw, lh := layerRect(l, w, h)
		c := app.Sub(x, y, lw, lh).WithStyle(a.theme.Dialog)
		c.Fill("")
		if i < len(a.layers)-1 {
			// A layer below the top one draws without the focus.
			saved := f.focusPath
			f.focusPath = nil
			c.Draw(l.w)
			f.focusPath = saved
			continue
		}
		c.Draw(l.w)
	}
	a.regions = f.regions
	return buf, f
}

// moving reports whether the screen changes without an event: a spinner
// turns, a notice is to go or a widget is Animated.
func (a *App) moving() bool {
	if len(a.busy) > 0 || a.notice != nil {
		return true
	}
	for _, l := range a.layers {
		if animating(l.w) {
			return true
		}
	}
	return false
}

// ShowHelp shows every binding that applies now, grouped, in a layer.
func (a *App) ShowHelp() {
	a.Push(newHelp(a), LayerOptions{})
}

// Suspend hands the terminal back to the shell, as Ctrl-Z does in other
// programs, and takes it again once the shell continues the program. It
// does nothing where the system has no job control.
func (a *App) Suspend() { a.suspend = true }

// drainPanics raises again a panic that happened in work started with Go.
func (a *App) drainPanics() {
	select {
	case p := <-a.panics:
		panic(p)
	default:
	}
}

// Run takes the terminal over and runs the application until Quit, and
// puts the terminal back as it found it before returning, also on a panic
// and on a signal that ends the process. It returns ErrNotATerminal where
// input or output is not a terminal.
func (a *App) Run() error {
	in, out := a.in, a.out
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	t, err := openTerminal(in, out, a.mouse)
	if err != nil {
		return err
	}
	defer t.close()
	if !a.themed && !t.colored() {
		a.theme = PlainTheme
	}
	return a.loop(t)
}

// driver is the terminal a loop runs on: a seam for tests.
type driver interface {
	size() (int, int)
	write(s string) error
	input() <-chan []byte
	resized() <-chan struct{}
	suspend()
	colored() bool
}

func (a *App) loop(t driver) error {
	a.ensureFocus(a.root)
	escape := time.NewTimer(time.Hour)
	escape.Stop()
	defer escape.Stop()
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for !a.quit {
		a.drainPanics()
		if err := a.draw(t); err != nil {
			return err
		}
		var tick <-chan time.Time
		if a.moving() {
			tick = ticker.C
		}
		select {
		case data, ok := <-t.input():
			if !ok {
				return ErrInputClosed
			}
			a.feed(data, true)
			if len(a.input) > 0 {
				escape.Reset(escapeGrace)
			}
		case <-escape.C:
			a.feed(nil, false)
		case <-t.resized():
			a.redraw = true
		case <-a.wake:
		case <-tick:
		}
		// Whatever was posted is handled before the next draw.
		a.runPosted()
		if a.suspend {
			a.suspend = false
			t.suspend()
			a.prev = nil
		}
	}
	return a.err
}

// draw draws a frame and writes what changed.
func (a *App) draw(t driver) error {
	w, h := t.size()
	a.width, a.height = w, h
	buf, f := a.render(w, h)
	prev := a.prev
	if a.redraw {
		prev, a.redraw = nil, false
	}
	out := buf.diff(prev, t.colored())
	if f.cursorX >= 0 {
		out += fmt.Sprintf("\x1b[%d;%dH\x1b[?25h", f.cursorY+1, f.cursorX+1)
	} else {
		out += "\x1b[?25l"
	}
	a.prev = buf
	return t.write(out)
}
