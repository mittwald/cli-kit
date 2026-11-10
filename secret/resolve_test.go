package secret

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	t.Cleanup(func() { f.Close() })
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
	r := Resolver{Label: "Password", Stdin: stdinFile(t, ""), Confirm: true, Prompt: answers(&asked, "a", "a")}
	if got, _, err := r.Resolve(); err != nil || got != "a" {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
	if len(asked) != 2 || !asked[1].Repeat || asked[1].Prompt() != "Repeat Password: " {
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

	r := Resolver{Label: "token", Env: "EXAMPLE_TOKEN", FileFlag: "--token-file", Stdin: stdinFile(t, "")}
	_, _, err := r.Resolve()
	if !errors.Is(err, ErrNoSource) {
		t.Fatalf("Resolve = %v, want ErrNoSource", err)
	}
	for _, want := range []string{"$EXAMPLE_TOKEN", "--token-file", "stdin", "terminal"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
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
	r := Resolver{Label: "token", Account: "default", Store: store, Env: "EXAMPLE_TOKEN", File: "/run/secrets/token"}
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
	for source, want := range map[Source]string{SourceEnv: "$EXAMPLE_TOKEN", SourceFile: "/run/secrets/token", SourceStdin: "stdin"} {
		err := r.Forget(source)
		if !errors.Is(err, ErrFixedSource) || !strings.Contains(err.Error(), want) {
			t.Errorf("Forget(%v) = %v, want ErrFixedSource naming %q", source, err, want)
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
