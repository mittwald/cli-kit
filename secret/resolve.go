package secret

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	xterm "golang.org/x/term"

	"github.com/mittwald/cli-kit/term"
)

// Source says where a [Resolver] found a secret.
type Source int

const (
	// SourceNone means no secret was found.
	SourceNone Source = iota
	// SourceFile is a file the user named, such as through --password-file.
	SourceFile
	// SourceEnv is an environment variable.
	SourceEnv
	// SourceStdin is a piped or redirected standard input, including a
	// file named "-".
	SourceStdin
	// SourceStore is the Resolver's Store, holding a secret from an
	// earlier run.
	SourceStore
	// SourcePrompt is an interactive prompt.
	SourcePrompt
)

// String returns a short lower-case name for s, such as "environment".
func (s Source) String() string {
	switch s {
	case SourceFile:
		return "file"
	case SourceEnv:
		return "environment"
	case SourceStdin:
		return "stdin"
	case SourceStore:
		return "store"
	case SourcePrompt:
		return "prompt"
	default:
		return "none"
	}
}

var (
	// ErrNoSource reports that no source offered a secret: no file, no
	// environment variable, nothing on stdin, nothing stored, and no
	// terminal to ask on. It is typical of a CI job or a service unit.
	ErrNoSource = errors.New("secret: no secret available")
	// ErrFixedSource reports, from [Resolver.Forget], that a rejected
	// secret came from a source the Resolver cannot change, such as an
	// environment variable. Asking again would return the same secret, so
	// the user has to fix the source.
	ErrFixedSource = errors.New("secret: rejected secret comes from a fixed source")
	// ErrMismatch reports that the two entries of a confirmed prompt
	// differ.
	ErrMismatch = errors.New("secret: entries do not match")
)

// DefaultMaxLen bounds what a Resolver reads from a file or stdin when
// MaxLen is zero.
const DefaultMaxLen = 4096

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

// Resolver finds one secret. See the package documentation for the order
// in which it tries its sources. All fields are optional; a source whose
// field is empty is skipped.
//
// Its errors are worded for the user of the command, such as "no password
// found in /run/secrets/pw", and match the package's sentinel errors
// ([ErrEmpty], [ErrNoSource], [ErrMismatch], [ErrFixedSource]) with
// [errors.Is].
type Resolver struct {
	// Name is the secret as the messages call it, such as "password" or
	// "API token". Empty means "secret".
	Name string
	// Label is the question an interactive prompt asks, such as "Password
	// to encrypt the export". Empty means Name with a capital first letter.
	Label string
	// Account is the name the secret is stored under in Store. It is
	// required when Store is set.
	Account string
	// Store remembers the secret between runs.
	Store Store
	// IgnoreStore skips reading Store, for a login command that should ask
	// for a fresh secret. Save still writes to it.
	IgnoreStore bool
	// Env is the name of an environment variable that may carry the secret,
	// such as "EXAMPLE_TOKEN". An empty value counts as unset.
	Env string
	// File is a path the user named to read the secret from, or "-" for
	// stdin. Only its first line is used.
	File string
	// FileFlag is the flag that sets File, such as "--password-file". It is
	// only used to word errors.
	FileFlag string
	// Stdin is the standard input to read a piped secret from; nil means
	// os.Stdin.
	Stdin *os.File
	// StdinBusy says stdin carries the command's own input, so a piped
	// stdin is not taken for the secret, and File "-" is refused.
	StdinBusy bool
	// Prompt asks interactively; nil means [TTYPrompt].
	Prompt PromptFunc
	// Hint is shown before an interactive prompt, never otherwise.
	Hint string
	// Confirm asks twice and compares, but only when prompting: a file or a
	// pipe cannot be mistyped.
	Confirm bool
	// TrimSpace removes leading and trailing white space from the secret,
	// as suits tokens. Without it only a trailing line ending is removed
	// from file and stdin input, so a passphrase keeps its spaces.
	TrimSpace bool
	// MaxLen bounds a read from a file or stdin; zero means
	// [DefaultMaxLen].
	MaxLen int
}

