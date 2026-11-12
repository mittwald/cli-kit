package secret

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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

func TestResolveMaxLen(t *testing.T) {
	r := Resolver{Stdin: stdinFile(t, strings.Repeat("x", 100)), MaxLen: 10}
	if got, _, err := r.Resolve(); err != nil || got != strings.Repeat("x", 10) {
		t.Fatalf("Resolve = %q, %v", got, err)
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
	_, openErr := os.Open(missing) // its wording differs between systems
	empty := writeFile(t, "\n")
	for _, tc := range []struct {
		name string
		r    Resolver
		want string
		is   error
	}{
		{"missing file", Resolver{Name: "password", File: missing},
			"reading password file: " + openErr.Error(), fs.ErrNotExist},
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
