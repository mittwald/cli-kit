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

// The deprecated forwards do what the redact package does.
func TestDeprecatedRedactForwards(t *testing.T) {
	if got := Redact("token abc123", "abc123"); got != "token "+Redacted {
		t.Errorf("Redact = %q", got)
	}
	if got := RedactPatterns("password=hunter2"); got != "password="+Redacted {
		t.Errorf("RedactPatterns = %q", got)
	}
}

func TestNewFileStoreWithoutAConfigurationDirectory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only Linux derives the configuration directory from the environment alone")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if _, err := NewFileStore("example-tool"); err == nil || !strings.Contains(err.Error(), "locate configuration directory") {
		t.Fatalf("NewFileStore = %v, want the missing directory reported", err)
	}
}

func TestFileStoreWithoutDir(t *testing.T) {
	s := &FileStore{}
	if _, err := s.Get("alice"); err == nil || !strings.Contains(err.Error(), "Dir is empty") {
		t.Errorf("Get = %v", err)
	}
	if err := s.Set("alice", "hunter2"); err == nil || !strings.Contains(err.Error(), "Dir is empty") {
		t.Errorf("Set = %v", err)
	}
	if err := s.Delete("alice"); err == nil || !strings.Contains(err.Error(), "Dir is empty") {
		t.Errorf("Delete = %v", err)
	}
}

func TestFileStoreDirThatIsAFile(t *testing.T) {
	file := filepath.Join(privateDir(t), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &FileStore{Dir: file}
	if _, err := s.Get("alice"); err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Errorf("Get = %v, want the file refused as directory", err)
	}
	// Set cannot create a directory below a file.
	s = &FileStore{Dir: filepath.Join(file, "below")}
	if err := s.Set("alice", "hunter2"); err == nil || !strings.Contains(err.Error(), "create") {
		t.Errorf("Set = %v, want the directory creation reported", err)
	}
}

func TestFileStoreEmptyFileIsNotFound(t *testing.T) {
	s := &FileStore{Dir: privateDir(t)}
	if err := os.WriteFile(filepath.Join(s.Dir, "alice"), []byte("\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get of a file holding only a line ending = %v, want ErrNotFound", err)
	}
}

func TestFileStoreDeleteFailure(t *testing.T) {
	s := &FileStore{Dir: privateDir(t)}
	// A directory that is not empty cannot be removed like a file.
	if err := os.MkdirAll(filepath.Join(s.Dir, "alice", "inner"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("alice"); err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "delete") {
		t.Fatalf("Delete = %v, want the failure reported", err)
	}
}

// fakeInfo describes a file of the given mode.
type fakeInfo struct{ mode fs.FileMode }

func (f fakeInfo) Name() string       { return "alice" }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return nil }

// The open refuses a symbolic link already; the check on what was opened
// refuses one too.
func TestCheckSecretFile(t *testing.T) {
	for _, tc := range []struct {
		mode fs.FileMode
		want string
	}{
		{fs.ModeSymlink | 0o600, "symbolic link"},
		{fs.ModeDir | 0o700, "not a regular file"},
		{fs.ModeNamedPipe | 0o600, "not a regular file"},
	} {
		err := checkSecretFile("alice", fakeInfo{tc.mode})
		if !errors.Is(err, ErrInsecureFile) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("mode %v: checkSecretFile = %v, want %q", tc.mode, err, tc.want)
		}
	}
	if err := checkSecretFile("alice", fakeInfo{0o600}); err != nil {
		t.Errorf("checkSecretFile of a private file = %v", err)
	}
}

func TestRequestPrompt(t *testing.T) {
	for _, tc := range []struct {
		req  Request
		want string
	}{
		{Request{Label: "Password"}, "Password: "},
		{Request{Label: "Password", Name: "password", Repeat: true}, "Repeat password: "},
		{Request{Label: "Password", Repeat: true}, "Repeat Password: "},
	} {
		if got := tc.req.Prompt(); got != tc.want {
			t.Errorf("%+v: Prompt = %q, want %q", tc.req, got, tc.want)
		}
	}
}

// A Store without an Account is refused before anything is read or written.
func TestResolverRequiresAccountWithAStore(t *testing.T) {
	r := &Resolver{Store: &Memory{}}
	if _, _, err := r.Lookup(); err == nil || !strings.Contains(err.Error(), "Account is required") {
		t.Errorf("Lookup = %v", err)
	}
	if err := r.Save("hunter2"); err == nil || !strings.Contains(err.Error(), "Account is required") {
		t.Errorf("Save = %v", err)
	}
}

func TestLookup(t *testing.T) {
	store := &Memory{}
	if err := store.Set("default", "  stored  "); err != nil {
		t.Fatal(err)
	}
	r := &Resolver{File: writeFile(t, "from-file\n"), Store: store, Account: "default", TrimSpace: true}
	if value, source, err := r.Lookup(); err != nil || value != "from-file" || source != SourceFile {
		t.Errorf("Lookup with File = %q, %v, %v", value, source, err)
	}
	r.File = ""
	if value, source, err := r.Lookup(); err != nil || value != "stored" || source != SourceStore {
		t.Errorf("Lookup from the store = %q, %v, %v; want the stored value trimmed", value, source, err)
	}
	r.Store = failing{}
	if _, _, err := r.Lookup(); !errors.Is(err, errUnavailable) {
		t.Errorf("Lookup with a failing store = %v, want its error", err)
	}
}

