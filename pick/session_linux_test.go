//go:build linux

package pick

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mittwald/cli-kit/live"
)

// ptyChildEnv names the prompt a test's child process puts up on the
// pseudo-terminal it is started on. The tests in this file drive prompts
// through a real terminal device, which is the only way to see what a
// prompt leaves the terminal in. They open one through /dev/ptmx, so they
// run on Linux only.
const ptyChildEnv = "PICK_PTY_CHILD"

// TestPromptOnPTY is the child side of the tests below: started with
// ptyChildEnv set, it puts up the prompt named there on its standard input
// and output and reports how the prompt ended on standard error.
func TestPromptOnPTY(t *testing.T) {
	items := []Item{{Cells: []string{"one"}}, {Cells: []string{"two"}}}
	forever := func() ([]Item, error) {
		time.Sleep(time.Minute)
		return items, nil
	}
	switch os.Getenv(ptyChildEnv) {
	case "":
		t.Skip("the child side of the pseudo-terminal tests")
	case "list":
		index, err := Select(os.Stdin, os.Stdout, Options{Title: "Which?", Items: items})
		fmt.Fprintf(os.Stderr, "answer: %d, %v\n", index, err)
	case "live":
		// An application that has live put the cursor back on a signal:
		// live's handler ends the process as soon as it is done.
		defer live.InstallSignalRestore()()
		index, err := Select(os.Stdin, os.Stdout, Options{Title: "Which?", Items: items})
		fmt.Fprintf(os.Stderr, "answer: %d, %v\n", index, err)
	case "restore-all":
		// An application that puts the terminal back itself, on an occasion
		// of its own, and lives on.
		own := make(chan os.Signal, 1)
		signal.Notify(own, syscall.SIGUSR1)
		go func() {
			<-own
			live.RestoreAll()
		}()
		index, err := Select(os.Stdin, os.Stdout, Options{Title: "Which?", Items: items})
		fmt.Fprintf(os.Stderr, "answer: %d, %v\n", index, err)
	case "handled":
		// An application with a handler of its own for the signal.
		own := make(chan os.Signal, 4)
		signal.Notify(own, syscall.SIGTERM)
		index, err := Select(os.Stdin, os.Stdout, Options{Title: "Which?", Items: items})
		fmt.Fprintf(os.Stderr, "answer: %d, %v\n", index, err)
		select {
		case sig := <-own:
			fmt.Fprintf(os.Stderr, "handler: %v\n", sig)
		case <-time.After(5 * time.Second):
			fmt.Fprintln(os.Stderr, "handler: nothing")
		}
	case "loading":
		index, err := Select(os.Stdin, os.Stdout, Options{Title: "Which?", Views: []View{{Name: "remote", Load: forever}}})
		fmt.Fprintf(os.Stderr, "answer: %d, %v\n", index, err)
	case "panic":
		_, _ = Select(os.Stdin, os.Stdout, Options{Title: "Which?", Views: []View{{Name: "remote", Load: func() ([]Item, error) {
			time.Sleep(100 * time.Millisecond)
			panic("boom")
		}}}})
	case "two":
		first, err := Input(os.Stdin, os.Stdout, InputOptions{Prompt: "First: "})
		fmt.Fprintf(os.Stderr, "answer: %q, %v\n", first, err)
		second, err := Input(os.Stdin, os.Stdout, InputOptions{Prompt: "Second: "})
		fmt.Fprintf(os.Stderr, "answer: %q, %v\n", second, err)
	}
}