// Resolve returns the secret and where it came from. It never returns an
// empty secret: a source that yields an empty value is skipped, except a
// File or an interactive answer, which are errors.
func (r *Resolver) Resolve() (string, Source, error) {
	if err := r.check(); err != nil {
		return "", SourceNone, err
	}
	if value, source, err := r.explicit(); err != nil || source != SourceNone {
		return value, source, err
	}
	if !r.StdinBusy {
		stdin := r.stdin()
		if !term.IsTerminal(stdin) {
			value, err := r.readFirstLine(stdin)
			if err != nil {
				return "", SourceNone, fail(nil, err, "reading %s from stdin", r.name())
			}
			// An empty stdin (</dev/null, a closed pipe) is no secret, but it is
			// not necessarily a mistake either: fall through to the store.
			if value != "" {
				return value, SourceStdin, nil
			}
		}
	}
	if value, ok, err := r.stored(); err != nil || ok {
		if err != nil {
			return "", SourceNone, err
		}
		return value, SourceStore, nil
	}
	value, err := r.ask()
	if err != nil {
		return "", SourceNone, err
	}
	return value, SourcePrompt, nil
}

// Lookup returns the secret only when it is available without asking or
// reading stdin: from File (unless "-"), the environment or the Store. It
// returns an error wrapping [ErrNotFound] when none of them has it. Use it
// for optional work, such as an extra column only shown when a token is
// already configured.
func (r *Resolver) Lookup() (string, Source, error) {
	if err := r.check(); err != nil {
		return "", SourceNone, err
	}
	if r.File != "-" {
		if value, source, err := r.explicit(); err != nil || source != SourceNone {
			return value, source, err
		}
	} else if value, ok := r.fromEnv(); ok {
		return value, SourceEnv, nil
	}
	value, ok, err := r.stored()
	if err != nil {
		return "", SourceNone, err
	}
	if !ok {
		return "", SourceNone, ErrNotFound
	}
	return value, SourceStore, nil
}

// Save stores value in the Resolver's Store under Account. Call it once the
// secret proved valid, so a mistyped one is never remembered. Without a
// Store it does nothing.
func (r *Resolver) Save(value string) error {
	if r.Store == nil {
		return nil
	}
	if err := r.check(); err != nil {
		return err
	}
	if value == "" {
		return ErrEmpty
	}
	return r.Store.Set(r.Account, value)
}

// Forget prepares a retry after a service rejected the secret that came
// from source. A stored secret is deleted, so the next Resolve asks again;
// a prompted one needs nothing. A secret from a file, the environment or
// stdin would come back unchanged, so Forget returns an error wrapping
// [ErrFixedSource] that tells the user what to change; do not retry then.
func (r *Resolver) Forget(source Source) error {
	switch source {
	case SourceStore:
		if r.Store == nil {
			return nil
		}
		if err := r.Store.Delete(r.Account); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		return nil
	case SourcePrompt, SourceNone:
		return nil
	case SourceEnv:
		return fail(ErrFixedSource, nil, "the %s in $%s was rejected; replace it, or unset it to be asked for one",
			r.name(), r.Env)
	case SourceFile:
		return fail(ErrFixedSource, nil, "the %s in %s was rejected; replace the file's contents", r.name(), r.File)
	default:
		return fail(ErrFixedSource, nil, "the %s piped to stdin was rejected", r.name())
	}
}

func (r *Resolver) check() error {
	if r.Store != nil && r.Account == "" {
		return errors.New("secret: Resolver.Account is required with a Store")
	}
	return nil
}

func (r *Resolver) name() string {
	if r.Name == "" {
		return "secret"
	}
	return r.Name
}

// label is the prompt's question: Label, or Name with a capital first
// letter ("password" asks "Password: ", "API token" asks "API token: ").
func (r *Resolver) label() string {
	if r.Label != "" {
		return r.Label
	}
	name := r.name()
	first, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(first)) + name[size:]
}

func (r *Resolver) stdin() *os.File {
	if r.Stdin != nil {
		return r.Stdin
	}
	return os.Stdin
}

