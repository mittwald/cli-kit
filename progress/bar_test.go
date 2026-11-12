package progress

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mittwald/cli-kit/live"
	"github.com/mittwald/cli-kit/term"
)

// testBar returns an enabled bar that is drawn as if started, without the
// ticker goroutine, so a test can drive every step itself.
func testBar(w io.Writer, columns, total int) *Bar {
	b := New(w, Options{Width: columns, Force: true})
	b.active, b.columns, b.total = true, columns, total
	return b
}

// fakeClock pins the bar's clock to an instant the test moves on.
func fakeClock(b *Bar) (start time.Time, advance func(time.Duration)) {
	start = time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	now := start
	var mu sync.Mutex
	b.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	return start, func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(d)
	}
}

func visible(body string) string {
	return term.StripANSI(strings.TrimPrefix(body, "\r"))
}

// lastFrame reads the frame on screen while the ticker may be drawing.
func lastFrame(b *Bar) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.last
}

// TestMain runs the tests on a terminal that announces 24-bit colors, so
// the frames they look at do not depend on the terminal they are run in.
func TestMain(m *testing.M) {
	if err := os.Setenv("COLORTERM", "truecolor"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func trackStyle() string { return sgr(DefaultPalette.Track, DefaultPalette.TrackText, true) }

func filledStyle(column, width int) string {
	background := gradientAt(DefaultPalette.Gradient, column, width)
	return sgr(background, textColor(background, defaultContrast, false), true)
}

var countdownPattern = regexp.MustCompile(`\(\d+s\)`)

func TestFitWidthPadsAndCuts(t *testing.T) {
	if got := fitWidth("hi", 6); got != "hi    " {
		t.Fatalf("pad = %q, want %q", got, "hi    ")
	}
	got := fitWidth("Loading package index", 10)
	if term.VisibleWidth(got) != 10 {
		t.Fatalf("cut width = %d, want 10 (%q)", term.VisibleWidth(got), got)
	}
	if !strings.HasSuffix(got, term.Ellipsis) {
		t.Fatalf("cut text should end with an ellipsis: %q", got)
	}
}

func TestRenderFillsAndContrasts(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 10, 10)
	b.current = 5
	b.SetLabel("Working")

	out := buf.String()
	if !strings.HasPrefix(out, "\r") {
		t.Fatalf("bar should start with a carriage return: %q", out)
	}
	// The gradient fill and the track are both present.
	for _, want := range []string{filledStyle(0, 10), filledStyle(4, 10), trackStyle()} {
		if !strings.Contains(out, want) {
			t.Fatalf("bar missing SGR %q:\n%q", want, out)
		}
	}
	// The filled part comes before the track.
	if strings.Index(out, filledStyle(0, 10)) > strings.Index(out, trackStyle()) {
		t.Fatalf("filled part should come before the track:\n%q", out)
	}
	if got := term.VisibleWidth(visible(out)); got != 10 {
		t.Fatalf("visible bar width = %d, want 10 (%q)", got, visible(out))
	}
}

func TestGradientAndAdaptiveText(t *testing.T) {
	var previous Color
	for index, want := range DefaultPalette.Gradient {
		column := index * 20
		if got := gradientAt(DefaultPalette.Gradient, column, 101); got != want {
			t.Fatalf("gradient stop %d at column %d = %#v, want %#v", index, column, got, want)
		}
		text := textColor(want, defaultContrast, false)
		if ratio := contrastRatio(want, text); ratio < defaultContrast-0.01 {
			t.Fatalf("gradient stop %d text contrast = %.2f, want at least %.2f", index, ratio, defaultContrast)
		}
		backgroundHSL := toHSL(want)
		textHSL := toHSL(text)
		hueDifference := math.Abs(backgroundHSL.hue - textHSL.hue)
		hueDifference = min(hueDifference, 1-hueDifference)
		if hueDifference > 0.02 || math.Abs(backgroundHSL.saturation-textHSL.saturation) > 0.08 {
			t.Fatalf("gradient stop %d text lost its background hue: background %#v, text %#v", index, backgroundHSL, textHSL)
		}
		if index > 0 && text == previous {
			t.Fatalf("gradient stops %d and %d share the text color %#v", index-1, index, text)
		}
		previous = text
	}
}

func TestSingleColorGradient(t *testing.T) {
	gradient := []Color{{R: 10, G: 20, B: 30}}
	for _, column := range []int{0, 5, 9} {
		if got := gradientAt(gradient, column, 10); got != gradient[0] {
			t.Fatalf("column %d = %#v, want %#v", column, got, gradient[0])
		}
	}
	var buf bytes.Buffer
	b := New(&buf, Options{Width: 10, Force: true, Palette: &Palette{Gradient: gradient}})
	b.active, b.columns, b.total, b.current = true, 10, 2, 1
	b.SetLabel("x")
	if !strings.Contains(buf.String(), "48;2;10;20;30;") {
		t.Fatalf("custom gradient not used: %q", buf.String())
	}
}

func TestStartUsesWidth(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, Options{Width: 173, Force: true})
	b.Start(4, 0)
	defer b.Clear()
	if got := term.VisibleWidth(visible(lastFrame(b))); got != 173 {
		t.Fatalf("bar width = %d, want 173", got)
	}

	fallback := New(&buf, Options{Force: true})
	fallback.Start(4, 0)
	defer fallback.Clear()
	if got := term.VisibleWidth(visible(lastFrame(fallback))); got != fallbackWidth {
		t.Fatalf("fallback width = %d, want %d", got, fallbackWidth)
	}
}