// openPTY opens a pseudo-terminal 80 by 24 cells large and returns both of
// its ends, or skips the test where there is none to be had.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Skipf("unlocking the pseudo-terminal: %v", err)
	}
	number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Skipf("naming the pseudo-terminal: %v", err)
	}
	slave, err = os.OpenFile("/dev/pts/"+strconv.Itoa(number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("opening the pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: 80}); err != nil {
		t.Skipf("sizing the pseudo-terminal: %v", err)
	}
	return master, slave
}

// ptyChild is a prompt running in a process of its own on a pseudo-terminal.
type ptyChild struct {
	t             *testing.T
	command       *exec.Cmd
	master, slave *os.File
	// before is the terminal's mode as the prompt found it.
	before unix.Termios
	stderr strings.Builder

	mu     sync.Mutex
	screen strings.Builder
	// deaf makes the test stop listening to the terminal, and drained is
	// closed once it has: the terminal has nothing more to say, or nobody
	// is to hear it.
	deaf    atomic.Bool
	drained chan struct{}
}

// startOnPTY starts the child side with the prompt mode names and waits for
// the prompt to be on screen and waiting for a key.
func startOnPTY(t *testing.T, mode, shown string) *ptyChild {
	t.Helper()
	master, slave := openPTY(t)
	before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	child := &ptyChild{t: t, master: master, slave: slave, before: *before, drained: make(chan struct{})}
	child.command = exec.Command(os.Args[0], "-test.run=^TestPromptOnPTY$")
	// A binary built with the race detector waits a second before it exits,
	// which the tests have no use for.
	child.command.Env = append(os.Environ(), ptyChildEnv+"="+mode, "GORACE=atexit_sleep_ms=0")
	child.command.Stdin, child.command.Stdout, child.command.Stderr = slave, slave, &child.stderr
	if err := child.command.Start(); err != nil {
		t.Fatalf("starting the child: %v", err)
	}
	t.Cleanup(func() { _ = child.command.Process.Kill() })
	go child.listen(master)
	child.waitFor(shown)
	// The prompt writes before it reads; give it the moment to get there.
	time.Sleep(50 * time.Millisecond)
	return child
}

// listen collects what the child writes to the terminal. It waits for
// output rather than blocking in a read, so that closing the terminal's
// other end is not held up by a read still in flight.
func (c *ptyChild) listen(master *os.File) {
	defer close(c.drained)
	buf := make([]byte, 4096)
	for !c.deaf.Load() {
		if ready, _ := awaitInput(master, -1, 10*time.Millisecond); !ready {
			continue
		}
		n, err := unix.Read(int(master.Fd()), buf)
		if n > 0 {
			c.mu.Lock()
			c.screen.Write(buf[:n])
			c.mu.Unlock()
		}
		if err != nil && !errors.Is(err, unix.EINTR) && !errors.Is(err, unix.EAGAIN) {
			return
		}
	}
}

// drawn is everything the child has written to the terminal so far.
func (c *ptyChild) drawn() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.screen.String()
}