// explicit tries File and then Env. It returns SourceNone when neither is
// set.
func (r *Resolver) explicit() (string, Source, error) {
	switch {
	case r.File == "-":
		if r.StdinBusy {
			return "", SourceNone, fail(nil, nil, "%s reads the %s from stdin, which is already being read for the input; name a file instead",
				r.dashFile(), r.name())
		}
		value, err := r.readFirstLine(r.stdin())
		if err != nil {
			return "", SourceNone, fail(nil, err, "reading %s from stdin", r.name())
		}
		if value == "" {
			return "", SourceNone, fail(ErrEmpty, nil, "no %s found in stdin", r.name())
		}
		return value, SourceStdin, nil
	case r.File != "":
		f, err := os.Open(r.File)
		if err != nil {
			return "", SourceNone, fail(nil, err, "reading %s file", r.name())
		}
		defer f.Close()
		value, err := r.readFirstLine(f)
		if err != nil {
			return "", SourceNone, fail(nil, err, "reading %s from %s", r.name(), r.File)
		}
		if value == "" {
			return "", SourceNone, fail(ErrEmpty, nil, "no %s found in %s", r.name(), r.File)
		}
		return value, SourceFile, nil
	}
	if value, ok := r.fromEnv(); ok {
		return value, SourceEnv, nil
	}
	return "", SourceNone, nil
}

// dashFile names File "-" in a message: the flag with its value, or the
// bare dash.
func (r *Resolver) dashFile() string {
	if r.FileFlag == "" {
		return `the file "-"`
	}
	return r.FileFlag + " -"
}

func (r *Resolver) fromEnv() (string, bool) {
	if r.Env == "" {
		return "", false
	}
	value := os.Getenv(r.Env)
	if r.TrimSpace {
		value = strings.TrimSpace(value)
	}
	return value, value != ""
}

func (r *Resolver) stored() (string, bool, error) {
	if r.Store == nil || r.IgnoreStore {
		return "", false, nil
	}
	value, err := r.Store.Get(r.Account)
	if errors.Is(err, ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if r.TrimSpace {
		value = strings.TrimSpace(value)
	}
	return value, value != "", nil
}

// readFirstLine reads the first line of rd, bounded by MaxLen. It stops at
// the newline, so a pipe that stays open after the secret does not block,
// and the rest of stdin stays unread. A trailing CR is dropped, so a file
// written on Windows works; interior spaces are kept, because a passphrase
// may contain them.
func (r *Resolver) readFirstLine(rd io.Reader) (string, error) {
	limit := r.MaxLen
	if limit <= 0 {
		limit = DefaultMaxLen
	}
	data := make([]byte, 0, min(limit, 256))
	var b [1]byte
	for len(data) < limit {
		n, err := rd.Read(b[:])
		if n == 1 {
			if b[0] == '\n' {
				break
			}
			data = append(data, b[0])
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
	}
	line := strings.TrimSuffix(string(data), "\r")
	if r.TrimSpace {
		line = strings.TrimSpace(line)
	}
	return line, nil
}

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

// resolveError is how a Resolver reports a failure: its message is meant
// for the user of the command as it is, while errors.Is still finds the
// sentinel error it belongs to and the cause behind it.
type resolveError struct {
	msg   string
	kind  error
	cause error
}

func fail(kind, cause error, format string, args ...any) error {
	return &resolveError{msg: fmt.Sprintf(format, args...), kind: kind, cause: cause}
}

func (e *resolveError) Error() string {
	if e.cause == nil {
		return e.msg
	}
	return e.msg + ": " + e.cause.Error()
}

func (e *resolveError) Unwrap() []error {
	var errs []error
	if e.kind != nil {
		errs = append(errs, e.kind)
	}
	if e.cause != nil {
		errs = append(errs, e.cause)
	}
	return errs
}

func joinOr(options []string) string {
	if len(options) == 1 {
		return options[0]
	}
	return strings.Join(options[:len(options)-1], ", ") + ", or " + options[len(options)-1]
}

// openTTY is replaced in tests.
var (
	defaultOpenTTY = term.OpenTTY
	openTTY        = defaultOpenTTY
)

// TTYPrompt is the default [PromptFunc]. It asks on the controlling
// terminal, which works even when stdin and stdout are redirected, and
// reads the answer with echo turned off. Without a controlling terminal it
// returns an error wrapping [ErrNoSource].
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
	_, _ = io.WriteString(out, req.Prompt())
	value, err := xterm.ReadPassword(int(tty.Fd()))
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