func TestNoRedrawWhenUnchanged(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 12, 4)
	b.current = 2
	b.SetLabel("Same")
	n := buf.Len()
	b.SetLabel("Same") // the same frame must not be written again
	if buf.Len() != n {
		t.Fatalf("identical frame was written again: grew from %d to %d", n, buf.Len())
	}
}

func TestTickCreepsTowardNextStep(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 10, 10)
	b.current, b.visual, b.label = 2, 2, "Waiting"
	b.tick()

	if b.visual <= 2 || b.visual >= 2.9 {
		t.Fatalf("visual progress = %v, want between step 2 and ceiling 2.9", b.visual)
	}
	if buf.Len() == 0 {
		t.Fatal("tick did not redraw")
	}
	for range 20 {
		b.tick()
	}
	if b.visual != 2.9 {
		t.Fatalf("visual progress = %v, want ceiling 2.9", b.visual)
	}
	if b.current != 2 {
		t.Fatalf("tick changed the real step from 2 to %d", b.current)
	}
}

func TestCountdownStartsAfterStall(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 60, 10)
	b.timeout = 30 * time.Second
	_, advance := fakeClock(b)
	b.Advance("Downloading package index")
	done := b.BeginWait()

	advance(defaultStallDelay - 100*time.Millisecond)
	b.tick()
	if got := visible(b.last); countdownPattern.MatchString(got) {
		t.Fatalf("countdown appeared before the stall delay: %q", got)
	}

	advance(600 * time.Millisecond) // 3.5s into the wait
	b.tick()
	if got, want := visible(b.last), "Downloading package index (27s)"; !strings.Contains(got, want) {
		t.Fatalf("stalled bar = %q, want %q", got, want)
	}
	if got := term.VisibleWidth(visible(b.last)); got != 60 {
		t.Fatalf("stalled bar width = %d, want 60", got)
	}

	advance(time.Second)
	b.tick()
	if got, want := visible(b.last), "(26s)"; !strings.Contains(got, want) {
		t.Fatalf("countdown did not tick down: %q, want %q", got, want)
	}

	advance(time.Minute) // past the timeout: stop at zero
	b.tick()
	if got, want := visible(b.last), "(0s)"; !strings.Contains(got, want) {
		t.Fatalf("expired countdown = %q, want %q", got, want)
	}

	// A finished wait removes the countdown, a new step resets the stall.
	done()
	b.tick()
	if got := visible(b.last); countdownPattern.MatchString(got) {
		t.Fatalf("countdown survived the finished wait: %q", got)
	}
	done2 := b.BeginWait()
	defer done2()
	b.Advance("Downloading archive")
	b.tick()
	if got := visible(b.last); countdownPattern.MatchString(got) {
		t.Fatalf("countdown shown right after a step: %q", got)
	}
}

func TestCountdownCutsLabelOnNarrowBars(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 30, 10)
	b.timeout = 30 * time.Second
	_, advance := fakeClock(b)
	b.Advance("Downloading package index for 2026-08-03")
	done := b.BeginWait()
	defer done()
	advance(defaultStallDelay)
	b.tick()

	got := visible(b.last)
	if !strings.Contains(got, term.Ellipsis+" (27s)") {
		t.Fatalf("narrow bar = %q, want a cut label followed by the countdown", got)
	}
	if width := term.VisibleWidth(got); width != 30 {
		t.Fatalf("narrow bar width = %d, want 30 (%q)", width, got)
	}

	// Too narrow for both: the label keeps the line.
	b.terminalWidth = func() int { return 6 }
	b.tick()
	if got := visible(b.last); countdownPattern.MatchString(got) {
		t.Fatalf("countdown squeezed out the label on a 6-cell bar: %q", got)
	}
}

