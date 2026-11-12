package progress

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rivo/uniseg"

	"github.com/mittwald/cli-kit/live"
	"github.com/mittwald/cli-kit/term"
)

const (
	// fallbackWidth is the bar width when the terminal width is unknown.
	fallbackWidth = 80
	// capFraction keeps an unfinished bar from ever looking complete.
	capFraction = 0.97
	// tickInterval is how often the fill creeps on and the countdown ticks.
	tickInterval = 100 * time.Millisecond
	// columnsPerTick is how far the fill creeps on per tick.
	columnsPerTick = 0.35
	// countdownGap separates the label from the countdown.
	countdownGap = 1
	// maxHeld is the longest partial line a writer holds back for its
	// newline; anything longer is written as it is.
	maxHeld = 64 << 10

	defaultContrast   = 4.5
	defaultStallDelay = 3 * time.Second
)

// Options configures a [Bar]. The zero value is ready to use.
type Options struct {
	// Width is the most cells the bar takes up. The bar is never wider than
	// the terminal and follows its width when the window changes, so a
	// Width beyond the terminal's, like zero, uses the full width of the
	// terminal. Where the terminal's width is unknown the bar is Width
	// cells wide, or 80 when Width is zero.
	Width int
	// Palette holds the bar's colors. Nil uses [DefaultPalette].
	Palette *Palette
	// TextContrast is the WCAG contrast ratio the label gets against the
	// gradient behind it, where the gradient allows it: see
	// [Palette.Gradient]. Zero uses 4.5, the WCAG AA level for text.
	TextContrast float64
	// StallDelay is how long an operation started with [Bar.BeginWait] may
	// run before the bar counts down its timeout. Zero uses three seconds.
	StallDelay time.Duration
	// Countdown formats the time left until the timeout. Nil formats it as
	// "(27s)", rounding up so the last second shown is the one being waited
	// out. Its result is shown as term.PlainLine leaves it. Countdown is
	// called while the bar is being drawn and must not call the bar's
	// methods.
	Countdown func(remaining time.Duration) string
	// Force draws the bar even when term.CanAnimate reports that its writer
	// may not be redrawn in place, for demos and tests.
	Force bool
}

// Bar is a progress bar drawn on one terminal line. Create it with [New],
// then call [Bar.Start] once the number of steps is known. All methods are
// safe for concurrent use, and all of them do nothing when the bar is not
// [Bar.Enabled], except that [Bar.WithCleared] and the writers of
// [Bar.Writer] still write what they are given.
type Bar struct {
	w       io.Writer
	enabled bool
	cursor  *live.Cursor
	now     func() time.Time
	// terminalWidth reports how wide the terminal is now, 0 when unknown.
	terminalWidth func() int
	gradient      []Color
	trackColors   [2]Color // background and text of the part not yet filled
	truecolor     bool     // the terminal takes 24-bit colors
	darker        bool     // the label is darker than the gradient, not lighter
	contrast      float64
	stall         time.Duration
	format        func(time.Duration) string
	limit         int // Options.Width; 0 sets no limit

	// life serializes Start, Finish and Clear, which stop the ticker and
	// wait for it, so it is never held by the ticker itself.
	life   sync.Mutex
	ticker *ticker

	// mu guards the drawing state below. The cursor's lock is taken after
	// it, around every write of a frame to the terminal. Neither is held
	// while a callback of WithCleared or a writer of Writer writes.
	mu           sync.Mutex
	active       bool // a frame belongs on screen: started and not cleared
	stopped      bool // Finish was called; the frame no longer moves
	complete     bool // finished successfully: drawn full
	columns      int  // cells the frame takes, as of the last draw
	total        int
	current      int
	visual       float64
	label        string
	filledStyles []string
	last         string // the last frame drawn, without the leading carriage return
	shown        bool
	shownWidth   int // the terminal's width when the frame on screen was drawn
	cleared      int // calls of WithCleared in progress; nothing is drawn meanwhile
	timeout      time.Duration
	waits        map[uint64]time.Time // the operations waited on, and when each began
	waitID       uint64               // the last key given out for waits
	countdown    string               // the countdown a finished bar keeps showing
	held         []heldLine           // the partial lines the writers of Writer hold back
}

