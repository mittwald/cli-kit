//go:build unix

package secret

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// A file that is swapped for a symbolic link while Get looks at it is never
// read through the link: the checks are made on the file Get opened.
func TestFileStoreNeverFollowsASwappedSymlink(t *testing.T) {
	dir := privateDir(t)
	target := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(target, []byte("another file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "alice")
	if err := os.WriteFile(path, []byte("real\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stop atomic.Bool
	swapped := make(chan struct{})
	go func() {
		defer close(swapped)
		link, file := filepath.Join(dir, ".link"), filepath.Join(dir, ".file")
		for !stop.Load() {
			_ = os.Symlink(target, link)
			_ = os.Rename(link, path)
			_ = os.WriteFile(file, []byte("real\n"), 0o600)
			_ = os.Rename(file, path)
		}
	}()
	s := &FileStore{Dir: dir}
	read, refused := 0, 0
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		got, err := s.Get("alice")
		switch {
		case err == nil && got == "real":
			read++
		case errors.Is(err, ErrInsecureFile):
			refused++
		default:
			stop.Store(true)
			<-swapped
			t.Fatalf("Get = %q, %v; want the file's secret or a refusal of the link", got, err)
		}
	}
	stop.Store(true)
	<-swapped
	t.Logf("%d reads, %d refusals", read, refused)
}

// A file that disappears while Get looks at it is a secret that is not
// there, not a failure.
func TestFileStoreGetDuringDelete(t *testing.T) {
	dir := privateDir(t)
	path := filepath.Join(dir, "alice")
	var stop atomic.Bool
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for !stop.Load() {
			_ = WriteFile(path, []byte("real"))
			_ = os.Remove(path)
		}
	}()
	s := &FileStore{Dir: dir}
	for deadline := time.Now().Add(500 * time.Millisecond); time.Now().Before(deadline); {
		if got, err := s.Get("alice"); (err != nil || got != "real") && !errors.Is(err, ErrNotFound) {
			stop.Store(true)
			<-finished
			t.Fatalf("Get = %q, %v; want the secret or ErrNotFound", got, err)
		}
	}
	stop.Store(true)
	<-finished
}

// Something that is no regular file is refused, and a named pipe does not
// make Get wait for a writer.
func TestFileStoreRefusesANamedPipe(t *testing.T) {
	dir := privateDir(t)
	if err := unix.Mkfifo(filepath.Join(dir, "alice"), 0o600); err != nil {
		t.Skipf("no named pipe: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "bob"), 0o700); err != nil {
		t.Fatal(err)
	}
	s := &FileStore{Dir: dir}
	for _, account := range []string{"alice", "bob"} {
		done := make(chan error, 1)
		go func() { _, err := s.Get(account); done <- err }()
		select {
		case err := <-done:
			if !errors.Is(err, ErrInsecureFile) {
				t.Errorf("Get(%q) = %v, want ErrInsecureFile", account, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("Get(%q) waits for a writer", account)
		}
	}
}

// A file of another user is not trusted, even with the right mode; only
// root can open one, so the check is exercised on the file's description.
func TestSecretFileOfAnotherUserIsRefused(t *testing.T) {
	path := filepath.Join(privateDir(t), "alice")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkSecretFile(path, info); err != nil {
		t.Fatalf("a file of our own = %v", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("no owner in the file's description")
	}
	stat.Uid++
	if err := checkSecretFile(path, info); !errors.Is(err, ErrInsecureFile) {
		t.Fatalf("a file of another user = %v, want ErrInsecureFile", err)
	}
}