func TestCountdownNeedsWaitAndTimeout(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 40, 4)
	b.timeout = 30 * time.Second
	_, advance := fakeClock(b)
	b.Advance("Waiting")
	advance(10 * time.Second)

	b.mu.Lock()
	got := b.countdownLocked()
	b.mu.Unlock()
	if got != "" {
		t.Fatalf("countdown = %q without a wait, want none", got)
	}

	b.timeout = 0
	done := b.BeginWait()
	defer done()
	advance(10 * time.Second)
	b.mu.Lock()
	got = b.countdownLocked()
	b.mu.Unlock()
	if got != "" {
		t.Fatalf("countdown = %q without a timeout, want none", got)
	}
}

// Poll loops start many quick waits within one step. None of them may show
// the countdown.
func TestCountdownStaysHiddenThroughQuickWaits(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 60, 10)
	b.timeout = 30 * time.Second
	_, advance := fakeClock(b)
	b.Advance("Waiting for the build")

	for range 12 { // 12 × (200ms wait + 300ms pause): well past the stall delay
		done := b.BeginWait()
		advance(200 * time.Millisecond)
		b.tick()
		if got := visible(b.last); countdownPattern.MatchString(got) {
			t.Fatalf("quick wait showed a countdown: %q", got)
		}
		done()
		advance(300 * time.Millisecond)
		b.tick()
		if got := visible(b.last); countdownPattern.MatchString(got) {
			t.Fatalf("pause between waits showed a countdown: %q", got)
		}
	}
}

func TestCustomCountdownAndStallDelay(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, Options{
		Width:      40,
		Force:      true,
		StallDelay: time.Second,
		Countdown:  func(remaining time.Duration) string { return fmt.Sprintf("[%v left]", remaining) },
	})
	b.active, b.columns, b.total, b.timeout = true, 40, 4, 10*time.Second
	_, advance := fakeClock(b)
	b.Advance("Uploading")
	done := b.BeginWait()
	defer done()
	advance(time.Second)
	b.tick()
	if got, want := visible(b.last), "Uploading [9s left]"; !strings.Contains(got, want) {
		t.Fatalf("bar = %q, want %q", got, want)
	}
}

// Operations may overlap, as parallel requests do. The countdown is that of
// the oldest one still under way, and the end of one wait ends no other.
func TestCountdownFollowsTheOldestOpenWait(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 60, 10)
	b.timeout = 30 * time.Second
	_, advance := fakeClock(b)
	b.Advance("Two requests")

	first := b.BeginWait()
	advance(10 * time.Second)
	second := b.BeginWait() // a newer wait does not start the stall delay again
	b.tick()
	if got, want := visible(b.last), "(20s)"; !strings.Contains(got, want) {
		t.Fatalf("bar = %q, want the first wait's %q", got, want)
	}

	advance(time.Second)
	second()
	second() // twice is once
	advance(14 * time.Second)
	b.tick()
	if got, want := visible(b.last), "(5s)"; !strings.Contains(got, want) {
		t.Fatalf("bar = %q, want %q: the first wait is still under way", got, want)
	}

	// With the oldest wait over, the next one counts.
	third := b.BeginWait()
	advance(4 * time.Second)
	first()
	b.tick()
	if got, want := visible(b.last), "(26s)"; !strings.Contains(got, want) {
		t.Fatalf("bar = %q, want the remaining wait's %q", got, want)
	}
	third()
	first() // a late call ends nothing
	b.tick()
	if got := visible(b.last); countdownPattern.MatchString(got) {
		t.Fatalf("countdown without a wait: %q", got)
	}

	// A wait from before a new Start is gone with the old bar.
	stale := b.BeginWait()
	b.Start(4, 30*time.Second)
	defer b.Clear()
	current := b.BeginWait()
	stale()
	b.mu.Lock()
	open := len(b.waits)
	b.mu.Unlock()
	if open != 1 {
		t.Fatalf("%d waits open after a stale done call, want the current one", open)
	}
	current()
}

func TestTickRedrawsStalledCountdown(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 60, 10)
	b.timeout = 30 * time.Second
	_, advance := fakeClock(b)
	b.Advance("Loading")
	done := b.BeginWait()
	defer done()

	for range 40 { // drive the fill to its ceiling for this step
		b.tick()
	}
	advance(defaultStallDelay)
	b.tick()
	buf.Reset()

	advance(time.Second)
	b.tick()
	if buf.Len() == 0 {
		t.Fatal("stalled bar at the fill ceiling did not redraw its countdown")
	}
}