type ticker struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// New returns a bar drawing to w. It draws nothing until [Bar.Start].
//
// The bar paints 24-bit colors where the COLORTERM environment variable
// announces them, as "truecolor" or "24bit", and the nearest of the 256
// colors everywhere else, where a 24-bit color could come out as anything.
func New(w io.Writer, options Options) *Bar {
	palette := DefaultPalette
	if options.Palette != nil {
		palette = *options.Palette
	}
	if len(palette.Gradient) == 0 {
		palette.Gradient = DefaultPalette.Gradient
	}
	b := &Bar{
		w:             w,
		enabled:       w != nil && (options.Force || term.CanAnimate(w)),
		cursor:        live.NewCursor(w),
		now:           time.Now,
		terminalWidth: func() int { return term.Columns(w, 0) },
		gradient:      append([]Color(nil), palette.Gradient...),
		trackColors:   [2]Color{palette.Track, palette.TrackText},
		truecolor:     truecolor(os.Getenv("COLORTERM")),
		contrast:      options.TextContrast,
		stall:         options.StallDelay,
		format:        options.Countdown,
		limit:         max(options.Width, 0),
		waits:         map[uint64]time.Time{},
	}
	if b.contrast <= 0 {
		b.contrast = defaultContrast
	}
	b.darker = darkerText(b.gradient, b.contrast)
	if b.stall <= 0 {
		b.stall = defaultStallDelay
	}
	if b.format == nil {
		b.format = formatCountdown
	}
	return b
}

func formatCountdown(remaining time.Duration) string {
	return fmt.Sprintf("(%ds)", int(math.Ceil(remaining.Seconds())))
}

// Enabled reports whether the bar draws at all.
func (b *Bar) Enabled() bool { return b.enabled }

// Start shows the bar for total steps, replacing a bar already shown. The
// timeout is the time an operation started with [Bar.BeginWait] may take;
// the bar counts it down once the operation stalls. A timeout of 0 never
// shows a countdown. Start hides the cursor until [Bar.Finish] or
// [Bar.Clear].
func (b *Bar) Start(total int, timeout time.Duration) {
	if !b.enabled {
		return
	}
	b.life.Lock()
	defer b.life.Unlock()
	b.clearLocked()

	b.mu.Lock()
	b.active, b.stopped, b.complete = true, false, false
	b.columns, _ = b.measure()
	b.total = max(total, 1)
	b.current, b.visual, b.label = 0, 0, ""
	b.filledStyles, b.last, b.shown = nil, "", false
	b.timeout, b.countdown = timeout, ""
	clear(b.waits)
	b.mu.Unlock()

	// Hide the cursor so it does not blink at the end of the bar.
	b.cursor.Hide()
	b.SetLabel("")
	b.ticker = b.startTicker()
}

// measure returns how many cells a frame takes now, and how wide the
// terminal is, 0 when that is unknown. The frame is as wide as the terminal
// and no wider than Options.Width.
func (b *Bar) measure() (columns, terminal int) {
	terminal = max(b.terminalWidth(), 0)
	switch {
	case terminal == 0 && b.limit == 0:
		return fallbackWidth, 0
	case terminal == 0:
		return b.limit, 0
	case b.limit == 0:
		return terminal, terminal
	}
	return min(terminal, b.limit), terminal
}

// Advance counts one step as done and shows label as the step now being
// worked on. The label is shown as term.PlainLine leaves it, on the bar's
// one line and without escape sequences, so a label taken from untrusted
// input cannot move the cursor, change the window title or break the bar
// onto a second line.
func (b *Bar) Advance(label string) {
	b.update(label, true)
}

// SetLabel shows label without counting a step, made plain as [Bar.Advance]
// makes it.
func (b *Bar) SetLabel(label string) {
	b.update(label, false)
}

func (b *Bar) update(label string, step bool) {
	if !b.enabled {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.active || b.stopped {
		return
	}
	if step && b.current < b.total {
		b.current++
	}
	b.visual = max(b.visual, float64(b.current))
	b.label = term.PlainLine(label)
	b.drawLocked()
}

// BeginWait marks the start of an operation that may stall, such as a
// network request, and returns the function that marks its end. Once the
// operation has run for the stall delay, the bar counts down the time left
// until the timeout given to [Bar.Start]. Operations may overlap: the
// countdown is that of the oldest one still under way, the one whose
// timeout comes first. The returned function is safe to call more than
// once, and it ends no wait but its own.
func (b *Bar) BeginWait() (done func()) {
	if !b.enabled {
		return func() {}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.active || b.stopped {
		return func() {}
	}
	b.waitID++
	id := b.waitID
	b.waits[id] = b.now()
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.waits, id)
	}
}

