package secret

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mittwald/cli-kit/live"
)

type promptResult struct {
	value string
	err   error
}

// startInProcessPrompt runs TTYPrompt in this process on a pseudo terminal
// and returns once it waits for the answer with echo turned off. Unlike
// startPrompt, it leaves the signals alone: none is sent.
func startInProcessPrompt(t *testing.T, req Request) (master, tty *os.File, shown string, result <-chan promptResult) {
	t.Helper()
	master, tty = openPTY(t)
	if !echoes(t, tty) {
		t.Skip("the pseudo terminal starts without echo")
	}
	t.Cleanup(func() { openTTY = defaultOpenTTY })
	// TTYPrompt closes what it opens, so it gets a file of its own.
	openTTY = func() (*os.File, error) { return os.OpenFile(tty.Name(), os.O_RDWR|syscall.O_NOCTTY, 0) }
	done := make(chan promptResult, 1)
	go func() {
		value, err := TTYPrompt(req)
		done <- promptResult{value, err}
	}()
	for deadline := time.Now().Add(20 * time.Second); !strings.Contains(shown, req.Prompt()) || echoes(t, tty); {
		if time.Now().After(deadline) {
			t.Fatalf("the prompt does not wait with echo off; terminal shows %q", shown)
		}
		shown += readAvailable(t, master)
	}
	return master, tty, shown, done
}

func await(t *testing.T, result <-chan promptResult) promptResult {
	t.Helper()
	select {
	case r := <-result:
		return r
	case <-time.After(20 * time.Second):
		t.Fatal("the prompt did not return")
		return promptResult{}
	}
}

// The hint comes before the question, the answer is not echoed, and the
// terminal gets its echo back.
func TestTTYPromptInProcess(t *testing.T) {
	master, tty, shown, result := startInProcessPrompt(t, Request{Label: "Password", Hint: "Create a token at https://example.com/tokens."})
	if !strings.Contains(shown, "Create a token at") || strings.Index(shown, "Create a token") > strings.Index(shown, "Password: ") {
		t.Errorf("terminal shows %q, want the hint before the question", shown)
	}
	if _, err := master.WriteString("hunter2\n"); err != nil {
		t.Fatal(err)
	}
	if r := await(t, result); r.err != nil || r.value != "hunter2" {
		t.Fatalf("TTYPrompt = %q, %v", r.value, r.err)
	}
	if after := readAvailable(t, master); strings.Contains(after, "hunter2") {
		t.Errorf("the answer was echoed: %q", after)
	}
	if !echoes(t, tty) {
		t.Error("the terminal is left with echo off")
	}
}

// What package live restores on the way out includes the prompt's
// terminal: its echo comes back and its line is ended. Once the prompt
// returned, a later restore leaves the terminal alone.
func TestTTYPromptRestoredByLive(t *testing.T) {
	master, tty, _, result := startInProcessPrompt(t, Request{Label: "Password", Repeat: true, Hint: "not shown on a repetition"})
	live.RestoreAll()
	if !echoes(t, tty) {
		t.Fatal("live.RestoreAll left the echo off")
	}
	if shown := readAvailable(t, master); !strings.Contains(shown, "\n") {
		t.Errorf("terminal shows %q, want the prompt's line ended", shown)
	}
	if _, err := master.WriteString("hunter2\n"); err != nil {
		t.Fatal(err)
	}
	if r := await(t, result); r.err != nil || r.value != "hunter2" {
		t.Fatalf("TTYPrompt = %q, %v", r.value, r.err)
	}
	live.RestoreAll() // nothing registered any more
}