func TestFinishedBarStaysFullUntilClear(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 10, 10)
	b.current, b.visual, b.label = 7, 7, "Done"

	b.Finish(true)
	if !b.active || !b.complete || !b.shown {
		t.Fatal("finished bar was not kept on screen")
	}
	if strings.Contains(buf.String(), "\x1b[K") {
		t.Fatalf("Finish erased the completed frame: %q", buf.String())
	}
	if strings.Contains(b.last, trackStyle()) {
		t.Fatalf("completed frame still shows the track: %q", b.last)
	}
	if !strings.Contains(b.last, filledStyle(9, 10)) {
		t.Fatalf("completed frame does not reach the last gradient color: %q", b.last)
	}
	// A finished bar no longer moves.
	n := buf.Len()
	b.Advance("late")
	b.tick()
	if buf.Len() != n {
		t.Fatalf("finished bar drew again: %q", buf.String()[n:])
	}

	buf.Reset()
	b.Clear()
	if b.active {
		t.Fatal("Clear left the finished bar active")
	}
	if !strings.Contains(buf.String(), "\r\x1b[K") {
		t.Fatalf("Clear did not erase the finished frame: %q", buf.String())
	}
}

func TestFinishOnFailureDoesNotComplete(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 10, 10)
	b.current, b.visual = 4, 4
	b.cursor.Hide()
	b.SetLabel("Failed")

	buf.Reset()
	b.Finish(false)
	if b.complete {
		t.Fatal("failed bar was marked complete")
	}
	if !strings.Contains(b.last, trackStyle()) {
		t.Fatalf("failed bar has no track: %q", b.last)
	}
	if !strings.Contains(buf.String(), live.ShowCursorSequence) {
		t.Fatalf("failed bar did not show the cursor: %q", buf.String())
	}
}

func TestStartHidesAndFinishShowsCursor(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, Options{Width: 20, Force: true})
	b.Start(4, 30*time.Second)
	if !strings.Contains(buf.String(), live.HideCursorSequence) {
		t.Fatalf("Start did not hide the cursor: %q", buf.String())
	}
	buf.Reset()
	b.Finish(true)
	if !strings.Contains(buf.String(), live.ShowCursorSequence) {
		t.Fatalf("Finish did not show the cursor: %q", buf.String())
	}
	buf.Reset()
	b.Clear()
	if strings.Contains(buf.String(), live.ShowCursorSequence) {
		t.Fatalf("Clear showed the cursor a second time: %q", buf.String())
	}
}

func TestWithClearedWritesThenRedraws(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 12, 4)
	b.current, b.visual = 2, 2
	b.SetLabel("Working")

	buf.Reset()
	b.WithCleared(func() { _, _ = fmt.Fprintln(&buf, "log line") })
	out := buf.String()
	clearAt := strings.Index(out, "\r\x1b[K")
	logAt := strings.Index(out, "log line\n")
	redrawAt := strings.LastIndex(out, "\r\x1b[")
	if clearAt < 0 || logAt < clearAt || redrawAt < logAt {
		t.Fatalf("erase, log and redraw are out of order: %q", out)
	}
	if !b.shown || b.last == "" {
		t.Fatal("bar was not drawn again after the log line")
	}
}

func TestWriterClearsAroundWrites(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 12, 4)
	b.SetLabel("Working")
	buf.Reset()

	n, err := fmt.Fprintln(b.Writer(&buf), "log line")
	if err != nil || n != len("log line\n") {
		t.Fatalf("Write() = %d, %v", n, err)
	}
	if out := buf.String(); !strings.HasPrefix(out, "\r\x1b[Klog line\n\r") {
		t.Fatalf("writer output = %q", out)
	}
}

func TestConcurrentLifecycle(t *testing.T) {
	var buf syncBuffer
	b := New(&buf, Options{Width: 40, Force: true})
	b.Start(100, 30*time.Second)
	t.Cleanup(b.Clear)

	var workers sync.WaitGroup
	for worker := range 5 {
		workers.Go(func() {
			for step := range 20 {
				switch worker {
				case 0:
					b.Advance(fmt.Sprintf("step %d", step))
				case 1:
					b.SetLabel(fmt.Sprintf("label %d", step))
				case 2:
					b.WithCleared(func() {})
				case 3:
					b.tick()
				case 4:
					b.BeginWait()()
				}
			}
		})
	}
	workers.Wait()
	b.Finish(true)
	if !b.complete {
		t.Fatal("bar did not finish")
	}
	b.Clear()
}

func TestConcurrentStartsKeepOneBar(t *testing.T) {
	var buf syncBuffer
	b := New(&buf, Options{Width: 40, Force: true})
	var starts sync.WaitGroup
	for range 8 {
		starts.Go(func() {
			b.Start(10, 30*time.Second)
		})
	}
	starts.Wait()

	b.mu.Lock()
	active := b.active
	b.mu.Unlock()
	if !active {
		t.Fatal("concurrent starts left no bar")
	}
	b.Finish(true)
	b.Clear()
}

func TestDisabledBarWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, Options{}) // a buffer is no terminal
	if b.Enabled() {
		t.Fatal("bar enabled on a buffer")
	}
	b.Start(3, time.Second)
	b.Advance("a")
	b.SetLabel("b")
	b.BeginWait()()
	called := false
	b.WithCleared(func() { called = true })
	b.Finish(true)
	b.Clear()
	if !called {
		t.Fatal("WithCleared did not call write")
	}
	if buf.Len() != 0 {
		t.Fatalf("disabled bar wrote %q", buf.String())
	}
}

// A label from untrusted input stays on the bar's line and shows as plain
// text: no escape sequence, 7-bit or 8-bit, no control character, no line
// break and no reordering survive.
func TestLabelIsPlainLine(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 40, 2)
	b.SetLabel("\x1b[31mred\x1b[0m\r\nsafe\tlabel\x1b]0;owned\a\u0085\x1bXsos\x1b\\\x1b(B\u009d52;c;Zm9v\u009c\u202eexe.txt\xff")
	b.mu.Lock()
	label := b.label
	b.mu.Unlock()
	if want := "red safe label exe.txt\ufffd"; label != want {
		t.Fatalf("label = %q, want %q", label, want)
	}
}

func TestClearErasesLine(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 8, 2)
	b.SetLabel("x")
	buf.Reset()
	b.Clear()
	if got := buf.String(); got != "\r\x1b[K" {
		t.Fatalf("Clear = %q, want %q", got, "\r\x1b[K")
	}
	buf.Reset()
	b.Clear() // already cleared: writes nothing
	if buf.Len() != 0 {
		t.Fatalf("second Clear wrote %q", buf.String())
	}
}

// syncBuffer is a bytes.Buffer safe for the ticker goroutine and the test
// to use at once.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

// The bar is as wide as the terminal is when it is drawn, not as wide as it
// was at Start. A frame from before the window was made narrower has been
// wrapped onto a second row by the terminal, and is erased with it.
func TestBarFollowsTheTerminalWidth(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, Options{Force: true})
	width := 80
	b.terminalWidth = func() int { return width }
	b.active, b.total = true, 10
	b.Advance("Downloading")
	if got := term.VisibleWidth(visible(b.last)); got != 80 {
		t.Fatalf("bar width = %d on a terminal of 80, want 80", got)
	}

	buf.Reset()
	width = 40
	b.Advance("Unpacking")
	if got, want := buf.String(), "\r\x1b[1A\x1b[J\x1b[48"; !strings.HasPrefix(got, want) {
		t.Fatalf("draw after narrowing = %q, want it to start with %q", got, want)
	}
	if got := term.VisibleWidth(visible(b.last)); got != 40 {
		t.Fatalf("bar width = %d on a terminal narrowed to 40, want 40", got)
	}
	if !strings.Contains(b.last, filledStyle(39, 40)) && !strings.Contains(b.last, trackStyle()) {
		t.Fatalf("frame was not painted for the new width: %q", b.last)
	}

	// Wider again: the frame on screen takes one row and is drawn over.
	buf.Reset()
	width = 100
	b.Advance("Installing")
	if got := buf.String(); !strings.HasPrefix(got, "\r\x1b[48") {
		t.Fatalf("draw after widening = %q, want a plain redraw", got)
	}
	if got := term.VisibleWidth(visible(b.last)); got != 100 {
		t.Fatalf("bar width = %d on a terminal widened to 100, want 100", got)
	}

	// Cleared after narrowing, without a draw in between.
	buf.Reset()
	width = 30
	b.Clear()
	if got, want := buf.String(), "\r\x1b[3A\x1b[J"; got != want {
		t.Fatalf("clear after narrowing = %q, want %q", got, want)
	}
}

// Options.Width is an upper bound. The bar is never wider than the terminal.
func TestWidthIsAnUpperBound(t *testing.T) {
	for _, c := range []struct{ limit, terminal, want int }{
		{30, 80, 30},
		{173, 80, 80},
		{0, 80, 80},
		{30, 0, 30}, // the terminal does not tell
		{0, 0, fallbackWidth},
		{-5, 20, 20},
	} {
		var buf bytes.Buffer
		b := New(&buf, Options{Width: c.limit, Force: true})
		b.terminalWidth = func() int { return c.terminal }
		b.Start(4, 0)
		got := term.VisibleWidth(visible(lastFrame(b)))
		b.Clear()
		if got != c.want {
			t.Errorf("Width %d on a terminal of %d: bar width = %d, want %d", c.limit, c.terminal, got, c.want)
		}
	}
}