// Finish stops the bar and shows the cursor again. On success the bar is
// drawn full; otherwise it stays as it is, with the countdown it showed.
// Either way the frame stays on screen, unchanged by whatever is written
// through [Bar.WithCleared] afterwards, until [Bar.Clear], so call Clear
// right before writing the result. A partial line that a writer of
// [Bar.Writer] holds back is written above the bar as a line of its own.
func (b *Bar) Finish(success bool) {
	if !b.enabled {
		return
	}
	b.life.Lock()
	defer b.life.Unlock()
	b.stopTickerLocked()
	b.mu.Lock()
	finishing := b.active && !b.stopped
	if finishing {
		// The countdown stops with the bar: a later redraw shows the frame
		// as it is now, not the time that has passed since.
		b.countdown = ""
		if success {
			b.current, b.visual, b.complete = b.total, float64(b.total), true
		} else {
			b.countdown = b.countdownLocked()
		}
		b.stopped = true
		// The last frame is drawn below the lines the writers held back.
		b.cleared++
	}
	b.mu.Unlock()
	b.flushHeld(true)
	if finishing {
		b.mu.Lock()
		b.cleared--
		b.drawLocked()
		b.mu.Unlock()
	}
	b.cursor.Show()
}

// Clear erases the bar, running or finished, and shows the cursor again.
// A partial line that a writer of [Bar.Writer] holds back is written where
// the bar was, as it is. Clearing a cleared bar writes nothing.
func (b *Bar) Clear() {
	if !b.enabled {
		return
	}
	b.life.Lock()
	defer b.life.Unlock()
	b.clearLocked()
}

// clearLocked is Clear with b.life held.
func (b *Bar) clearLocked() {
	b.stopTickerLocked()
	b.mu.Lock()
	b.eraseLocked()
	b.active = false
	b.mu.Unlock()
	b.flushHeld(false)
	b.cursor.Show()
}

// WithCleared takes the bar off its line, calls write, and draws the bar
// again below whatever write wrote. Use it for every line written to the
// bar's terminal while the bar is shown. Without a bar shown it just calls
// write.
//
// The bar is not drawn while write runs, and none of its locks are held:
// write may call the bar's methods and may write through a writer of
// [Bar.Writer], as a logger set up with one does. Calls from several
// goroutines run write at the same time; what they write is theirs to keep
// in order.
func (b *Bar) WithCleared(write func()) {
	if !b.enabled {
		write()
		return
	}
	b.mu.Lock()
	b.cleared++
	b.eraseLocked()
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.cleared--
		b.drawLocked()
	}()
	write()
}

// Writer returns a writer that writes to w through [Bar.WithCleared], for
// use as a logger's output or as the output of a command that is run.
//
// The bar is drawn below whole lines only, so what follows the last newline
// of a write is held back until the rest of that line arrives: drawn right
// away, the bar would cover it. The line is put together from every writer
// the bar returned for w, where w is a pointer such as an *os.File. A line
// still held back is written by [Bar.Finish] above the finished bar, ended
// with a newline, by [Bar.Clear] as it is, and by the writer itself once
// it has grown to 64 KiB. Write reports the bytes it was given as written,
// held back or not. Nothing is held back while no bar is on screen or when
// the bar is not [Bar.Enabled].
func (b *Bar) Writer(w io.Writer) io.Writer {
	l := &lineWriter{bar: b, w: w}
	l.line = l
	// Only a pointer is safe to compare with any other writer.
	if w != nil && reflect.ValueOf(w).Kind() == reflect.Pointer {
		l.line = w
	}
	return l
}

// lineWriter is the writer of Bar.Writer.
type lineWriter struct {
	bar *Bar
	w   io.Writer
	// line is what the partial line held back is kept under: the
	// underlying writer where writers can be told apart by it, so that all
	// writers for one stream build one line, otherwise this writer.
	line any
}

// heldLine is the start of a line that was written without its end.
type heldLine struct {
	line any
	w    io.Writer
	text []byte
}

