//go:build windows

package term

import (
	"os"
	"testing"
)

// Only a console can show escape sequences as text: a file takes them as
// they are, and its handle is no screen buffer to switch anything on for.
func TestPlainConsoleOfAFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if plainConsole(f) || plainConsole(wrapped{f}) || plainConsole(nil) {
		t.Error("a file reports as a console that shows escape sequences as text")
	}
	if !ShouldColor(ColorAlways, f) {
		t.Error("always does not color a file")
	}
	descriptor(f, func(fd uintptr) {
		// Asked twice: the second answer is the remembered one.
		for range 2 {
			if enableVT(fd) {
				t.Error("virtual terminal processing reports as on for a file")
			}
		}
	})
}
