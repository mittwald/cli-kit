package secret

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	gokeyring "github.com/zalando/go-keyring"
)

// useMockKeyring swaps go-keyring for an in-memory provider. The real
// provider is never restored: no test may touch the user's keyring.
func useMockKeyring(t *testing.T) {
	t.Helper()
	gokeyring.MockInit()
	t.Cleanup(gokeyring.MockInit)
}

// exerciseStore runs the contract every Store implements.
func exerciseStore(t *testing.T, s Store) {
	t.Helper()
	if _, err := s.Get("alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get on empty store = %v, want ErrNotFound", err)
	}
	if err := s.Set("alice", ""); !errors.Is(err, ErrEmpty) {
		t.Fatalf("Set empty = %v, want ErrEmpty", err)
	}
	if err := s.Set("alice", "first"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Set("alice", "second"); err != nil {
		t.Fatalf("Set replace: %v", err)
	}
	if got, err := s.Get("alice"); err != nil || got != "second" {
		t.Fatalf("Get = %q, %v; want second", got, err)
	}
	if err := s.Delete("alice"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get("alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := s.Delete("alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound", err)
	}
}

func TestMemoryStore(t *testing.T) {
	exerciseStore(t, &Memory{})
	m := NewMemory(map[string]string{"bob": "pw"})
	if got, err := m.Get("bob"); err != nil || got != "pw" {
		t.Fatalf("NewMemory Get = %q, %v", got, err)
	}
}

func TestKeyringStore(t *testing.T) {
	useMockKeyring(t)
	exerciseStore(t, Keyring{Service: "example-tool"})
}

func TestKeyringRequiresService(t *testing.T) {
	useMockKeyring(t)
	if _, err := (Keyring{}).Get("a"); !errors.Is(err, ErrNoService) {
		t.Fatalf("Get = %v, want ErrNoService", err)
	}
	if err := (Keyring{}).Set("a", "b"); !errors.Is(err, ErrNoService) {
		t.Fatalf("Set = %v, want ErrNoService", err)
	}
	if err := (Keyring{}).Delete("a"); !errors.Is(err, ErrNoService) {
		t.Fatalf("Delete = %v, want ErrNoService", err)
	}
}

func TestKeyringServicesStaySeparate(t *testing.T) {
	useMockKeyring(t)
	passwords := Keyring{Service: "example-tool"}
	users := Keyring{Service: "example-tool-user"}
	if err := passwords.Set("default", "secret"); err != nil {
		t.Fatal(err)
	}
	if err := users.Set("default", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := users.Delete("default"); err != nil {
		t.Fatal(err)
	}
	if got, err := passwords.Get("default"); err != nil || got != "secret" {
		t.Fatalf("password = %q, %v", got, err)
	}
}

func TestKeyringTreatsEmptyValueAsNotFound(t *testing.T) {
	useMockKeyring(t)
	// Only another tool can store an empty value.
	if err := gokeyring.Set("example-tool", "alice", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := (Keyring{Service: "example-tool"}).Get("alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get = %v, want ErrNotFound", err)
	}
}

func TestKeyringErrorsDoNotCarryTheSecret(t *testing.T) {
	gokeyring.MockInitWithError(errors.New("no secret service"))
	t.Cleanup(gokeyring.MockInit)
	err := Keyring{Service: "example-tool"}.Set("alice", "hunter2")
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("Set = %v, want an unavailable-keyring error", err)
	}
	if containsSecret(err, "hunter2") {
		t.Fatalf("error %q carries the secret", err)
	}
}

func containsSecret(err error, secret string) bool {
	return err != nil && Redact(err.Error(), secret) != err.Error()
}

func TestFileStore(t *testing.T) {
	exerciseStore(t, &FileStore{Dir: filepath.Join(t.TempDir(), "example-tool")})
}

func TestFileStorePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	dir := filepath.Join(t.TempDir(), "example-tool")
	s := &FileStore{Dir: dir}
	if err := s.Set("alice", "hunter2"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v, %v; want 0700", info.Mode().Perm(), err)
	}
	info, err := os.Stat(filepath.Join(dir, "alice"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("dir holds %v, %v; want only the secret file (no temporary leftovers)", entries, err)
	}

	if err := os.Chmod(filepath.Join(dir, "alice"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("alice"); !errors.Is(err, ErrInsecureFile) {
		t.Fatalf("Get world-readable = %v, want ErrInsecureFile", err)
	}
}

func TestFileStoreRefusesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere")
	if err := os.WriteFile(target, []byte("planted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "alice")); err != nil {
		t.Fatal(err)
	}
	s := &FileStore{Dir: dir}
	if _, err := s.Get("alice"); !errors.Is(err, ErrInsecureFile) {
		t.Fatalf("Get through symlink = %v, want ErrInsecureFile", err)
	}
	if err := s.Set("alice", "new"); !errors.Is(err, ErrInsecureFile) {
		t.Fatalf("Set through symlink = %v, want ErrInsecureFile", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "planted" {
		t.Fatalf("symlink target was overwritten: %q", data)
	}
}

func TestFileStoreTrimsLineEndings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "alice"), []byte("hunter2\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := (&FileStore{Dir: dir}).Get("alice"); err != nil || got != "hunter2" {
		t.Fatalf("Get = %q, %v", got, err)
	}
}

func TestFileStoreRejectsUnsafeAccountNames(t *testing.T) {
	s := &FileStore{Dir: t.TempDir()}
	for _, account := range []string{"", ".", "..", "../x", "a/b", `a\b`, ".hidden", "a\x00b"} {
		if err := s.Set(account, "v"); err == nil {
			t.Errorf("Set(%q) succeeded, want an error", account)
		}
	}
}

func TestNewFileStore(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	s, err := NewFileStore("example-tool")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(s.Dir) != "example-tool" {
		t.Fatalf("Dir = %q", s.Dir)
	}
	for _, bad := range []string{"", "../x", "/abs"} {
		if _, err := NewFileStore(bad); err == nil {
			t.Errorf("NewFileStore(%q) succeeded", bad)
		}
	}
}

// failing is a Store whose every call fails, like a keyring without a
// Secret Service.
type failing struct{}

var errUnavailable = errors.New("keyring unavailable")

func (failing) Get(string) (string, error) { return "", errUnavailable }
func (failing) Set(string, string) error   { return errUnavailable }
func (failing) Delete(string) error        { return errUnavailable }

func TestFallbackUsesSecondaryWhenPrimaryFails(t *testing.T) {
	secondary := &Memory{}
	s := Fallback(failing{}, secondary)
	if err := s.Set("alice", "pw"); err != nil {
		t.Fatalf("Set = %v", err)
	}
	if got, _ := secondary.Get("alice"); got != "pw" {
		t.Fatalf("secondary holds %q", got)
	}
	if got, err := s.Get("alice"); err != nil || got != "pw" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if err := s.Delete("alice"); err != nil {
		t.Fatalf("Delete = %v", err)
	}
	if _, err := s.Get("alice"); !errors.Is(err, errUnavailable) {
		t.Fatalf("Get after Delete = %v, want the primary's failure", err)
	}
}

func TestFallbackPrefersPrimary(t *testing.T) {
	primary, secondary := &Memory{}, &Memory{}
	s := Fallback(primary, secondary)
	exerciseStore(t, s)
	if err := s.Set("alice", "pw"); err != nil {
		t.Fatal(err)
	}
	if _, err := secondary.Get("alice"); !errors.Is(err, ErrNotFound) {
		t.Fatal("Set wrote the secondary although the primary worked")
	}
	// A secret left in the secondary from a run without keyring is still
	// found, and deleting removes it from both.
	if err := secondary.Set("bob", "old"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get("bob"); err != nil || got != "old" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if err := s.Delete("bob"); err != nil {
		t.Fatalf("Delete = %v", err)
	}
	if _, err := secondary.Get("bob"); !errors.Is(err, ErrNotFound) {
		t.Fatal("Delete left the secondary copy")
	}
}

func TestRedact(t *testing.T) {
	got := Redact("token=abc123 again abc123 and abc", "abc", "abc123", "")
	want := "token=[REDACTED] again [REDACTED] and [REDACTED]"
	if got != want {
		t.Fatalf("Redact = %q, want %q", got, want)
	}
}
