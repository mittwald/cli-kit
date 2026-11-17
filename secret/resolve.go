package secret

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

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
	// ErrNotText reports a file or a stdin that does not hold a line of
	// text: it starts with a UTF-16 byte order mark, as files written by
	// some Windows tools do, or contains a NUL byte.
	ErrNotText = errors.New("secret: not a line of text")
)

// What readFirstLine refuses as [ErrNotText].
var (
	errUTF16 = fmt.Errorf("%w: UTF-16", ErrNotText)
	errNUL   = fmt.Errorf("%w: NUL byte", ErrNotText)
)

// DefaultMaxLen is the longest line, in bytes, a Resolver takes from a file
// or stdin when MaxLen is zero.
const DefaultMaxLen = 4096

// Resolver finds one secret. See the package documentation for the order
// in which it tries its sources. All fields are optional; a source whose
// field is empty is skipped.
//
// Mind the standard input. It comes before the Store, so that a piped
// secret wins over a remembered one, and that has a price: wherever stdin
// is not a terminal, Resolve reads a line from it, whatever that line is.
// A tool run in a shell loop that reads stdin (while read host; do
// example-tool deploy "$host"; done <hosts), in a Git hook that is handed
// its input on stdin, or over ssh without a terminal takes the next line of
// that input for the secret, or waits for a line that never comes, although
// the Store holds the secret. So:
//
//   - A command that reads its own input from stdin sets StdinBusy.
//   - A tool that should take a secret from stdin only when asked to sets
//     StdinBusy unless the user named the file "-", as in
//     StdinBusy: passwordFile != "-". Its users then write
//     --password-file - to pipe a secret in.
//   - Otherwise tell the users: a script that calls the tool where stdin is
//     not theirs to give away runs it with </dev/null.
//
// Its errors are worded for the user of the command, such as "no password
// found in /run/secrets/pw", and match the package's sentinel errors
// ([ErrEmpty], [ErrNoSource], [ErrMismatch], [ErrFixedSource],
// [ErrTooLong], [ErrNotText]) with [errors.Is]. They never contain the
// secret. An error about a File that could not be opened does not repeat
// its path either, since a user who passes the secret where its file's name
// belongs would see it printed.
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
	// only used to word errors, which name it in place of a file that
	// could not be opened.
	FileFlag string
	// Stdin is the standard input to read a piped secret from; nil means
	// os.Stdin.
	Stdin *os.File
	// StdinBusy says stdin carries the command's own input, or is otherwise
	// not to be taken for the secret: a stdin that is no terminal is left
	// unread, and File "-" is refused. See the note on stdin above.
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
	// from file and stdin input, so a passphrase keeps its spaces. A UTF-8
	// byte order mark at the start of a file or stdin is always dropped.
	TrimSpace bool
	// MaxLen is the longest line, in bytes without its line ending, taken
	// from a file or stdin; zero means [DefaultMaxLen]. A longer line is an
	// error wrapping [ErrTooLong], never a secret cut short.
	MaxLen int
}

// Resolve returns the secret and where it came from. It never returns an
// empty secret: a source that yields an empty value is skipped, except a
// File or an interactive answer, which are errors.
//
// A Store that cannot be read, for any reason but holding no secret, does
// not end the search: Resolve asks instead, since the user may still know
// the secret. When that fails too, such as without a terminal, the error
// tells both and matches both with [errors.Is]: what the prompt reported
// and the Store's failure.
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
				return "", SourceNone, r.readFailed(err, "stdin")
			}
			// An empty stdin (</dev/null, a closed pipe) is no secret, but it is
			// not necessarily a mistake either: fall through to the store.
			if value != "" {
				return value, SourceStdin, nil
			}
		}
	}
	value, ok, storeErr := r.stored()
	if ok {
		return value, SourceStore, nil
	}
	// A store that failed does not end the search: the user may know the
	// secret, and a prompt that fails says what else to offer.
	value, err := r.ask()
	if err != nil {
		if storeErr != nil {
			err = &resolveError{msg: err.Error() + "; the stored " + r.name() + " could not be read",
				kind: err, cause: storeErr}
		}
		return "", SourceNone, err
	}
	return value, SourcePrompt, nil
}

// Lookup returns the secret only when it is available without asking or
// reading stdin: from File (unless "-"), the environment or the Store. It
// returns an error wrapping [ErrNotFound] when none of them has it, and the
// Store's own error when the Store could not be read. Use it for optional
// work, such as an extra column only shown when a token is already
// configured.
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
// Store it does nothing. A Store that failed while Resolve looked for the
// secret usually fails here as well; Save returns that error.
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
// when the Store cannot delete it, Forget returns the Store's error, and a
// retry would only find the same secret. A prompted secret needs nothing. A
// secret from a file, the environment or stdin would come back unchanged,
// so Forget returns an error wrapping [ErrFixedSource] that tells the user
// what to change; do not retry then.
//
// Pass the source Resolve returned. The message for [SourceFile] names
// File, which is the path of a file Resolve has read by then.
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
			return "", SourceNone, r.readFailed(err, "stdin")
		}
		if value == "" {
			return "", SourceNone, fail(ErrEmpty, nil, "no %s found in stdin", r.name())
		}
		return value, SourceStdin, nil
	case r.File != "":
		f, err := os.Open(r.File)
		if err != nil {
			// Not the path: until the file opened, File may be anything the
			// user typed after the flag, the secret itself included.
			return "", SourceNone, fail(nil, withoutPath(err), "cannot read %s", r.fileSetting())
		}
		defer f.Close()
		// From here on File is known to name a file.
		value, err := r.readFirstLine(f)
		if err != nil {
			return "", SourceNone, r.readFailed(err, r.File)
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

// fileSetting names File in a message without repeating its value: "the
// file named by --password-file", or "the password file".
func (r *Resolver) fileSetting() string {
	if r.FileFlag == "" {
		return "the " + r.name() + " file"
	}
	return "the file named by " + r.FileFlag
}

// withoutPath returns what went wrong with a file without the file's path,
// which an *fs.PathError carries.
func withoutPath(err error) error {
	if pathErr, ok := errors.AsType[*fs.PathError](err); ok {
		return pathErr.Err
	}
	return err
}

// readFailed words a failed readFirstLine. where is "stdin" or the path of
// a file that was opened.
func (r *Resolver) readFailed(err error, where string) error {
	switch {
	case errors.Is(err, ErrTooLong):
		return fail(ErrTooLong, nil, "the %s in %s is longer than %d bytes", r.name(), where, r.maxLen())
	case errors.Is(err, errUTF16):
		return fail(ErrNotText, nil, "the %s in %s is UTF-16 text; save it as UTF-8", r.name(), where)
	case errors.Is(err, ErrNotText):
		return fail(ErrNotText, nil, "the %s in %s contains a NUL byte; it has to be a line of text", r.name(), where)
	}
	return fail(nil, err, "reading %s from %s", r.name(), where)
}

func (r *Resolver) maxLen() int {
	if r.MaxLen <= 0 {
		return DefaultMaxLen
	}
	return r.MaxLen
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