// Write writes the whole lines of p, and of what was held back before it,
// to the underlying writer with the bar taken off its line.
func (l *lineWriter) Write(p []byte) (int, error) {
	b := l.bar
	if !b.enabled {
		return l.w.Write(p)
	}
	held := b.takeHeld(l.line)
	data := p
	if len(held) > 0 {
		data = append(held, p...)
	}
	whole := bytes.LastIndexByte(data, '\n') + 1
	if rest := data[whole:]; len(rest) > 0 && (len(rest) > maxHeld || !b.hold(l, rest)) {
		whole = len(data)
	}
	if whole == 0 {
		return len(p), nil
	}
	var (
		n   int
		err error
	)
	b.WithCleared(func() { n, err = l.w.Write(data[:whole]) })
	if err != nil {
		// What was not written is not kept either: the caller is told how
		// much of p went out and decides about the rest.
		b.takeHeld(l.line)
		return min(max(n-len(held), 0), len(p)), err
	}
	return len(p), nil
}

// Unwrap returns the underlying writer.
func (l *lineWriter) Unwrap() io.Writer { return l.w }

// hold keeps text, the start of a line written through l, until the rest
// of the line arrives or Finish or Clear write it. It reports false when no
// frame is on screen that could cover the text, so there is no reason to
// hold it back. The text is copied: it is the caller's again once its Write
// returns.
func (b *Bar) hold(l *lineWriter, text []byte) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.active {
		return false
	}
	for i := range b.held {
		if b.held[i].line == l.line {
			// Another write got in between: its text comes first.
			b.held[i].text = append(b.held[i].text, text...)
			return true
		}
	}
	b.held = append(b.held, heldLine{line: l.line, w: l.w, text: bytes.Clone(text)})
	return true
}

// takeHeld returns the text held back under line and forgets it.
func (b *Bar) takeHeld(line any) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, held := range b.held {
		if held.line == line {
			b.held = slices.Delete(b.held, i, i+1)
			return held.text
		}
	}
	return nil
}

// flushHeld writes every partial line held back, in the order the lines
// were begun, each ended with a newline when terminate is set.
func (b *Bar) flushHeld(terminate bool) {
	b.mu.Lock()
	lines := b.held
	b.held = nil
	b.mu.Unlock()
	for _, held := range lines {
		text := held.text
		if terminate {
			text = append(text, '\n')
		}
		b.WithCleared(func() { _, _ = held.w.Write(text) })
	}
}

func (b *Bar) startTicker() *ticker {
	t := &ticker{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer b.cursor.RestoreOnPanic()
		clock := time.NewTicker(tickInterval)
		defer func() {
			clock.Stop()
			close(t.done)
		}()
		for {
			select {
			case <-clock.C:
				b.tick()
			case <-t.stop:
				return
			}
		}
	}()
	return t
}

// stopTickerLocked stops the ticker and waits for it, with b.life held.
func (b *Bar) stopTickerLocked() {
	t := b.ticker
	if t == nil {
		return
	}
	t.once.Do(func() { close(t.stop) })
	<-t.done
	b.ticker = nil
}

// tick creeps the fill toward the next step and redraws, so a countdown
// keeps ticking even when the fill has reached its ceiling.
func (b *Bar) tick() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.active || b.stopped || b.columns <= 0 {
		return
	}
	target := min(float64(b.current)+0.9, float64(b.total)*capFraction)
	if b.visual < target {
		step := columnsPerTick * float64(b.total) / float64(b.columns)
		b.visual = min(b.visual+step, target)
	}
	b.drawLocked()
}

// drawLocked draws the current frame with b.mu held.
func (b *Bar) drawLocked() {
	b.cursor.Lock()
	defer b.cursor.Unlock()
	b.renderOutputLocked()
}

// eraseLocked erases the frame with b.mu held.
func (b *Bar) eraseLocked() {
	b.cursor.Lock()
	defer b.cursor.Unlock()
	b.eraseOutputLocked()
}

// eraseOutputLocked erases the frame with b.mu and the cursor lock held.
func (b *Bar) eraseOutputLocked() {
	if b.shown {
		_, terminal := b.measure()
		_, _ = io.WriteString(b.w, b.homeLocked(terminal, true))
		b.shown = false
		b.last = ""
	}
}

// homeLocked returns the sequence that moves the cursor from behind the
// frame on screen to its first cell, and erases the frame when erase is set.
//
// A frame drawn before the terminal was made narrower than it is has been
// wrapped onto further rows by the terminal. Those rows are counted at the
// width the terminal has now, moved up over, and erased in any case, so no
// part of the old frame stays behind.
func (b *Bar) homeLocked(terminal int, erase bool) string {
	rows := 1
	if b.shown && terminal > 0 && terminal != b.shownWidth {
		rows = term.ScreenRows([]string{b.last}, terminal)
	}
	switch {
	case rows > 1:
		return fmt.Sprintf("\r\x1b[%dA\x1b[J", rows-1)
	case erase:
		return "\r\x1b[K"
	}
	return "\r"
}