func (c *ptyChild) waitFor(text string) {
	c.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(c.drawn(), text) {
		if time.Now().After(deadline) {
			c.t.Fatalf("the child never drew %q; it drew %q", text, c.drawn())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (c *ptyChild) keys(keys string) {
	c.t.Helper()
	if _, err := c.master.WriteString(keys); err != nil {
		c.t.Fatalf("typing %q: %v", keys, err)
	}
}

// wait waits for the child to end and returns how it did, with what it
// reported on standard error.
func (c *ptyChild) wait() (*os.ProcessState, string) {
	c.t.Helper()
	ended := make(chan struct{})
	go func() {
		_ = c.command.Wait()
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(10 * time.Second):
		_ = c.command.Process.Kill()
		<-ended
		c.t.Fatalf("the child did not end; it reported %q and drew %q", c.stderr.String(), c.drawn())
	}
	return c.command.ProcessState, c.stderr.String()
}

// expectRestored checks that the terminal is in the mode the prompt found
// it in, and that the prompt erased itself and showed the cursor again.
func (c *ptyChild) expectRestored() {
	c.t.Helper()
	after, err := unix.IoctlGetTermios(int(c.slave.Fd()), unix.TCGETS)
	if err != nil {
		c.t.Fatal(err)
	}
	if *after != c.before {
		c.t.Errorf("the terminal was left in another mode: local flags %#o, want %#o", after.Lflag, c.before.Lflag)
	}
	// With the last descriptor of the terminal closed, its other end reads
	// to the end of what was written.
	_ = c.slave.Close()
	select {
	case <-c.drained:
	case <-time.After(5 * time.Second):
		c.t.Fatal("the terminal never ran dry")
	}
	drawn := c.drawn()
	erased, shown := strings.LastIndex(drawn, "\x1b[J"), strings.LastIndex(drawn, showCursor)
	if erased < 0 || shown < erased {
		c.t.Errorf("the prompt did not erase itself and show the cursor at its end: %q", drawn)
	}
	if rest := drawn[shown+len(showCursor):]; strings.Contains(rest, "Which?") || strings.Contains(rest, hideCursor) {
		c.t.Errorf("the prompt drew again after the terminal was put back: %q", rest)
	}
}

func signaled(state *os.ProcessState, sig syscall.Signal) bool {
	status, ok := state.Sys().(syscall.WaitStatus)
	return ok && status.Signaled() && status.Signal() == sig
}

// However a prompt ends, the terminal is as it was found.
func TestPromptPutsTheTerminalBack(t *testing.T) {
	for _, c := range []struct {
		name, mode, keys, want string
	}{
		{"answered", "list", "\x1b[B\r", "answer: 1, <nil>"},
		{"Esc", "list", "\x1b", "answer: -1, prompt aborted: went back"},
		{"Ctrl-C", "list", "\x03", "answer: -1, prompt aborted: interrupted"},
		{"Ctrl-C while a view loads", "loading", "\x03", "answer: -1, prompt aborted: interrupted"},
		{"Esc while a view loads", "loading", "\x1b", "answer: -1, prompt aborted: went back"},
		{"a panic in Load", "panic", "", "panic: boom"},
	} {
		t.Run(c.name, func(t *testing.T) {
			child := startOnPTY(t, c.mode, "Esc cancel")
			child.keys(c.keys)
			state, reported := child.wait()
			if !strings.Contains(reported, c.want) {
				t.Errorf("the child reported %q, want %q", reported, c.want)
			}
			if panicked := c.mode == "panic"; state.Success() == panicked {
				t.Errorf("the child ended with %v and reported %q", state, reported)
			}
			child.expectRestored()
		})
	}
}

// A signal that ends the process finds the terminal put back, and still ends
// the process the way it would have without a prompt on screen.
func TestPromptPutsTheTerminalBackOnASignal(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT} {
		t.Run(sig.String(), func(t *testing.T) {
			child := startOnPTY(t, "list", "Esc cancel")
			if err := child.command.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			state, reported := child.wait()
			if sig == syscall.SIGQUIT {
				// Go's own way of dying of SIGQUIT: the goroutine dump.
				if state.ExitCode() != 2 || !strings.Contains(reported, "SIGQUIT") {
					t.Errorf("the child ended with %v and reported %q, want the SIGQUIT dump", state, reported)
				}
			} else if !signaled(state, sig) {
				t.Errorf("the child ended with %v, want death by %v", state, sig)
			}
			if strings.Contains(reported, "answer:") {
				t.Errorf("the prompt returned to a process that should have ended: %q", reported)
			}
			child.expectRestored()
		})
	}
}

// live's signal handler ends the process on its own account, without
// waiting for anybody but the functions registered with it. A prompt is one
// of them, so the terminal is put back before the process dies, whichever
// of the two handlers gets to the signal first.
func TestPromptPutsTheTerminalBackUnderLivesSignalHandler(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			child := startOnPTY(t, "live", "Esc cancel")
			if err := child.command.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			state, reported := child.wait()
			if !signaled(state, sig) {
				t.Errorf("the child ended with %v, want death by %v", state, sig)
			}
			if strings.Contains(reported, "answer:") {
				t.Errorf("the prompt returned to a process that should have ended: %q", reported)
			}
			child.expectRestored()
		})
	}
}

// live.RestoreAll is what an application calls where it ends on its own
// terms. An open prompt puts the terminal back with it and, the process
// still being there, returns ErrInterrupted instead of waiting for an
// answer to a question that is no longer on screen.
func TestPromptGivesWayToRestoreAll(t *testing.T) {
	child := startOnPTY(t, "restore-all", "Esc cancel")
	if err := child.command.Process.Signal(syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	state, reported := child.wait()
	if want := "answer: -1, prompt aborted: interrupted"; !state.Success() || !strings.Contains(reported, want) {
		t.Errorf("the child ended with %v and reported %q, want %q", state, reported, want)
	}
	child.expectRestored()
}

// A signal the application handles itself is left to it: the terminal is
// put back, the prompt returns ErrInterrupted, and the handler is still the
// application's.
func TestPromptLeavesAHandledSignalToTheApplication(t *testing.T) {
	child := startOnPTY(t, "handled", "Esc cancel")
	if err := child.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	state, reported := child.wait()
	if !state.Success() {
		t.Errorf("the child ended with %v, want it to live through a signal it handles", state)
	}
	for _, want := range []string{"answer: -1, prompt aborted: interrupted", "handler: terminated"} {
		if !strings.Contains(reported, want) {
			t.Errorf("the child reported %q, want %q in it", reported, want)
		}
	}
	child.expectRestored()
}

// A terminal that goes away while a prompt is up leaves nobody to answer:
// the prompt is interrupted rather than failing with the read error.
func TestPromptTakesAGoneTerminalForAnInterrupt(t *testing.T) {
	child := startOnPTY(t, "list", "Esc cancel")
	child.deaf.Store(true)
	<-child.drained
	_ = child.slave.Close()
	_ = child.master.Close()
	_, reported := child.wait()
	if want := "answer: -1, prompt aborted: interrupted"; !strings.Contains(reported, want) {
		t.Errorf("the child reported %q, want %q", reported, want)
	}
}

// Two answers that arrive in one read answer two prompts.
func TestPromptsShareKeysTypedAhead(t *testing.T) {
	child := startOnPTY(t, "two", "First: ")
	child.keys("abc\rdef\r")
	_, reported := child.wait()
	if want := "answer: \"abc\", <nil>\nanswer: \"def\", <nil>\n"; !strings.Contains(reported, want) {
		t.Errorf("the child reported %q, want %q", reported, want)
	}
	child.expectRestored()
}

// The function Status returns takes the line back once. A second call,
// deferred behind an explicit one, leaves what was written since alone.
func TestStatusErasesOnce(t *testing.T) {
	master, slave := openPTY(t)
	done := Status(slave, "Fetching releases...")
	done()
	fmt.Fprint(slave, "3 releases\n")
	done()
	_ = slave.Close()

	var drawn strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := master.Read(buf)
		drawn.Write(buf[:n])
		if err != nil {
			if !errors.Is(err, syscall.EIO) {
				t.Fatal(err)
			}
			break
		}
	}
	if got := strings.Count(drawn.String(), "\x1b[J"); got != 1 {
		t.Errorf("the line was erased %d times, want once: %q", got, drawn.String())
	}
	if !strings.HasSuffix(drawn.String(), "3 releases\r\n") {
		t.Errorf("what was written after the status line is gone: %q", drawn.String())
	}
}

