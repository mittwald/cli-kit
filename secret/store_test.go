package secret

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// privateDir returns a new directory only its owner can enter or write to,
// whatever the umask of the test run: a FileStore refuses a directory
// other users can write to.
func privateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
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
	dir := privateDir(t)
	target := filepath.Join(dir, "elsewhere")
	if err := os.WriteFile(target, []byte("planted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "alice")); err != nil {
		t.Fatal(err)
	}
	s := &FileStore{Dir: dir}
	if _, err := s.Get("alice"); !errors.Is(err, ErrInsecureFile) || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Get through symlink = %v, want ErrInsecureFile naming the link", err)
	}
	if err := s.Set("alice", "new"); !errors.Is(err, ErrInsecureFile) || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Set through symlink = %v, want ErrInsecureFile naming the link", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "planted" {
		t.Fatalf("symlink target was overwritten: %q", data)
	}
}

func TestFileStoreTrimsLineEndings(t *testing.T) {
	dir := privateDir(t)
	if err := os.WriteFile(filepath.Join(dir, "alice"), []byte("hunter2\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := (&FileStore{Dir: dir}).Get("alice"); err != nil || got != "hunter2" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	// An editor's byte order mark is no part of the secret.
	if err := os.WriteFile(filepath.Join(dir, "bob"), []byte("\xef\xbb\xbfhunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := (&FileStore{Dir: dir}).Get("bob"); err != nil || got != "hunter2" {
		t.Fatalf("Get of a file with a byte order mark = %q, %v", got, err)
	}
}

// Whoever can write to the directory can swap the files in it, so such a
// directory is refused, and left as it is.
func TestFileStoreRefusesAWritableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	for _, mode := range []fs.FileMode{0o770, 0o707, 0o777} {
		dir := privateDir(t)
		s := &FileStore{Dir: dir}
		if err := s.Set("alice", "hunter2"); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Get("alice"); !errors.Is(err, ErrInsecureFile) || !strings.Contains(err.Error(), "chmod go-w") {
			t.Errorf("mode %04o: Get = %v, want ErrInsecureFile saying how to fix it", mode, err)
		}
		if err := s.Set("alice", "changed"); !errors.Is(err, ErrInsecureFile) {
			t.Errorf("mode %04o: Set = %v, want ErrInsecureFile", mode, err)
		}
		if data, _ := os.ReadFile(filepath.Join(dir, "alice")); string(data) != "hunter2" {
			t.Errorf("mode %04o: the refused Set wrote %q", mode, data)
		}
		if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != mode {
			t.Errorf("mode %04o: directory mode is now %v, %v; it must not be changed", mode, info.Mode().Perm(), err)
		}
	}
	// Others may read the directory: that shows account names, no secrets.
	dir := privateDir(t)
	s := &FileStore{Dir: dir}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("alice", "hunter2"); err != nil {
		t.Fatalf("Set in a 0755 directory = %v", err)
	}
	if got, err := s.Get("alice"); err != nil || got != "hunter2" {
		t.Fatalf("Get in a 0755 directory = %q, %v", got, err)
	}
	// Without a directory there is no secret, and Get creates nothing.
	missing := filepath.Join(dir, "missing")
	if _, err := (&FileStore{Dir: missing}).Get("alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get without a directory = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Get created the directory: %v", err)
	}
}

// Set takes only what Get returns unchanged, and nothing is cut short on
// the way in or out.
func TestFileStoreKeepsOnlyWhatItCanReturn(t *testing.T) {
	dir := privateDir(t)
	s := &FileStore{Dir: dir}
	longest := strings.Repeat("k", maxFileSize)
	if err := s.Set("alice", longest); err != nil {
		t.Fatalf("Set of %d bytes = %v", len(longest), err)
	}
	if got, err := s.Get("alice"); err != nil || got != longest {
		t.Fatalf("Get = %d bytes, %v; want all %d", len(got), err, len(longest))
	}
	if err := s.Set("alice", "line1\nline2"); err != nil {
		t.Fatalf("Set of two lines = %v", err)
	}
	if got, err := s.Get("alice"); err != nil || got != "line1\nline2" {
		t.Fatalf("Get = %q, %v", got, err)
	}

	const marker = "s3cr3t-value"
	if err := s.Set("alice", longest+"k"); !errors.Is(err, ErrTooLong) {
		t.Errorf("Set of %d bytes = %v, want ErrTooLong", len(longest)+1, err)
	}
	for _, value := range []string{marker + "\n", marker + "\r\n", marker + "\r", "\n", "\xef\xbb\xbf" + marker} {
		err := s.Set("alice", value)
		if err == nil {
			t.Errorf("Set(%q) succeeded, though Get would return something else", value)
		} else if containsSecret(err, marker) {
			t.Errorf("Set(%q): error %q carries the value", value, err)
		}
	}
	if got, err := s.Get("alice"); err != nil || got != "line1\nline2" {
		t.Errorf("a refused Set changed the stored value: %q, %v", got, err)
	}

	// A file that grew beyond the limit by other means is an error, not a
	// secret cut short.
	if err := os.WriteFile(filepath.Join(dir, "big"), []byte(longest+"k\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get("big"); !errors.Is(err, ErrTooLong) || got != "" {
		t.Errorf("Get of an oversized file = %d bytes, %v; want ErrTooLong", len(got), err)
	}
}

func TestFileStoreRejectsUnsafeAccountNames(t *testing.T) {
	s := &FileStore{Dir: privateDir(t)}
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
	for _, bad := range []string{"", "../x", "/abs", ".", "./", "a/.."} {
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
	if _, err := s.Get("alice"); !errors.Is(err, ErrNotFound) || !errors.Is(err, errUnavailable) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound and the primary's failure", err)
	}
}

// stuck is a store that can be read but takes no change, like a keyring
// that answers from its cache while every write is denied.
type stuck struct{ Store }

var errDenied = errors.New("keyring denied the change")

func (stuck) Set(string, string) error { return errDenied }
func (stuck) Delete(string) error      { return errDenied }

// A primary that did not take the new secret must not go on answering with
// the old one: Get asks it first.
func TestFallbackSetDropsWhatPrimaryStillHolds(t *testing.T) {
	primary, secondary := NewMemory(map[string]string{"alice": "old"}), &Memory{}
	s := Fallback(readOnly{primary}, secondary)
	if err := s.Set("alice", "new"); err != nil {
		t.Fatalf("Set = %v", err)
	}
	if got, err := s.Get("alice"); err != nil || got != "new" {
		t.Fatalf("Get = %q, %v; want the secret that was just set", got, err)
	}
	if _, err := primary.Get("alice"); !errors.Is(err, ErrNotFound) {
		t.Errorf("primary still holds the old secret: %v", err)
	}
}

// When the old secret cannot be removed from primary either, the new one
// would never be read: Set says so, and leaves no unread copy behind.
func TestFallbackSetFailsWhenPrimaryKeepsTheOldSecret(t *testing.T) {
	primary, secondary := NewMemory(map[string]string{"alice": "0ld-s3cret"}), &Memory{}
	s := Fallback(stuck{primary}, secondary)
	err := s.Set("alice", "new-secret")
	if !errors.Is(err, errDenied) || errors.Is(err, ErrNotFound) {
		t.Fatalf("Set = %v, want primary's failure", err)
	}
	if containsSecret(err, "new-secret") || containsSecret(err, "0ld-s3cret") {
		t.Errorf("error %q carries a secret", err)
	}
	if _, err := secondary.Get("alice"); !errors.Is(err, ErrNotFound) {
		t.Errorf("secondary keeps a copy nothing reads: %v", err)
	}
	if got, err := s.Get("alice"); err != nil || got != "0ld-s3cret" {
		t.Errorf("Get = %q, %v", got, err)
	}
	// Nothing is hidden when primary holds the very same secret.
	if err := s.Set("alice", "0ld-s3cret"); err != nil {
		t.Errorf("Set of the secret primary holds = %v", err)
	}
}

// Once primary took the secret, an older copy does not stay behind in
// secondary, where it would be readable and come back when primary is away.
func TestFallbackSetRemovesTheSecondaryCopy(t *testing.T) {
	files := &FileStore{Dir: filepath.Join(t.TempDir(), "example-tool")}
	if err := Fallback(failing{}, files).Set("alice", "hunter2"); err != nil {
		t.Fatalf("Set without primary = %v", err)
	}
	keyring := &Memory{}
	for name, store := range map[string]Store{"Fallback": Fallback(keyring, files), "Migrating": Migrating(keyring, files)} {
		if err := files.Set("alice", "hunter2"); err != nil {
			t.Fatal(err)
		}
		if err := store.Set("alice", "hunter2"); err != nil {
			t.Fatalf("%s: Set = %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(files.Dir, "alice")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: the file is still there after primary took the secret: %v", name, err)
		}
		if got, err := store.Get("alice"); err != nil || got != "hunter2" {
			t.Errorf("%s: Get = %q, %v", name, got, err)
		}
	}
	// A secondary that cannot delete does not make the Set fail.
	if err := Fallback(keyring, Unavailable(nil)).Set("alice", "hunter2"); err != nil {
		t.Errorf("Set with a secondary that is not there = %v", err)
	}
}

// A secret primary still returns is not forgotten, whatever secondary did.
func TestFallbackDeleteFailsWhilePrimaryHoldsTheSecret(t *testing.T) {
	for name, secondary := range map[string]*Memory{
		"with a copy":    NewMemory(map[string]string{"alice": "copy"}),
		"without a copy": {},
	} {
		primary := NewMemory(map[string]string{"alice": "rejected"})
		store := Fallback(stuck{primary}, secondary)
		err := store.Delete("alice")
		if !errors.Is(err, errDenied) || errors.Is(err, ErrNotFound) {
			t.Errorf("%s: Delete = %v, want primary's failure and not ErrNotFound", name, err)
		}
		if _, err := secondary.Get("alice"); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: secondary keeps its copy: %v", name, err)
		}
		// The Resolver must not report the secret as forgotten: the next
		// Resolve would return it again.
		r := Resolver{Account: "alice", Store: store}
		if err := r.Forget(SourceStore); !errors.Is(err, errDenied) {
			t.Errorf("%s: Forget = %v, want primary's failure", name, err)
		}
	}
}

// Without a primary, deleting what is not stored says so, the way Get does.
func TestFallbackDeleteWithoutPrimaryAndNothingStored(t *testing.T) {
	s := Fallback(failing{}, &Memory{})
	err := s.Delete("alice")
	if !errors.Is(err, ErrNotFound) || !errors.Is(err, errUnavailable) {
		t.Fatalf("Delete = %v, want ErrNotFound and the primary's failure", err)
	}
	r := Resolver{Account: "alice", Store: s}
	if err := r.Forget(SourceStore); err != nil {
		t.Errorf("Forget = %v", err)
	}
	// A secondary that fails for a reason of its own is reported with it.
	other := errors.New("no home directory")
	err = Fallback(failing{}, Unavailable(other)).Delete("alice")
	if !errors.Is(err, other) || !errors.Is(err, errUnavailable) || errors.Is(err, ErrNotFound) {
		t.Errorf("Delete with both stores failing = %v, want both failures", err)
	}
	// A primary that cannot delete but knows it holds nothing counts as empty.
	if err := Fallback(stuck{&Memory{}}, &Memory{}).Delete("alice"); !errors.Is(err, ErrNotFound) || errors.Is(err, errDenied) {
		t.Errorf("Delete with an empty primary = %v, want ErrNotFound alone", err)
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
	for _, tc := range []struct {
		name    string
		text    string
		secrets []string
		want    string
	}{
		{"no secrets", "plain text", nil, "plain text"},
		{"nothing found", "plain text", []string{"hunter2"}, "plain text"},
		{"only empty secrets", "plain text", []string{"", ""}, "plain text"},
		{"overlapping secrets", "abcdef", []string{"abcd", "cdef"}, "[REDACTED]"},
		{"overlap with the longer one behind", "xabcdef!", []string{"bcdef", "abc"}, "x[REDACTED]!"},
		{"a secret overlapping itself", "-aaa-", []string{"aa"}, "-[REDACTED]-"},
		{"neighbors stay apart", "abcabc", []string{"abc"}, "[REDACTED][REDACTED]"},
		{"the marker is never matched", "token=longsecret", []string{"longsecret", "RED", "[", "E"}, "token=[REDACTED]"},
		{"regular expression characters", "a.*b and axxb", []string{".*"}, "a[REDACTED]b and axxb"},
		{"at both ends", "pw and pw", []string{"pw"}, "[REDACTED] and [REDACTED]"},
	} {
		if got := Redact(tc.text, tc.secrets...); got != tc.want {
			t.Errorf("%s: Redact(%q, %q) = %q, want %q", tc.name, tc.text, tc.secrets, got, tc.want)
		}
	}
}

// A secret is also found in the forms it takes inside a request.
func TestRedactEscapedForms(t *testing.T) {
	const tricky = `p@ss w/rd"<1>&ü`
	body, err := json.Marshal(map[string]string{"password": tricky})
	if err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	enc := json.NewEncoder(&plain)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]string{"password": tricky}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, text, want string }{
		{"as it is", "password: " + tricky + ".", "password: [REDACTED]."},
		{"form body", "user=alice&password=" + url.QueryEscape(tricky) + "&x=1", "user=alice&password=[REDACTED]&x=1"},
		{"query with %20", "?password=" + strings.ReplaceAll(url.QueryEscape(tricky), "+", "%20") + "&x=1", "?password=[REDACTED]&x=1"},
		{"URL path", "GET /keys/" + url.PathEscape(tricky) + "/info", "GET /keys/[REDACTED]/info"},
		{"JSON", string(body), `{"password":"[REDACTED]"}`},
		{"JSON without HTML escapes", strings.TrimSpace(plain.String()), `{"password":"[REDACTED]"}`},
	} {
		got := Redact(tc.text, tricky)
		if got != tc.want {
			t.Errorf("%s: Redact(%q) = %q, want %q", tc.name, tc.text, got, tc.want)
		}
	}
	// A quote at the end of a secret stays part of its JSON form.
	if got := Redact(`{"password":"say \"hi\""}`, `say "hi"`); got != `{"password":"[REDACTED]"}` {
		t.Errorf("Redact of a quoted secret in JSON = %q", got)
	}

	// What Redact cannot find, as documented: a secret inside a larger
	// encoded value.
	basic := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("alice:"+tricky))
	if got := Redact(basic, tricky); got != basic {
		t.Errorf("Redact found the password inside Basic credentials: %q; update its documentation", got)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte("alice:" + tricky))
	if got := Redact(basic, tricky, encoded); got != "Authorization: Basic [REDACTED]" {
		t.Errorf("Redact with the encoded credentials as a secret = %q", got)
	}
}

