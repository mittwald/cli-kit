package progress

import (
	"fmt"
	"io"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"
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

	defaultContrast   = 4.5
	defaultStallDelay = 3 * time.Second
)

// Options configures a [Bar]. The zero value is ready to use.
type Options struct {
	// Width is the bar width in cells. Zero uses the full width of the
	// terminal, or 80 cells when that is unknown.
	Width int
	// Palette holds the bar's colors. Nil uses [DefaultPalette].
	Palette *Palette
	// TextContrast is the minimum WCAG contrast ratio between the label and
	// the gradient behind it. Zero uses 4.5, the WCAG AA level for text.
	TextContrast float64
	// StallDelay is how long an operation started with [Bar.BeginWait] may
	// run before the bar counts down its timeout. Zero uses three seconds.
	StallDelay time.Duration
	// Countdown formats the time left until the timeout. Nil formats it as
	// "(27s)", rounding up so the last second shown is the one being waited
	// out.
	Countdown func(remaining time.Duration) string
	// Force draws the bar even when term.CanAnimate reports that its writer
	// may not be redrawn in place, for demos and tests.
	Force bool
}

// Bar is a progress bar drawn on one terminal line. Create it with [New],
// then call [Bar.Start] once the number of steps is known. All methods are
// safe for concurrent use, and all of them do nothing when the bar is not
// [Bar.Enabled].
type Bar struct {
	w        io.Writer
	enabled  bool
	cursor   *live.Cursor
	now      func() time.Time
	gradient []Color
	track    string
	contrast float64
	stall    time.Duration
	format   func(time.Duration) string
	width    int // Options.Width; 0 asks the terminal at Start

	// life serializes Start, Finish and Clear, which stop the ticker and
	// wait for it, so it is never held by the ticker itself.
	life   sync.Mutex
	ticker *ticker

	// mu guards the drawing state below. The cursor's lock is taken after
	// it, around every write to the terminal.
	mu           sync.Mutex
	active       bool // a frame belongs on screen: started and not cleared
	stopped      bool // Finish was called; the frame no longer moves
	complete     bool // finished successfully: drawn full
	columns      int
	total        int
	current      int
	visual       float64
	label        string
	filledStyles []string
	last         string // the last frame drawn, without the leading carriage return
	shown        bool
	timeout      time.Duration
	waitStart    time.Time // start of the operation waited on; zero when none
	waitID       uint64    // tells a late done call from a newer wait
}

type ticker struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// New returns a bar drawing to w. It draws nothing until [Bar.Start].
func New(w io.Writer, options Options) *Bar {
	palette := DefaultPalette
	if options.Palette != nil {
		palette = *options.Palette
	}
	if len(palette.Gradient) == 0 {
		palette.Gradient = DefaultPalette.Gradient
	}
	b := &Bar{
		w:        w,
		enabled:  w != nil && (options.Force || term.CanAnimate(w)),
		cursor:   live.NewCursor(w),
		now:      time.Now,
		gradient: append([]Color(nil), palette.Gradient...),
		track:    sgr(palette.Track, palette.TrackText),
		contrast: options.TextContrast,
		stall:    options.StallDelay,
		format:   options.Countdown,
		width:    max(options.Width, 0),
	}
	if b.contrast <= 0 {
		b.contrast = defaultContrast
	}
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

	columns := b.width
	if columns == 0 {
		columns = term.Columns(b.w, fallbackWidth)
	}
	b.mu.Lock()
	b.active, b.stopped, b.complete = true, false, false
	b.columns = columns
	b.total = max(total, 1)
	b.current, b.visual, b.label = 0, 0, ""
	b.filledStyles, b.last, b.shown = nil, "", false
	b.timeout = timeout
	b.waitStart = time.Time{}
	b.mu.Unlock()

	// Hide the cursor so it does not blink at the end of the bar.
	b.cursor.Hide()
	b.SetLabel("")
	b.ticker = b.startTicker()
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
// until the timeout given to [Bar.Start]. The returned function is safe to
// call more than once, and a late call does not end a newer wait.
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
	b.waitStart = b.now()
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.waitID == id {
			b.waitStart = time.Time{}
		}
	}
}

