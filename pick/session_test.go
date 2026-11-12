package pick

import (
	"os"
	"strings"
	"testing"
)

// The block is redrawn in place, so every draw but the first starts by
// moving back over the lines the previous one left.
func TestRendererRedrawsInPlace(t *testing.T) {
	var out strings.Builder
	r := &renderer{w: &out}

	if err := r.draw([]string{"one", "two", "three"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "A") && strings.Contains(out.String(), "\x1b[2A") {
		t.Error("the first draw moved the cursor up")
	}
	out.Reset()

	if err := r.draw([]string{"one", "two"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "\r\x1b[2A") {
		t.Errorf("redraw = %q, want it to start two lines up", out.String())
	}
	if !strings.HasSuffix(out.String(), "\x1b[J") {
		t.Errorf("redraw = %q, want it to erase what the taller block left", out.String())
	}
	out.Reset()

	if err := r.clear(); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "\r\x1b[1A\x1b[J"; got != want {
		t.Errorf("clear() = %q, want %q", got, want)
	}
	out.Reset()
	if err := r.clear(); err != nil || out.Len() != 0 {
		t.Errorf("clear() of nothing wrote %q, %v", out.String(), err)
	}
}

// A terminal made narrower while the block is up wraps what was drawn onto
// more rows, so the way back is counted at the width it has by then.
func TestRendererCountsRowsAtTheWidthItHasNow(t *testing.T) {
	var out strings.Builder
	n := 80
	r := &renderer{w: &out, width: func() int { return n }}

	if err := r.draw([]string{"one", "\x1b[1mabcdefghij\x1b[0m", "two"}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	n = 4
	if err := r.clear(); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "\r\x1b[4A\x1b[J"; got != want {
		t.Errorf("clear() = %q, want %q", got, want)
	}

	n = 80
	if err := r.draw([]string{"Apply now? abcdefghij"}); err != nil {
		t.Fatal(err)
	}
	if err := r.placeCursor(3); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	n = 10
	if err := r.clear(); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "\r\x1b[J"; got != want {
		t.Errorf("clear() with the cursor on the first row = %q, want %q", got, want)
	}
}

// The block starts in the first column of the line the cursor is on, the
// first time it is drawn like every time after: a block that started where
// the caller's unfinished line ended would be counted a row short once its
// first line reached the right edge.
func TestRendererTakesTheLineItStartsOn(t *testing.T) {
	var out strings.Builder
	r := &renderer{w: &out}
	if err := r.draw([]string{"Which release?", "❯ 1.4.24"}); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "\r\x1b[2KWhich release?\r\n\x1b[2K❯ 1.4.24\x1b[J"; got != want {
		t.Errorf("first draw = %q, want %q", got, want)
	}
}

// Once the terminal was put back, by the end of the prompt or by a signal,
// nothing is drawn any more.
func TestRendererDrawsNothingOnceEnded(t *testing.T) {
	var out strings.Builder
	r := &renderer{w: &out}
	_ = r.draw([]string{"one", "two"})
	out.Reset()
	if err := r.end(); err != nil || out.String() != "\r\x1b[1A\x1b[J" {
		t.Fatalf("end() wrote %q, %v; want the block erased", out.String(), err)
	}
	out.Reset()
	_ = r.draw([]string{"one", "two"})
	_ = r.placeCursor(3)
	_ = r.clear()
	if out.Len() != 0 {
		t.Errorf("after end() the renderer wrote %q", out.String())
	}
}

// Keys a prompt read past its answer wait for the next prompt on the same
// input, and for no other.
func TestFileInputKeepsTypeAheadForTheSameInput(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	other, otherW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	defer otherW.Close()

	first := newFileInput(r)
	first.unread([]byte("def\r"))
	first.close()

	stranger := newFileInput(other)
	defer stranger.close()
	if len(stranger.ahead) != 0 {
		t.Errorf("another input was handed %q", stranger.ahead)
	}
	second := newFileInput(r)
	defer second.close()
	keys, err := second.read()
	if err != nil || string(keys) != "def\r" {
		t.Fatalf("read() = %q, %v; want the keys the prompt before left", keys, err)
	}
	// They are handed out once.
	third := newFileInput(r)
	defer third.close()
	if len(third.ahead) != 0 {
		t.Errorf("the keys were handed out again: %q", third.ahead)
	}
}
