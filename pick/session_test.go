package pick

import (
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
