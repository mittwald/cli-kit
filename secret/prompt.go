package secret

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	xterm "golang.org/x/term"

	"github.com/mittwald/cli-kit/term"
)

// Request describes one interactive question for a [PromptFunc].
type Request struct {
	// Label is the question, such as "Password" or "Password to encrypt
	// the export".
	Label string
	// Name is the secret as the messages call it, such as "password". The
	// second question of a confirmed entry asks to repeat it.
	Name string
	// Hint is optional text to show before the first question, such as
	// where to create a token and which scopes it needs. The prompts of this
	// package show the URLs in it as hyperlinks (see term.Linkify).
	Hint string
	// Repeat marks the second question of a confirmed entry.
	Repeat bool
}

// Prompt returns the text to put in front of the input field: "Password to
// encrypt the export: ", or "Repeat password: " for the second question of a
// confirmed entry.
func (r Request) Prompt() string {
	if r.Repeat {
		name := r.Name
		if name == "" {
			name = r.Label
		}
		return "Repeat " + name + ": "
	}
	return r.Label + ": "
}

// PromptFunc asks the user for a secret without echoing it. It returns an
// error wrapping [ErrNoSource] when there is no terminal to ask on.
//
// [TTYPrompt] is the default. A tool with its own input widget, such as
// pick.Input from this module, turns it into a PromptFunc with
// [InputPrompt].
type PromptFunc func(Request) (string, error)

func (r *Resolver) ask() (string, error) {
	prompt := r.Prompt
	if prompt == nil {
		prompt = TTYPrompt
	}
	req := Request{Label: r.label(), Name: r.name(), Hint: r.Hint}
	value, err := prompt(req)
	if err != nil {
		return "", r.promptFailed(err)
	}
	if r.TrimSpace {
		value = strings.TrimSpace(value)
	}
	if value == "" {
		return "", fail(ErrEmpty, nil, "empty %s", r.name())
	}
	if r.Confirm {
		req.Repeat = true
		again, err := prompt(req)
		if err != nil {
			return "", r.promptFailed(err)
		}
		if r.TrimSpace {
			again = strings.TrimSpace(again)
		}
		if again != value {
			return "", fail(ErrMismatch, nil, "%ss do not match", r.name())
		}
	}
	return value, nil
}

// promptFailed words a failed prompt. A missing terminal becomes the list
// of sources the user could have offered instead.
func (r *Resolver) promptFailed(err error) error {
	if !errors.Is(err, ErrNoSource) {
		return fail(nil, err, "reading %s", r.name())
	}
	var options []string
	if r.Env != "" {
		options = append(options, "set $"+r.Env)
	}
	if r.FileFlag != "" {
		options = append(options, "pass "+r.FileFlag)
	}
	if !r.StdinBusy {
		options = append(options, "pipe one to stdin")
	}
	options = append(options, "run this on a terminal")
	return fail(ErrNoSource, nil, "no %s available: %s", r.name(), joinOr(options))
}

// TTYPrompt is the default [PromptFunc]. It asks on the controlling
// terminal, which works even when stdin and stdout are redirected, and
// reads the answer with echo turned off. Without a controlling terminal it
// returns an error wrapping [ErrNoSource].
//
// The terminal gets its echo back when the process ends at the prompt, too.
// While TTYPrompt waits for the answer, the restoring is registered with
// live.OnRestore of this module, so it happens wherever a program lets
// package live clean up: in the signal handler of live.InstallSignalRestore
// before the signal ends the process, in live.RestoreAll, and in
// Cursor.RestoreOnPanic. Call live.RestoreAll only on the way out of the
// process: at a prompt that goes on waiting afterwards, the answer would be
// echoed.
//
// On Unix TTYPrompt also watches SIGINT (Ctrl-C), SIGTERM and SIGHUP
// itself, for programs without that handler: it restores the terminal, and
// then the signal ends the process as it does by default, also in a program
// that handles the signal in its own way, since the read of the answer
// cannot be interrupted otherwise. A signal the process ignores stays
// ignored. On other systems a process that is killed at the prompt leaves
// the console as it is, unless live's handler is installed.
func TTYPrompt(req Request) (string, error) {
	tty, err := openTTY()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNoSource, err)
	}
	defer tty.Close()
	if !term.IsTerminal(tty) {
		return "", ErrNoSource
	}
	out, closeOut := promptOutput(tty)
	defer closeOut()
	if req.Hint != "" && !req.Repeat {
		_, _ = fmt.Fprintln(term.Links(out), req.Hint)
	}
	stop := restoreOnExit(tty, out)
	_, _ = io.WriteString(out, req.Prompt())
	value, err := xterm.ReadPassword(int(tty.Fd()))
	stop()
	// The terminal swallowed the Enter key along with the echo, so the
	// cursor is still on the prompt line.
	_, _ = io.WriteString(out, "\n")
	if err != nil {
		return "", fmt.Errorf("terminal: %w", err)
	}
	return string(value), nil
}

// InputPrompt returns a [PromptFunc] that asks with input, a text field
// that draws nothing for what is typed, on in and out. With pick.Input from
// this module:
//
//	prompt := secret.InputPrompt(os.Stdin, os.Stderr,
//		func(in, out *os.File, prompt string) (string, error) {
//			return pick.Input(in, out, pick.InputOptions{Prompt: prompt, Secret: true})
//		})
//
// Unlike [TTYPrompt] it asks on in and out only. Where either is not a
// terminal it returns [ErrNoSource] without calling input, so a [Resolver]
// names the sources the user could offer instead. Before the first question
// of a request it writes the request's Hint to out, its URLs as hyperlinks.
// The question passed to input is [Request.Prompt]; input's errors are
// returned as they are.
func InputPrompt(in, out *os.File, input func(in, out *os.File, prompt string) (string, error)) PromptFunc {
	return func(req Request) (string, error) {
		if !term.Interactive(in, out) {
			return "", ErrNoSource
		}
		if req.Hint != "" && !req.Repeat {
			_, _ = fmt.Fprintln(term.Links(out), req.Hint)
		}
		return input(in, out, req.Prompt())
	}
}