// Finish stops the bar and shows the cursor again. On success the bar is
// drawn full; otherwise it stays as it is. Either way the frame stays on
// screen until [Bar.Clear], so call Clear right before writing the result.
func (b *Bar) Finish(success bool) {
	if !b.enabled {
		return
	}
	b.life.Lock()
	defer b.life.Unlock()
	b.stopTickerLocked()
	b.mu.Lock()
	if b.active && !b.stopped {
		b.stopped = true
		if success {
			b.current, b.visual, b.complete = b.total, float64(b.total), true
			b.drawLocked()
		}
	}
	b.mu.Unlock()
	b.cursor.Show()
}

// Clear erases the bar, running or finished, and shows the cursor again.
// Clearing a cleared bar writes nothing.
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
	b.cursor.Show()
}

// WithCleared takes the bar off its line, calls write, and draws the bar
// again below whatever write wrote. Use it for every line written to the
// bar's terminal while the bar is shown. Without a bar shown it just calls
// write. write must not call methods of b.
func (b *Bar) WithCleared(write func()) {
	if !b.enabled {
		write()
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.shown {
		write()
		return
	}
	b.cursor.Lock()
	defer b.cursor.Unlock()
	b.eraseOutputLocked()
	write()
	b.renderOutputLocked()
}

// Writer returns a writer that writes to w through [Bar.WithCleared], for
// use as a logger's output. Each write should be one or more whole lines.
func (b *Bar) Writer(w io.Writer) io.Writer {
	return clearingWriter{bar: b, w: w}
}

type clearingWriter struct {
	bar *Bar
	w   io.Writer
}

// Write writes p to the underlying writer with the bar taken off its line.
func (c clearingWriter) Write(p []byte) (n int, err error) {
	c.bar.WithCleared(func() { n, err = c.w.Write(p) })
	return n, err
}

// Unwrap returns the underlying writer.
func (c clearingWriter) Unwrap() io.Writer { return c.w }

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
		_, _ = io.WriteString(b.w, "\r\x1b[K")
		b.shown = false
		b.last = ""
	}
}

// renderOutputLocked draws the frame with b.mu and the cursor lock held. A
// frame identical to the one on screen is not drawn again, so the bar does
// not flicker.
func (b *Bar) renderOutputLocked() {
	if !b.active {
		return
	}
	body := b.frameLocked()
	if body == b.last {
		return
	}
	_, _ = io.WriteString(b.w, "\r"+body)
	b.last = body
	b.shown = true
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

// countdownLocked returns the time left until the timeout once the
// operation waited on has run for the stall delay. The age of that
// operation is what counts, not the time since the last step: a loop of
// many quick requests within one step must not blink the countdown on and
// off.
func (b *Bar) countdownLocked() string {
	if b.complete || b.timeout <= 0 || b.waitStart.IsZero() {
		return ""
	}
	waiting := b.now().Sub(b.waitStart)
	if waiting < b.stall {
		return ""
	}
	return b.format(max(b.timeout-waiting, 0))
}

// paint colors text cell by cell: the first filled cells in the gradient,
// the rest in the track colors.
func (b *Bar) paint(text string, filled int) string {
	if len(b.filledStyles) != b.columns {
		b.filledStyles = make([]string, b.columns)
		for column := range b.columns {
			background := gradientAt(b.gradient, column, b.columns)
			b.filledStyles[column] = sgr(background, textColor(background, b.contrast))
		}
	}
	var body strings.Builder
	body.Grow(len(text) + b.columns*32)
	column := 0
	lastStyle := ""
	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		cluster := graphemes.Str()
		style := b.track
		if column < filled && b.columns > 0 {
			style = b.filledStyles[min(column, b.columns-1)]
		}
		if style != lastStyle {
			body.WriteString("\x1b[" + style + "m")
			lastStyle = style
		}
		body.WriteString(cluster)
		column += runewidth.StringWidth(cluster)
	}
	body.WriteString("\x1b[0m")
	return body.String()
}

// fitWidth pads or cuts s to exactly width cells.
func fitWidth(s string, width int) string {
	return term.PadRight(term.Truncate(s, width), width)
}
