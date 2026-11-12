package secret

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// stdinFile returns a file holding what a pipe would have carried. A real
// file answers "not a terminal", exactly like a redirect.
func stdinFile(t *testing.T, content string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pw")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// answers returns a PromptFunc that hands out the given answers in order
// and records the requests it was asked.
func answers(asked *[]Request, values ...string) PromptFunc {
	return func(req Request) (string, error) {
		*asked = append(*asked, req)
		if len(values) == 0 {
			return "", errors.New("unexpected prompt")
		}
		v := values[0]
		values = values[1:]
		return v, nil
	}
}

func noPrompt(t *testing.T) PromptFunc {
	return func(Request) (string, error) {
		t.Helper()
		t.Fatal("prompted although another source had the secret")
		return "", nil
	}
}

func TestResolveOrder(t *testing.T) {
	t.Setenv("EXAMPLE_TOKEN", "from-env")
	store := NewMemory(map[string]string{"default": "from-store"})
	file := writeFile(t, "from-file\n")

	r := Resolver{Label: "Token", Account: "default", Store: store, Env: "EXAMPLE_TOKEN",
		File: file, Stdin: stdinFile(t, "from-stdin\n"), Prompt: noPrompt(t)}
	check := func(want string, wantSource Source) {
		t.Helper()
		got, source, err := r.Resolve()
		if err != nil || got != want || source != wantSource {
			t.Fatalf("Resolve = %q, %v, %v; want %q from %v", got, source, err, want, wantSource)
		}
	}
	check("from-file", SourceFile)
	r.File = ""
	check("from-env", SourceEnv)
	t.Setenv("EXAMPLE_TOKEN", "")
	check("from-stdin", SourceStdin)
	r.Stdin = stdinFile(t, "")
	check("from-store", SourceStore)

	var asked []Request
	r.Stdin = stdinFile(t, "")
	r.IgnoreStore = true
	r.Prompt = answers(&asked, "typed")
	check("typed", SourcePrompt)
	if len(asked) != 1 || asked[0].Label != "Token" || asked[0].Repeat {
		t.Fatalf("asked %+v", asked)
	}
}

func TestResolveFirstLineOnlyAndKeepsSpaces(t *testing.T) {
	r := Resolver{Stdin: stdinFile(t, "hunter2 with spaces\r\ntrailing junk\n")}
	got, source, err := r.Resolve()
	if err != nil || got != "hunter2 with spaces" || source != SourceStdin {
		t.Fatalf("Resolve = %q, %v, %v", got, source, err)
	}
}

func TestResolveTrimSpace(t *testing.T) {
	t.Setenv("EXAMPLE_TOKEN", "  tok \t")
	r := Resolver{Env: "EXAMPLE_TOKEN", TrimSpace: true}
	if got, _, err := r.Resolve(); err != nil || got != "tok" {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
}

// A line longer than MaxLen is an error, never a secret cut short, and
// its rest is not left for the next reader to take for a secret.
func TestResolveMaxLen(t *testing.T) {
	long := strings.Repeat("x", 100)
	r := Resolver{Name: "token", Stdin: stdinFile(t, long+"\nnext\n"), MaxLen: 10}
	got, source, err := r.Resolve()
	if !errors.Is(err, ErrTooLong) || got != "" || source != SourceNone {
		t.Fatalf("Resolve = %q, %v, %v; want ErrTooLong", got, source, err)
	}
	if want := "the token in stdin is longer than 10 bytes"; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
	file := writeFile(t, long)
	r = Resolver{Name: "token", File: file, MaxLen: 10, Prompt: noPrompt(t)}
	if _, _, err := r.Resolve(); !errors.Is(err, ErrTooLong) || err.Error() != "the token in "+file+" is longer than 10 bytes" {
		t.Errorf("Resolve from a file = %v, want ErrTooLong naming the file", err)
	}
	r = Resolver{File: writeFile(t, strings.Repeat("x", DefaultMaxLen+1)+"\n")}
	if _, _, err := r.Resolve(); !errors.Is(err, ErrTooLong) {
		t.Errorf("Resolve with the default limit = %v, want ErrTooLong", err)
	}

	// The limit counts the secret, not its line ending or a byte order mark.
	exact := strings.Repeat("x", 10)
	for _, content := range []string{exact, exact + "\n", exact + "\r\n", exact + "\r", "\xef\xbb\xbf" + exact + "\r\n"} {
		r := Resolver{Stdin: stdinFile(t, content), MaxLen: 10}
		if got, _, err := r.Resolve(); err != nil || got != exact {
			t.Errorf("Resolve of %q = %q, %v; want the %d bytes", content, got, err, len(exact))
		}
	}
	// A byte order mark is longer than a short limit: its bytes do not count
	// while they may still become one, and count once they did not.
	const bom = "\xef\xbb\xbf"
	for _, tc := range []struct {
		content, want string
		tooLong       bool
	}{
		{content: bom + "x\n", want: "x"},
		{content: bom + "x", want: "x"},
		{content: bom + "x\r\n", want: "x"},
		{content: "\xef", want: "\xef"},
		{content: bom + "xy\n", tooLong: true},
		{content: "\xef\xbbx\n", tooLong: true},
		{content: "\xef\xbb\n", tooLong: true},
		{content: "\xef\xbb", tooLong: true},
		{content: "\xefx", tooLong: true},
	} {
		r := Resolver{Stdin: stdinFile(t, tc.content), MaxLen: 1}
		got, _, err := r.Resolve()
		if tc.tooLong {
			if !errors.Is(err, ErrTooLong) {
				t.Errorf("Resolve of %q with MaxLen 1 = %q, %v; want ErrTooLong", tc.content, got, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("Resolve of %q with MaxLen 1 = %q, %v; want %q", tc.content, got, err, tc.want)
		}
	}
	r = Resolver{Stdin: stdinFile(t, exact+"\rx\n"), MaxLen: 10}
	if _, _, err := r.Resolve(); !errors.Is(err, ErrTooLong) {
		t.Errorf("Resolve of a line with a CR inside = %v, want ErrTooLong", err)
	}
}

// A byte order mark is no part of the secret, and a file in an encoding
// the secret cannot be read from is refused instead of handed on as bytes.
func TestResolveInputThatIsNoPlainText(t *testing.T) {
	for _, trim := range []bool{false, true} {
		r := Resolver{File: writeFile(t, "\xef\xbb\xbftok123\r\n"), TrimSpace: trim}
		if got, source, err := r.Resolve(); err != nil || got != "tok123" || source != SourceFile {
			t.Errorf("TrimSpace %v: Resolve of a file with a byte order mark = %q, %v, %v", trim, got, source, err)
		}
		r = Resolver{Stdin: stdinFile(t, "\xef\xbb\xbftok123\n"), TrimSpace: trim}
		if got, _, err := r.Resolve(); err != nil || got != "tok123" {
			t.Errorf("TrimSpace %v: Resolve of a stdin with a byte order mark = %q, %v", trim, got, err)
		}
	}
	// Only the mark, as an editor saves an empty file: no secret.
	r := Resolver{File: writeFile(t, "\xef\xbb\xbf\n")}
	if _, _, err := r.Resolve(); !errors.Is(err, ErrEmpty) {
		t.Errorf("Resolve of a file holding only a byte order mark = %v, want ErrEmpty", err)
	}

	utf16le := "\xff\xfet\x00o\x00k\x00\r\x00\n\x00" // what Windows PowerShell's > writes
	utf16be := "\xfe\xff\x00t\x00o\x00k\x00\n"
	for name, tc := range map[string]struct{ content, want string }{
		"UTF-16 little endian": {utf16le, "is UTF-16 text; save it as UTF-8"},
		"UTF-16 big endian":    {utf16be, "is UTF-16 text; save it as UTF-8"},
		"NUL byte":             {"t\x00o\x00k\x00\n", "contains a NUL byte; it has to be a line of text"},
		"long UTF-16":          {"\xff\xfe" + strings.Repeat("t\x00", DefaultMaxLen), "is UTF-16 text; save it as UTF-8"},
	} {
		file := writeFile(t, tc.content)
		r := Resolver{Name: "token", File: file, TrimSpace: true, Prompt: noPrompt(t)}
		got, source, err := r.Resolve()
		if !errors.Is(err, ErrNotText) || got != "" || source != SourceNone {
			t.Errorf("%s: Resolve = %q, %v, %v; want ErrNotText", name, got, source, err)
			continue
		}
		if want := "the token in " + file + " " + tc.want; err.Error() != want {
			t.Errorf("%s: error = %q, want %q", name, err, want)
		}
		r = Resolver{Name: "token", Stdin: stdinFile(t, tc.content), Prompt: noPrompt(t)}
		if _, _, err := r.Resolve(); !errors.Is(err, ErrNotText) || !strings.HasPrefix(err.Error(), "the token in stdin ") {
			t.Errorf("%s: Resolve from stdin = %v, want ErrNotText", name, err)
		}
	}
}

// A store that cannot be read does not keep the user from typing the
// secret; and when nobody can be asked, the store's failure is not lost.
func TestResolveAsksWhenTheStoreFails(t *testing.T) {
	insecure := privateDir(t)
	if err := os.WriteFile(filepath.Join(insecure, "default"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	noHome := errors.New("locate the home directory: $HOME is not defined")
	for name, tc := range map[string]struct {
		store Store
		is    error
	}{
		"keyring without fallback":        {failing{}, errUnavailable},
		"both stores failing":             {Fallback(failing{}, Unavailable(noHome)), noHome},
		"a file the store refuses":        {Fallback(&Memory{}, &FileStore{Dir: insecure}), ErrInsecureFile},
		"an account the file store lacks": {Fallback(failing{}, &FileStore{Dir: privateDir(t)}), errUnavailable},
	} {
		if errors.Is(tc.is, ErrInsecureFile) && runtime.GOOS == "windows" {
			continue // POSIX permissions
		}
		account := "default"
		if name == "an account the file store lacks" {
			account = "https://api.example.com/"
		}
		var asked []Request
		r := Resolver{Name: "token", Account: account, Store: tc.store, Stdin: stdinFile(t, ""), Prompt: answers(&asked, "typed")}
		got, source, err := r.Resolve()
		if err != nil || got != "typed" || source != SourcePrompt || len(asked) != 1 {
			t.Errorf("%s: Resolve = %q, %v, %v after %d questions; want the prompt's answer", name, got, source, err, len(asked))
		}

		// Nobody to ask: both reasons are in the error.
		r.Env, r.Prompt = "EXAMPLE_TOKEN", func(Request) (string, error) { return "", ErrNoSource }
		_, source, err = r.Resolve()
		if !errors.Is(err, ErrNoSource) || !errors.Is(err, tc.is) || source != SourceNone {
			t.Errorf("%s: Resolve without a terminal = %v, %v; want ErrNoSource and the store's failure", name, source, err)
			continue
		}
		const start = "no token available: set $EXAMPLE_TOKEN, pipe one to stdin, or run this on a terminal; the stored token could not be read: "
		if !strings.HasPrefix(err.Error(), start) {
			t.Errorf("%s: error = %q, want it to start with %q", name, err, start)
		}

		// Any other way the prompt fails keeps the store's failure as well.
		r.Prompt = answers(new([]Request), "")
		if _, _, err := r.Resolve(); !errors.Is(err, ErrEmpty) || !errors.Is(err, tc.is) {
			t.Errorf("%s: Resolve with an empty answer = %v; want ErrEmpty and the store's failure", name, err)
		}
		// Lookup never asks, so it reports the store.
		if _, _, err := r.Lookup(); !errors.Is(err, tc.is) || errors.Is(err, ErrNotFound) {
			t.Errorf("%s: Lookup = %v, want the store's failure", name, err)
		}
	}
}

// What follows the flag is not repeated until it is known to name a file:
// a user who passes the password where its file belongs must not find it
// in the output or a log.
func TestResolveDoesNotRepeatAFileThatDoesNotOpen(t *testing.T) {
	const mistake = "hunter2-the-real-password"
	for _, r := range []Resolver{
		{Name: "password", File: mistake, FileFlag: "--password-file"},
		{Name: "password", File: mistake},
		{Name: "password", File: mistake + "/x"},
		{Name: "password", File: mistake + "\x00"},
		{Name: "password", File: strings.Repeat(mistake, 400)},
	} {
		for name, call := range map[string]func() (string, Source, error){"Resolve": r.Resolve, "Lookup": r.Lookup} {
			_, _, err := call()
			if err == nil {
				t.Fatalf("%s with File %q succeeded", name, r.File)
			}
			if containsSecret(err, mistake) {
				t.Errorf("%s: error %q repeats the File argument", name, err)
			}
		}
	}
}

func TestResolveDashReadsStdin(t *testing.T) {
	r := Resolver{File: "-", Stdin: stdinFile(t, "piped\n")}
	if got, source, err := r.Resolve(); err != nil || got != "piped" || source != SourceStdin {
		t.Fatalf("Resolve = %q, %v, %v", got, source, err)
	}
	r = Resolver{File: "-", FileFlag: "--password-file", StdinBusy: true, Stdin: stdinFile(t, "payload")}
	if _, _, err := r.Resolve(); err == nil || !strings.Contains(err.Error(), "--password-file -") {
		t.Fatalf("Resolve with busy stdin = %v, want refusal naming the flag", err)
	}
}

func TestResolveBusyStdinIsNotRead(t *testing.T) {
	var asked []Request
	stdin := stdinFile(t, "the command's input\n")
	r := Resolver{Stdin: stdin, StdinBusy: true, Prompt: answers(&asked, "typed")}
	if got, source, err := r.Resolve(); err != nil || got != "typed" || source != SourcePrompt {
		t.Fatalf("Resolve = %q, %v, %v", got, source, err)
	}
	rest := make([]byte, 64)
	if n, _ := stdin.Read(rest); string(rest[:n]) != "the command's input\n" {
		t.Fatalf("stdin was consumed: left %q", rest[:n])
	}
}

func TestResolveEmptyFileIsAnError(t *testing.T) {
	r := Resolver{File: writeFile(t, "\n"), Prompt: noPrompt(t)}
	if _, _, err := r.Resolve(); !errors.Is(err, ErrEmpty) {
		t.Fatalf("Resolve = %v, want ErrEmpty", err)
	}
}

func TestResolveNeverReturnsAnEmptyAnswer(t *testing.T) {
	var asked []Request
	r := Resolver{Stdin: stdinFile(t, ""), Prompt: answers(&asked, "")}
	if _, _, err := r.Resolve(); !errors.Is(err, ErrEmpty) {
		t.Fatalf("Resolve = %v, want ErrEmpty", err)
	}
}

func TestResolveConfirm(t *testing.T) {
	var asked []Request
	r := Resolver{Name: "password", Label: "Password to encrypt the export", Stdin: stdinFile(t, ""),
		Confirm: true, Prompt: answers(&asked, "a", "a")}
	if got, _, err := r.Resolve(); err != nil || got != "a" {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
	if len(asked) != 2 || asked[0].Prompt() != "Password to encrypt the export: " ||
		!asked[1].Repeat || asked[1].Prompt() != "Repeat password: " {
		t.Fatalf("asked %+v", asked)
	}

	asked = nil
	r.Prompt = answers(&asked, "a", "b")
	if _, _, err := r.Resolve(); !errors.Is(err, ErrMismatch) {
		t.Fatalf("Resolve = %v, want ErrMismatch", err)
	}

	// A piped secret cannot be mistyped twice the same way; asking again
	// would hang a script.
	r.Stdin = stdinFile(t, "piped\n")
	r.Prompt = noPrompt(t)
	if got, _, err := r.Resolve(); err != nil || got != "piped" {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
}

func TestResolveWithoutAnySourceNamesTheOptions(t *testing.T) {
	openTTY = func() (*os.File, error) { return nil, errors.New("no /dev/tty") }
	t.Cleanup(func() { openTTY = defaultOpenTTY })
	t.Setenv("EXAMPLE_TOKEN", "")

	r := Resolver{Name: "token", Env: "EXAMPLE_TOKEN", FileFlag: "--token-file", Stdin: stdinFile(t, "")}
	_, _, err := r.Resolve()
	if !errors.Is(err, ErrNoSource) {
		t.Fatalf("Resolve = %v, want ErrNoSource", err)
	}
	const want = "no token available: set $EXAMPLE_TOKEN, pass --token-file, pipe one to stdin, or run this on a terminal"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
	// With stdin taken, piping is no option.
	r.StdinBusy = true
	if _, _, err := r.Resolve(); err == nil || strings.Contains(err.Error(), "stdin") {
		t.Errorf("busy stdin: error = %v, want no stdin option", err)
	}
}

// The messages are meant for the user of the command as they are, and
// still match the package's sentinel errors.
func TestResolveMessages(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	_, openErr := os.Open(missing)
	// What is wrong with the file, which is worded differently between
	// systems, without the file's path.
	noSuchFile := strings.TrimPrefix(openErr.Error(), "open "+missing+": ")
	empty := writeFile(t, "\n")
	for _, tc := range []struct {
		name string
		r    Resolver
		want string
		is   error
	}{
		{"missing file", Resolver{Name: "password", File: missing},
			"cannot read the password file: " + noSuchFile, fs.ErrNotExist},
		{"missing file named by a flag", Resolver{Name: "password", File: missing, FileFlag: "--password-file"},
			"cannot read the file named by --password-file: " + noSuchFile, fs.ErrNotExist},
		{"empty file", Resolver{Name: "password", File: empty}, "no password found in " + empty, ErrEmpty},
		{"dash with busy stdin", Resolver{Name: "password", File: "-", FileFlag: "--password-file", StdinBusy: true},
			"--password-file - reads the password from stdin, which is already being read for the input; name a file instead", nil},
		{"dash without flag", Resolver{Name: "password", File: "-", StdinBusy: true},
			`the file "-" reads the password from stdin, which is already being read for the input; name a file instead`, nil},
		{"dash with empty stdin", Resolver{Name: "password", File: "-", Stdin: stdinFile(t, "")}, "no password found in stdin", ErrEmpty},
		{"empty answer", Resolver{Name: "password", Stdin: stdinFile(t, ""), Prompt: answers(new([]Request), "")},
			"empty password", ErrEmpty},
		{"mismatch", Resolver{Name: "password", Stdin: stdinFile(t, ""), Confirm: true, Prompt: answers(new([]Request), "a", "b")},
			"passwords do not match", ErrMismatch},
		{"failing prompt", Resolver{Name: "password", Stdin: stdinFile(t, ""),
			Prompt: func(Request) (string, error) { return "", errors.New("terminal: EOF") }},
			"reading password: terminal: EOF", nil},
		{"default name", Resolver{File: empty}, "no secret found in " + empty, ErrEmpty},
	} {
		_, _, err := tc.r.Resolve()
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s: error = %v, want %q", tc.name, err, tc.want)
		}
		if tc.is != nil && !errors.Is(err, tc.is) {
			t.Errorf("%s: error %v does not match %v", tc.name, err, tc.is)
		}
	}
}

func TestResolveLabelDefaultsToName(t *testing.T) {
	for _, tc := range []struct{ name, prompt string }{
		{"password", "Password: "}, {"API token", "API token: "}, {"", "Secret: "},
	} {
		var asked []Request
		r := Resolver{Name: tc.name, Stdin: stdinFile(t, ""), Prompt: answers(&asked, "x")}
		if _, _, err := r.Resolve(); err != nil || len(asked) != 1 || asked[0].Prompt() != tc.prompt {
			t.Errorf("Name %q: asked %+v, %v; want prompt %q", tc.name, asked, err, tc.prompt)
		}
	}
}

func TestResolveRequiresAccountWithStore(t *testing.T) {
	r := Resolver{Store: &Memory{}}
	if _, _, err := r.Resolve(); err == nil {
		t.Fatal("Resolve without Account succeeded")
	}
}

func TestLookupNeverPromptsOrReadsStdin(t *testing.T) {
	stdin := stdinFile(t, "piped\n")
	r := Resolver{Account: "default", Store: &Memory{}, Stdin: stdin, Prompt: noPrompt(t)}
	if _, _, err := r.Lookup(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Lookup = %v, want ErrNotFound", err)
	}
	if err := r.Save("saved"); err != nil {
		t.Fatal(err)
	}
	if got, source, err := r.Lookup(); err != nil || got != "saved" || source != SourceStore {
		t.Fatalf("Lookup = %q, %v, %v", got, source, err)
	}
	t.Setenv("EXAMPLE_TOKEN", "env")
	r.Env = "EXAMPLE_TOKEN"
	r.File = "-"
	if got, source, err := r.Lookup(); err != nil || got != "env" || source != SourceEnv {
		t.Fatalf("Lookup = %q, %v, %v", got, source, err)
	}
	rest := make([]byte, 16)
	if n, _ := stdin.Read(rest); string(rest[:n]) != "piped\n" {
		t.Fatal("Lookup read stdin")
	}
}

func TestSaveAndForget(t *testing.T) {
	store := &Memory{}
	r := Resolver{Name: "token", Account: "default", Store: store, Env: "EXAMPLE_TOKEN", File: "/run/secrets/token"}
	if err := r.Save(""); !errors.Is(err, ErrEmpty) {
		t.Fatalf("Save empty = %v", err)
	}
	if err := r.Save("tok"); err != nil {
		t.Fatal(err)
	}
	if err := r.Forget(SourceStore); err != nil {
		t.Fatalf("Forget store = %v", err)
	}
	if _, err := store.Get("default"); !errors.Is(err, ErrNotFound) {
		t.Fatal("Forget left the stored secret")
	}
	if err := r.Forget(SourceStore); err != nil {
		t.Fatalf("Forget twice = %v", err)
	}
	if err := r.Forget(SourcePrompt); err != nil {
		t.Fatalf("Forget prompt = %v", err)
	}
	for source, want := range map[Source]string{
		SourceEnv:   "the token in $EXAMPLE_TOKEN was rejected; replace it, or unset it to be asked for one",
		SourceFile:  "the token in /run/secrets/token was rejected; replace the file's contents",
		SourceStdin: "the token piped to stdin was rejected",
	} {
		err := r.Forget(source)
		if !errors.Is(err, ErrFixedSource) || err.Error() != want {
			t.Errorf("Forget(%v) = %v, want ErrFixedSource saying %q", source, err, want)
		}
	}
	if err := (&Resolver{}).Save("x"); err != nil {
		t.Fatalf("Save without store = %v", err)
	}
}

func TestSourceString(t *testing.T) {
	for source, want := range map[Source]string{SourceNone: "none", SourceFile: "file", SourceEnv: "environment",
		SourceStdin: "stdin", SourceStore: "store", SourcePrompt: "prompt"} {
		if source.String() != want {
			t.Errorf("%d.String() = %q, want %q", source, source.String(), want)
		}
	}
}

func TestTTYPromptWithoutTerminal(t *testing.T) {
	openTTY = func() (*os.File, error) { return nil, errors.New("no /dev/tty") }
	t.Cleanup(func() { openTTY = defaultOpenTTY })
	if _, err := TTYPrompt(Request{Label: "Password"}); !errors.Is(err, ErrNoSource) {
		t.Fatalf("TTYPrompt = %v, want ErrNoSource", err)
	}
	// A file that opens but is no terminal is no place to ask either.
	openTTY = func() (*os.File, error) { return os.Open(writeFile(t, "x")) }
	if _, err := TTYPrompt(Request{Label: "Password"}); !errors.Is(err, ErrNoSource) {
		t.Fatalf("TTYPrompt on a file = %v, want ErrNoSource", err)
	}
}

func TestResolvePromptsWhenFallbackStoreIsEmpty(t *testing.T) {
	var asked []Request
	r := Resolver{Account: "default", Store: Fallback(failing{}, &Memory{}),
		Stdin: stdinFile(t, ""), Prompt: answers(&asked, "typed")}
	if got, source, err := r.Resolve(); err != nil || got != "typed" || source != SourcePrompt {
		t.Fatalf("Resolve = %q, %v, %v; want the prompt's answer", got, source, err)
	}
}

func TestResolveStopsAtNewlineOfAnOpenPipe(t *testing.T) {
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer rd.Close()
	defer wr.Close()
	if _, err := wr.WriteString("piped\nmore input\n"); err != nil {
		t.Fatal(err)
	}
	type result struct {
		value string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		r := Resolver{Stdin: rd}
		value, _, err := r.Resolve()
		done <- result{value, err}
	}()
	select {
	case res := <-done:
		if res.err != nil || res.value != "piped" {
			t.Fatalf("Resolve = %q, %v", res.value, res.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Resolve waited for the pipe to close")
	}
	rest := make([]byte, 64)
	if n, _ := rd.Read(rest); string(rest[:n]) != "more input\n" {
		t.Fatalf("Resolve consumed past the first line: left %q", rest[:n])
	}
}

// Off a terminal InputPrompt asks nothing and writes nothing, and the
// Resolver names the sources the user could offer instead.
func TestInputPromptWithoutTerminal(t *testing.T) {
	in, err := os.Open(writeFile(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	prompt := InputPrompt(in, out, func(*os.File, *os.File, string) (string, error) {
		t.Error("input was called without a terminal")
		return "", nil
	})
	if _, err := prompt(Request{Label: "Password", Hint: "hint"}); !errors.Is(err, ErrNoSource) {
		t.Fatalf("prompt = %v, want ErrNoSource", err)
	}
	r := Resolver{Name: "API token", Env: "EXAMPLE_TOKEN", StdinBusy: true, Prompt: prompt}
	if _, _, err := r.Resolve(); !errors.Is(err, ErrNoSource) ||
		err.Error() != "no API token available: set $EXAMPLE_TOKEN, or run this on a terminal" {
		t.Errorf("Resolve = %v, want the sources to offer", err)
	}
	if written, _ := os.ReadFile(out.Name()); len(written) != 0 {
		t.Errorf("wrote %q off a terminal", written)
	}
}