// The frame is exactly as wide as the bar, whatever the label holds and
// however little room there is, in every locale.
func TestFrameIsExactlyAsWideAsTheBar(t *testing.T) {
	labels := []string{"", "x", "世界世界世界", "a世b界", "éé", "👨‍👩‍👧 family", "Downloading package index", "→ step • two…"}
	for columns := 1; columns <= 14; columns++ {
		for _, label := range labels {
			for _, waiting := range []bool{false, true} {
				for _, total := range []int{0, 1, -5, math.MaxInt} {
					var buf bytes.Buffer
					b := New(&buf, Options{Width: columns, Force: true})
					b.active, b.total, b.timeout = true, max(total, 1), 30*time.Second
					_, advance := fakeClock(b)
					if waiting {
						b.BeginWait()
						advance(5 * time.Second)
					}
					b.Advance(label)
					b.Advance(label)
					b.tick()
					if got := term.VisibleWidth(visible(b.last)); got != columns {
						t.Errorf("columns %d, label %q, waiting %v, total %d: frame is %d cells wide: %q",
							columns, label, waiting, total, got, visible(b.last))
					}
				}
			}
		}
	}
}

// An unfinished bar keeps at least one cell of track, also where rounding
// would fill a narrow bar to its end; only Finish(true) draws it full.
func TestUnfinishedBarIsNeverFull(t *testing.T) {
	for _, columns := range []int{1, 2, 3, 8, 10, 16, 17, 80} {
		var buf bytes.Buffer
		b := testBar(&buf, columns, 3)
		for range 3 { // the last step is being worked on
			b.Advance("step")
		}
		for range 50 {
			b.tick()
		}
		if !strings.Contains(b.last, trackStyle()) {
			t.Errorf("unfinished bar of %d cells is drawn full: %q", columns, b.last)
		}
		b.Finish(true)
		if strings.Contains(b.last, trackStyle()) {
			t.Errorf("finished bar of %d cells still shows the track: %q", columns, b.last)
		}
		b.Clear()
	}
}

// 24-bit colors are painted only where COLORTERM announces them; any other
// terminal gets the nearest of the 256 colors.
func TestBarPaintsThe256ColorsWithoutTruecolor(t *testing.T) {
	for value, want := range map[string]bool{
		"truecolor": true, "24bit": true, "TrueColor": true,
		"": false, "1": false, "yes": false, "256color": false,
	} {
		if got := truecolor(value); got != want {
			t.Errorf("truecolor(%q) = %v, want %v", value, got, want)
		}
	}

	t.Setenv("COLORTERM", "")
	var buf bytes.Buffer
	b := New(&buf, Options{Width: 20, Force: true})
	b.active, b.total, b.current = true, 4, 2
	b.SetLabel("Working")
	out := buf.String()
	if strings.Contains(out, "48;2;") || strings.Contains(out, "38;2;") {
		t.Fatalf("24-bit color on a terminal that did not announce it: %q", out)
	}
	track := fmt.Sprintf("\x1b[48;5;%d;38;5;%dm",
		term.Nearest256(DefaultPalette.Track.R, DefaultPalette.Track.G, DefaultPalette.Track.B),
		term.Nearest256(DefaultPalette.TrackText.R, DefaultPalette.TrackText.G, DefaultPalette.TrackText.B))
	if !strings.Contains(out, track) {
		t.Fatalf("frame %q does not paint the track as %q", out, track)
	}
	if got := term.VisibleWidth(visible(out)); got != 20 {
		t.Fatalf("frame is %d cells wide, want 20", got)
	}
}

// The label goes one way for the whole gradient: lighter on dark colors,
// darker on light ones, on which lighter text could not be read.
func TestLabelDirectionFollowsTheGradient(t *testing.T) {
	if darkerText(DefaultPalette.Gradient, defaultContrast) {
		t.Fatal("label on the default gradient is darker than it")
	}
	light := []Color{{R: 255, G: 255, B: 255}, {R: 255, G: 221, B: 0}, {R: 120, G: 220, B: 120}, {R: 0, G: 200, B: 255}}
	if !darkerText(light, defaultContrast) {
		t.Fatal("label on a light gradient is lighter than it")
	}
	for column := range 40 {
		background := gradientAt(light, column, 40)
		text := textColor(background, defaultContrast, true)
		if ratio := contrastRatio(background, text); ratio < defaultContrast-0.01 {
			t.Fatalf("column %d: text %v on %v has contrast %.2f, want at least %.2f", column, text, background, ratio, defaultContrast)
		}
	}

	// From dark to light, no way reaches the contrast everywhere. The label
	// goes the way that comes closest, and the same way in every cell.
	mixed := []Color{{R: 20, G: 20, B: 60}, {R: 250, G: 250, B: 200}}
	darker := darkerText(mixed, defaultContrast)
	for column := range 40 {
		background := gradientAt(mixed, column, 40)
		text := textColor(background, defaultContrast, darker)
		if lighter := relativeLuminance(text) > relativeLuminance(background); lighter == darker && text != background {
			t.Fatalf("column %d: text %v on %v goes the other way", column, text, background)
		}
	}

	var buf bytes.Buffer
	b := New(&buf, Options{Width: 10, Force: true, Palette: &Palette{Gradient: light}})
	b.active, b.total = true, 2
	b.Finish(true)
	want := sgr(light[0], textColor(light[0], defaultContrast, true), true)
	if !strings.Contains(buf.String(), want) {
		t.Fatalf("bar on a light gradient = %q, want its first cell painted %q", buf.String(), want)
	}
}