// renderOutputLocked draws the frame with b.mu and the cursor lock held, as
// wide as the terminal is now. A frame identical to the one on screen is
// not drawn again, so the bar does not flicker.
func (b *Bar) renderOutputLocked() {
	if !b.active || b.cleared > 0 {
		return
	}
	columns, terminal := b.measure()
	b.columns = columns
	body := b.frameLocked()
	if body == b.last {
		// The frame on screen is no wider than the terminal is now, so the
		// terminal has not wrapped it.
		b.shownWidth = terminal
		return
	}
	_, _ = io.WriteString(b.w, b.homeLocked(terminal, false)+body)
	b.last = body
	b.shown = true
	b.shownWidth = terminal
}

// frameLocked renders the bar, without the leading carriage return.
func (b *Bar) frameLocked() string {
	fraction := b.visual / float64(b.total)
	if b.complete {
		fraction = 1
	} else {
		fraction = min(fraction, capFraction)
	}
	filled := min(int(fraction*float64(b.columns)+0.5), b.columns)
	if !b.complete {
		// Rounding fills a narrow bar to its last cell; only a finished bar
		// is drawn full.
		filled = max(min(filled, b.columns-1), 0)
	}
	// Keep a one-cell inset on each side so the text never touches the
	// edges, even when it is cut to the full width.
	inner := b.columns
	if b.columns >= 3 {
		inner = b.columns - 2
	}
	text := fitWidth(b.labelWithCountdownLocked(inner), inner)
	if b.columns >= 3 {
		text = " " + text + " "
	}
	return b.paint(text, filled)
}

// labelWithCountdownLocked appends the countdown to the label, cutting the
// label short when both do not fit into width cells. When even a cut label
// does not fit, the label wins.
func (b *Bar) labelWithCountdownLocked(width int) string {
	countdown := b.countdownLocked()
	if countdown == "" {
		return b.label
	}
	room := width - term.VisibleWidth(countdown) - countdownGap
	if room < 1 {
		return b.label
	}
	return term.Truncate(b.label, room) + strings.Repeat(" ", countdownGap) + countdown
}

// countdownLocked returns the time left until the timeout once the oldest
// operation waited on has run for the stall delay. The age of that
// operation is what counts, not the time since the last step: a loop of
// many quick requests within one step must not blink the countdown on and
// off. A finished bar keeps the countdown it had.
func (b *Bar) countdownLocked() string {
	if b.stopped {
		return b.countdown
	}
	if b.complete || b.timeout <= 0 || len(b.waits) == 0 {
		return ""
	}
	var oldest time.Time
	for _, began := range b.waits {
		if oldest.IsZero() || began.Before(oldest) {
			oldest = began
		}
	}
	waiting := b.now().Sub(oldest)
	if waiting < b.stall {
		return ""
	}
	return term.PlainLine(b.format(max(b.timeout-waiting, 0)))
}

// paint colors text cell by cell: the first filled cells in the gradient,
// the rest in the track colors.
func (b *Bar) paint(text string, filled int) string {
	if len(b.filledStyles) != b.columns {
		b.filledStyles = make([]string, b.columns)
		for column := range b.columns {
			background := gradientAt(b.gradient, column, b.columns)
			b.filledStyles[column] = sgr(background, textColor(background, b.contrast, b.darker), b.truecolor)
		}
	}
	track := sgr(b.trackColors[0], b.trackColors[1], b.truecolor)
	var body strings.Builder
	body.Grow(len(text) + b.columns*32)
	column := 0
	lastStyle := ""
	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		cluster := graphemes.Str()
		style := track
		if column < filled && b.columns > 0 {
			style = b.filledStyles[min(column, b.columns-1)]
		}
		if style != lastStyle {
			body.WriteString("\x1b[" + style + "m")
			lastStyle = style
		}
		body.WriteString(cluster)
		column += term.WidthAt(cluster, column)
	}
	body.WriteString("\x1b[0m")
	return body.String()
}

// fitWidth pads or cuts s to exactly width cells.
func fitWidth(s string, width int) string {
	return term.PadRight(term.Truncate(s, width), width)
}