func TestWriteFileReplacesPrivately(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token-cache.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("new")); err != nil {
		t.Fatalf("WriteFile = %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "new" {
		t.Fatalf("contents = %q, %v", data, err)
	}
	if info, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("mode = %v, %v; want 0600 whatever the old file had", info.Mode(), err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("directory holds %d entries, want only the file", len(entries))
	}
	if err := WriteFile(filepath.Join(dir, "missing", "file"), []byte("x")); err == nil {
		t.Fatal("WriteFile into a missing directory succeeded")
	}
}

func TestWriteFileRefusesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere")
	link := filepath.Join(dir, "token-cache.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(link, []byte("x")); !errors.Is(err, ErrInsecureFile) {
		t.Fatalf("WriteFile through a symlink = %v, want ErrInsecureFile", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("WriteFile wrote through the symlink")
	}
}

// The temporary file does not grow with the destination's name, so a name
// near the usual 255-byte limit still works.
func TestWriteFileLongName(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, strings.Repeat("a", 250))
	if err := WriteFile(path, []byte("x")); err != nil {
		t.Fatalf("WriteFile with a 250-byte name = %v", err)
	}
	store := &FileStore{Dir: dir}
	if err := store.Set(strings.Repeat("b", 240), "x"); err != nil {
		t.Fatalf("Set with a 240-byte account = %v", err)
	}
}