// A failed bar is frozen: a log line written later draws the same frame
// again, not the countdown for the time that has passed since.
func TestFinishedBarKeepsItsCountdown(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 40, 4)
	b.timeout = 30 * time.Second
	_, advance := fakeClock(b)
	b.Advance("Request")
	b.BeginWait()
	advance(10 * time.Second)
	b.tick()
	b.Finish(false)
	frozen := b.last
	if got, want := visible(frozen), "Request (20s)"; !strings.Contains(got, want) {
		t.Fatalf("failed bar = %q, want %q", got, want)
	}

	advance(7 * time.Second)
	buf.Reset()
	_, _ = fmt.Fprintln(b.Writer(&buf), "request failed")
	if b.last != frozen {
		t.Fatalf("failed bar changed after a log line: %q, want %q", visible(b.last), visible(frozen))
	}
	if got, want := buf.String(), "\r\x1b[Krequest failed\n\r"+frozen; got != want {
		t.Fatalf("log line after Finish wrote %q, want %q", got, want)
	}
}

// What follows the last newline of a write is held back until its newline
// arrives: drawn right away, the bar would cover it.
func TestWriterHoldsBackPartialLines(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 12, 4)
	b.SetLabel("Working")
	frame := b.last
	w := b.Writer(&buf)

	buf.Reset()
	n, err := fmt.Fprint(w, "Uploading... ")
	if n != len("Uploading... ") || err != nil {
		t.Fatalf("partial write = %d, %v, want its own length", n, err)
	}
	if buf.Len() != 0 {
		t.Fatalf("partial line was written under the bar: %q", buf.String())
	}
	n, err = fmt.Fprint(w, "done\nnext: ")
	if n != len("done\nnext: ") || err != nil {
		t.Fatalf("write = %d, %v, want its own length", n, err)
	}
	if got, want := buf.String(), "\r\x1b[KUploading... done\n\r"+frame; got != want {
		t.Fatalf("wrote %q, want %q", got, want)
	}

	// Finish ends the line that is held back above the finished bar.
	buf.Reset()
	b.Finish(false)
	if got, want := buf.String(), "\r\x1b[Knext: \n\r"+frame; got != want {
		t.Fatalf("Finish wrote %q, want %q", got, want)
	}

	// Clear writes it as it is, where the bar was.
	_, _ = fmt.Fprint(w, "result: ")
	buf.Reset()
	b.Clear()
	if got, want := buf.String(), "\r\x1b[Kresult: "; got != want {
		t.Fatalf("Clear wrote %q, want %q", got, want)
	}

	// Without a bar on screen nothing is held back.
	buf.Reset()
	_, _ = fmt.Fprint(w, "ok")
	if got, want := buf.String(), "ok"; got != want {
		t.Fatalf("write without a bar = %q, want %q", got, want)
	}

	// Every writer for one stream adds to the same line, in the order the
	// lines were begun on their streams.
	var other bytes.Buffer
	b = testBar(&buf, 12, 4)
	b.SetLabel("Working")
	_, _ = fmt.Fprint(b.Writer(&other), "second stream")
	_, _ = fmt.Fprint(b.Writer(&buf), "Uploading... ")
	buf.Reset()
	_, _ = fmt.Fprintln(b.Writer(&buf), "done")
	if got, want := buf.String(), "\r\x1b[KUploading... done\n\r"+frame; got != want {
		t.Fatalf("two writers for one stream wrote %q, want %q", got, want)
	}
	b.Clear()
	if got, want := other.String(), "second stream"; got != want {
		t.Fatalf("Clear wrote %q to the other stream, want %q", got, want)
	}
	b.mu.Lock()
	held := len(b.held)
	b.mu.Unlock()
	if held != 0 {
		t.Fatalf("%d writers still recorded as holding a line back", held)
	}
}

