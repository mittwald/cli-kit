package term

import (
	"bytes"
	"io"
	"os"
	"testing"
)

type wrapped struct{ w io.Writer }

func (w wrapped) Write(p []byte) (int, error) { return w.w.Write(p) }
func (w wrapped) Unwrap() io.Writer           { return w.w }

func TestIsTerminalNotATerminal(t *testing.T) {
	var buf bytes.Buffer
	if IsTerminal(&buf) || IsTerminal(wrapped{&buf}) || IsTerminal(nil) {
		t.Error("a buffer reports as a terminal")
	}
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if IsTerminal(wrapped{wrapped{f}}) {
		t.Error("a regular file reports as a terminal")
	}
	if w, h := Size(f); w != 0 || h != 0 {
		t.Errorf("Size(file) = %d, %d", w, h)
	}
	if got := Columns(f, 80); got != 80 {
		t.Errorf("Columns fallback = %d", got)
	}
	if Interactive(f, f) || Interactive(nil, f) {
		t.Error("files are interactive")
	}
}
