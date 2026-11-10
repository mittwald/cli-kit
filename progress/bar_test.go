package progress

import (
	"bytes"
	"fmt"
	"io"
	"math"
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

func trackStyle() string { return sgr(DefaultPalette.Track, DefaultPalette.TrackText) }

func filledStyle(column, width int) string {
	background := gradientAt(DefaultPalette.Gradient, column, width)
	return sgr(background, textColor(background, defaultContrast))
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
		text := textColor(want, defaultContrast)
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
	b.columns, b.last, b.filledStyles = 6, "", nil
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

func TestWaitDoneIgnoresOlderWait(t *testing.T) {
	b := testBar(io.Discard, 40, 4)
	b.timeout = 30 * time.Second
	start, _ := fakeClock(b)
	stale := b.BeginWait()
	current := b.BeginWait()
	stale() // a late done call from the previous wait must not end the current one

	b.mu.Lock()
	waitStart := b.waitStart
	b.mu.Unlock()
	if !waitStart.Equal(start) {
		t.Fatalf("current wait start = %v, want %v", waitStart, start)
	}
	current()
	b.mu.Lock()
	waitStart = b.waitStart
	b.mu.Unlock()
	if !waitStart.IsZero() {
		t.Fatalf("finished wait left start %v", waitStart)
	}
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

func TestSanitizeLabel(t *testing.T) {
	label := "\x1b[31mred\x1b[0m\r\nsafe\tlabel\x1b]0;owned\a\u0085"
	if got, want := sanitizeLabel(label), "redsafe label"; got != want {
		t.Fatalf("sanitizeLabel() = %q, want %q", got, want)
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
