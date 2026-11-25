//go:build unix

package ttyin

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestReadReturnsInputAndCanBeWoken(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	in := NewReader(r)
	defer in.Close()
	if !in.Wakes() {
		t.Fatal("a pipe cannot be woken")
	}
	if _, err := w.Write([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8)
	n, err := in.Read(buf)
	if err != nil || string(buf[:n]) != "ab" {
		t.Fatalf("read %q, %v", buf[:n], err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := in.Read(buf)
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	in.Wake()
	select {
	case err := <-done:
		if !errors.Is(err, ErrWoken) {
			t.Fatalf("woken read returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the read was not woken")
	}
}