// A line without an end is not held back forever, and a failed write
// reports how much of the caller's bytes went out.
func TestWriterLimitsWhatItHoldsBack(t *testing.T) {
	var buf bytes.Buffer
	b := testBar(&buf, 12, 4)
	b.SetLabel("Working")
	w := b.Writer(&buf)
	buf.Reset()
	long := strings.Repeat("x", maxHeld+1)
	if n, err := fmt.Fprint(w, long); n != len(long) || err != nil {
		t.Fatalf("write = %d, %v", n, err)
	}
	if !strings.Contains(buf.String(), long) {
		t.Fatal("a partial line beyond the limit was held back")
	}

	failing := b.Writer(failingWriter{})
	_, _ = fmt.Fprint(failing, "held ")
	n, err := fmt.Fprint(failing, "back\n")
	if n != 0 || err == nil {
		t.Fatalf("failed write = %d, %v, want 0 and the error", n, err)
	}
	b.Clear()
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// The callback of WithCleared may log through the bar's own writer and call
// the bar's methods. Both took the lock WithCleared was holding.
func TestWithClearedCallbackMayUseTheBar(t *testing.T) {
	var buf syncBuffer
	b := testBar(&buf, 20, 4)
	b.SetLabel("Working")
	logger := log.New(b.Writer(&buf), "", 0)

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		b.WithCleared(func() {
			logger.Println("inside")
			b.Advance("Next")
			b.WithCleared(func() { logger.Println("nested") })
		})
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("WithCleared deadlocked on a callback that uses the bar")
	}

	buf.mu.Lock()
	out := buf.buf.String()
	buf.mu.Unlock()
	// Erased once, both lines written with no frame between them, then the
	// bar drawn again with what the callback changed.
	if !strings.Contains(out, "\r\x1b[Kinside\nnested\n\r\x1b[") {
		t.Fatalf("output = %q", out)
	}
	if got := visible(lastFrame(b)); !strings.Contains(got, "Next") {
		t.Fatalf("bar after the callback = %q, want its new label", got)
	}
}

// The countdown is shown as plain text on the bar's one line, whatever the
// formatter returns.
func TestCountdownIsPlainLine(t *testing.T) {
	var buf bytes.Buffer
	b := New(&buf, Options{
		Width:     40,
		Force:     true,
		Countdown: func(time.Duration) string { return "\x1b[31m9s\tleft\n" },
	})
	b.active, b.total, b.timeout = true, 4, 10*time.Second
	_, advance := fakeClock(b)
	b.Advance("Uploading")
	b.BeginWait()
	advance(defaultStallDelay)
	b.tick()
	if got, want := visible(b.last), " Uploading 9s left "; !strings.HasPrefix(got, want) {
		t.Fatalf("bar = %q, want it to start with %q", got, want)
	}
	if got := term.VisibleWidth(visible(b.last)); got != 40 {
		t.Fatalf("bar width = %d, want 40", got)
	}
}

// A bar that finishes does not show a cursor the application around it
// still keeps hidden: the cursors of one terminal count together.
func TestBarLeavesTheApplicationsCursorHidden(t *testing.T) {
	var buf bytes.Buffer
	restore := live.HideCursor(&buf)
	b := New(&buf, Options{Width: 20, Force: true})
	b.Start(2, 0)
	b.Finish(true)
	b.Clear()
	if got := strings.Count(buf.String(), live.HideCursorSequence); got != 1 {
		t.Fatalf("cursor hidden %d times, want once: %q", got, buf.String())
	}
	if strings.Contains(buf.String(), live.ShowCursorSequence) {
		t.Fatalf("bar showed the cursor the application hides: %q", buf.String())
	}
	restore()
	if !strings.HasSuffix(buf.String(), live.ShowCursorSequence) {
		t.Fatalf("application did not show the cursor: %q", buf.String())
	}
}

// Everything at once, with the ticker running: starts, finishes and clears
// while steps, waits, log lines and partial lines come in from other
// goroutines. The race detector and the deadline are what this test is for.
func TestConcurrentWritersAndLifecycle(t *testing.T) {
	var buf syncBuffer
	b := New(&buf, Options{Width: 40, Force: true})
	logger := log.New(b.Writer(&buf), "", 0)
	var workers sync.WaitGroup
	for worker := range 9 {
		workers.Go(func() {
			for i := range 200 {
				switch worker {
				case 0:
					b.Start(10, time.Second)
				case 1:
					b.Advance("step")
				case 2:
					logger.Printf("log %d", i)
				case 3:
					done := b.BeginWait()
					done()
					done()
				case 4:
					b.Finish(i%2 == 0)
				case 5:
					b.Clear()
				case 6:
					b.WithCleared(func() { b.SetLabel("label") })
				case 7:
					_, _ = fmt.Fprint(b.Writer(&buf), "partial ")
				case 8:
					b.tick()
				}
			}
		})
	}
	finished := make(chan struct{})
	go func() {
		workers.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(30 * time.Second):
		t.Fatal("bar deadlocked")
	}
	b.Clear()
	if b.cursor.Hidden() {
		t.Fatal("cleared bar keeps the cursor hidden")
	}
	b.mu.Lock()
	held, cleared := len(b.held), b.cleared
	b.mu.Unlock()
	if held != 0 || cleared != 0 {
		t.Fatalf("cleared bar holds %d partial lines back and counts %d writes in progress", held, cleared)
	}
}
