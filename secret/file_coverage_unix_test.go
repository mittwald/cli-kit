//go:build unix

package secret

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A file that cannot be opened is reported as such, not as missing or as
// a symbolic link.
func TestFileStoreReportsAFileThatDoesNotOpen(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root opens any file")
	}
	s := &FileStore{Dir: privateDir(t)}
	if err := os.WriteFile(filepath.Join(s.Dir, "alice"), []byte("hunter2"), 0o000); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get("alice")
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrInsecureFile) || !strings.Contains(err.Error(), "secret: read") {
		t.Fatalf("Get = %v, want the failed open reported", err)
	}
}
