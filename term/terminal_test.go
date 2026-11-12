package term

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
	"time"
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

// fdOnly wraps a file descriptor without being a file.
type fdOnly struct{ f *os.File }

func (f fdOnly) Fd() uintptr { return f.f.Fd() }

// Something that has a file descriptor and nothing else is asked for it.
func TestIsTerminalWithAFileDescriptorAlone(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if IsTerminal(fdOnly{f}) {
		t.Error("a regular file's descriptor reports as a terminal")
	}
	if w, h := Size(fdOnly{f}); w != 0 || h != 0 {
		t.Errorf("Size = %d, %d", w, h)
	}
}

// A file that is closed, or nil, is no terminal and has no size.
func TestIsTerminalClosedFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	var none *os.File
	for _, v := range []any{f, none, wrapped{f}} {
		if IsTerminal(v) {
			t.Errorf("IsTerminal(%v) of a closed or nil file", v)
		}
		if w, h := Size(v); w != 0 || h != 0 {
			t.Errorf("Size(%v) = %d, %d", v, w, h)
		}
	}
}

// Asking whether a file is a terminal leaves the file as it is. Taking its
// descriptor with Fd would put it into blocking mode, where a read deadline
// no longer ends a Read and neither does Close.
func TestAskingLeavesDeadlinesWorking(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	defer r.Close()
	if err := r.SetReadDeadline(time.Now().Add(time.Hour)); err != nil {
		t.Skipf("this platform has no deadlines on a pipe: %v", err)
	}
	was := CurrentColorMode()
	t.Cleanup(func() { SetColorMode(was) })
	for _, mode := range []ColorMode{ColorAuto, ColorAlways} {
		SetColorMode(mode)
		if IsTerminal(r) || IsTerminal(wrapped{r}) || CanAnimate(r) || Columns(r, 80) != 80 {
			t.Fatal("a pipe reports as a terminal")
		}
		_ = Colored(r)
		_, _ = Size(r)
	}

	if err := r.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := r.Read(make([]byte, 1))
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Errorf("Read = %v, want a deadline error", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("the read deadline no longer ends a Read")
		// End the Read, so that the test does not leave it behind.
		_, _ = w.Write([]byte{0})
		t.Logf("the Read ended with %v once there was something to read", <-done)
	}
}
