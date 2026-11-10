//go:build unix

package pick

import (
	"os"
	"testing"
	"time"
)

// An escape sequence split across two writes reaches grace through the real
// input, so an arrow key is not taken for Esc, and grace waits before giving
// up on a lone ESC.
func TestFileInputGraceJoinsSplitEscape(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	in := &fileInput{file: r}
	go func() {
		_, _ = w.Write([]byte{0x1b})
		time.Sleep(10 * time.Millisecond)
		_, _ = w.Write([]byte("[B"))
	}()
	first, err := in.read()
	if err != nil || string(first) != "\x1b" {
		t.Fatalf("read = %q, %v", first, err)
	}
	if got := string(in.grace()); got != "[B" {
		t.Errorf("grace = %q, want the rest of the sequence", got)
	}
	start := time.Now()
	if got := in.grace(); len(got) != 0 {
		t.Errorf("grace with nothing pending = %q", got)
	}
	if time.Since(start) < escapeGrace/2 {
		t.Error("grace returned before waiting")
	}
}