func TestForgetReportsAStoreThatCannotDelete(t *testing.T) {
	r := &Resolver{Store: failing{}, Account: "default"}
	if err := r.Forget(SourceStore); !errors.Is(err, errUnavailable) {
		t.Errorf("Forget = %v, want the store's error", err)
	}
	r.Store = nil
	if err := r.Forget(SourceStore); err != nil {
		t.Errorf("Forget without a store = %v", err)
	}
}

func TestResolverStdinDefaultsToOsStdin(t *testing.T) {
	if got := (&Resolver{}).stdin(); got != os.Stdin {
		t.Errorf("stdin = %v, want os.Stdin", got)
	}
}

func TestResolveDashWhileStdinIsBusy(t *testing.T) {
	r := &Resolver{File: "-", StdinBusy: true, FileFlag: "--token-file", Prompt: noPrompt(t)}
	_, _, err := r.Resolve()
	if err == nil || !strings.Contains(err.Error(), "--token-file - reads the secret from stdin") {
		t.Fatalf("Resolve = %v", err)
	}
	r.FileFlag = ""
	if _, _, err := r.Resolve(); err == nil || !strings.Contains(err.Error(), `the file "-" reads`) {
		t.Fatalf("Resolve without FileFlag = %v", err)
	}
}

func TestResolveReportsAFailedRead(t *testing.T) {
	stdin := stdinFile(t, "hunter2\n")
	_ = stdin.Close()
	r := &Resolver{File: "-", Stdin: stdin, Prompt: noPrompt(t)}
	_, _, err := r.Resolve()
	if err == nil || !strings.Contains(err.Error(), "reading secret from stdin") || !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Resolve = %v, want the read error", err)
	}
}

func TestWithoutPath(t *testing.T) {
	plain := errors.New("plain")
	if got := withoutPath(plain); !errors.Is(got, plain) {
		t.Errorf("withoutPath of an error without a path = %v", got)
	}
	pathErr := &fs.PathError{Op: "open", Path: "/secret/hunter2", Err: fs.ErrPermission}
	if got := withoutPath(pathErr); !errors.Is(got, fs.ErrPermission) || strings.Contains(got.Error(), "hunter2") {
		t.Errorf("withoutPath = %v", got)
	}
}

func TestAskTrimsAndConfirms(t *testing.T) {
	var asked []Request
	r := &Resolver{StdinBusy: true, TrimSpace: true, Confirm: true, Prompt: answers(&asked, " hunter2 ", "hunter2\t")}
	value, source, err := r.Resolve()
	if err != nil || value != "hunter2" || source != SourcePrompt {
		t.Fatalf("Resolve = %q, %v, %v", value, source, err)
	}
	// The repetition fails.
	asked = nil
	r.Prompt = answers(&asked, "hunter2")
	if _, _, err := r.Resolve(); err == nil || !strings.Contains(err.Error(), "unexpected prompt") {
		t.Fatalf("Resolve with a failing repetition = %v", err)
	}
	if len(asked) != 2 || !asked[1].Repeat {
		t.Fatalf("asked %+v", asked)
	}
}

func TestPromptFailedWithOnlyATerminalLeft(t *testing.T) {
	r := &Resolver{StdinBusy: true, Prompt: func(Request) (string, error) { return "", ErrNoSource }}
	_, _, err := r.Resolve()
	if !errors.Is(err, ErrNoSource) || !strings.HasSuffix(err.Error(), "no secret available: run this on a terminal") {
		t.Fatalf("Resolve = %v", err)
	}
}

func TestTTYPromptWithoutATerminal(t *testing.T) {
	t.Cleanup(func() { openTTY = defaultOpenTTY })
	errNoTTY := errors.New("no controlling terminal")
	openTTY = func() (*os.File, error) { return nil, errNoTTY }
	if _, err := TTYPrompt(Request{Label: "Password"}); !errors.Is(err, ErrNoSource) || !errors.Is(err, errNoTTY) {
		t.Errorf("TTYPrompt = %v, want ErrNoSource and the cause", err)
	}
	// A file that opens but is no terminal.
	openTTY = func() (*os.File, error) { return os.Open(writeFile(t, "x")) }
	if _, err := TTYPrompt(Request{Label: "Password"}); !errors.Is(err, ErrNoSource) {
		t.Errorf("TTYPrompt on a regular file = %v, want ErrNoSource", err)
	}
}

func TestFallbackSetFailsWhenBothFail(t *testing.T) {
	err := Fallback(failing{}, stuck{&Memory{}}).Set("alice", "hunter2")
	if !errors.Is(err, errUnavailable) || !errors.Is(err, errDenied) {
		t.Fatalf("Set = %v, want both errors", err)
	}
}

func TestFallbackDeleteReportsBothWhilePrimaryHoldsTheSecret(t *testing.T) {
	primary := &Memory{}
	if err := primary.Set("alice", "hunter2"); err != nil {
		t.Fatal(err)
	}
	err := Fallback(stuck{primary}, failing{}).Delete("alice")
	if !errors.Is(err, errDenied) || !errors.Is(err, errUnavailable) || !strings.Contains(err.Error(), "still holds") {
		t.Fatalf("Delete = %v, want both errors", err)
	}
}

func TestFallbackDeleteReportsTheSecondaryFailure(t *testing.T) {
	// Primary held nothing; secondary failed, so the secret may still be
	// there.
	if err := Fallback(&Memory{}, failing{}).Delete("alice"); !errors.Is(err, errUnavailable) {
		t.Fatalf("Delete = %v, want the secondary's error", err)
	}
	primary := &Memory{}
	if err := primary.Set("alice", "hunter2"); err != nil {
		t.Fatal(err)
	}
	if err := Fallback(primary, failing{}).Delete("alice"); err != nil {
		t.Fatalf("Delete = %v", err)
	}
}