// A terminal that takes no more output, as after Ctrl-S or behind a reader
// that stopped, holds a redraw up for as long as it likes, and with it the
// erasing of the block. Where the process is about to end, the mode of the
// terminal must be put back all the same: live's signal handler waits a
// moment for it and no longer, and the prompt's own watcher raises the
// signal again only once it is done.
func TestSessionPutsTheModeBackWhileOutputIsStalled(t *testing.T) {
	for _, way := range []string{"live's handler", "the prompt's own watcher"} {
		t.Run(way, func(t *testing.T) {
			master, slave := openPTY(t)
			before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
			if err != nil {
				t.Fatal(err)
			}
			s, err := openTerminal(slave, slave, true)
			if err != nil {
				t.Fatal(err)
			}
			// Nobody reads the other end, so a draw fills the terminal's
			// buffer and then blocks, holding the block's lock.
			var draws atomic.Int64
			frame := []string{strings.Repeat("x", 70), strings.Repeat("y", 70)}
			go func() {
				for s.screen.draw(frame) == nil {
					draws.Add(1)
				}
			}()
			stalled := func() bool {
				seen := draws.Load()
				time.Sleep(100 * time.Millisecond)
				return draws.Load() == seen
			}
			for deadline := time.Now().Add(10 * time.Second); !stalled(); {
				if time.Now().After(deadline) {
					t.Fatal("the terminal kept taking output")
				}
			}

			done := make(chan struct{})
			go func() {
				defer close(done)
				if way == "live's handler" {
					s.restored()
				} else {
					s.releaseNow()
				}
			}()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("the terminal was not put back while its output was stalled")
			}
			after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
			if err != nil {
				t.Fatal(err)
			}
			if *after != *before {
				t.Errorf("the terminal was left in another mode: local flags %#o, want %#o", after.Lflag, before.Lflag)
			}

			// Once the terminal takes output again, the cursor is shown, and
			// the block is not erased from a screen that moved on meanwhile.
			var drawn strings.Builder
			read := make(chan struct{})
			go func() {
				defer close(read)
				buf := make([]byte, 4096)
				for !strings.Contains(drawn.String(), showCursor) {
					n, err := master.Read(buf)
					drawn.Write(buf[:n])
					if err != nil {
						return
					}
				}
			}()
			select {
			case <-read:
			case <-time.After(10 * time.Second):
				t.Fatal("the cursor was never shown again")
			}
			if !strings.Contains(drawn.String(), showCursor) {
				t.Errorf("the cursor was not shown again: %q", drawn.String())
			}
			// A draw ends in the same sequence an erasure does, but only an
			// erasure follows the way up to the first line with it at once.
			if strings.Contains(drawn.String(), "\x1b[1A\x1b[J") {
				t.Errorf("the block was erased after the terminal had been given up: %q", drawn.String()[max(len(drawn.String())-60, 0):])
			}
			s.close()
		})
	}
}
