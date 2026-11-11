package secret

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrInsecureFile reports a secret file that is a symbolic link, not a
// regular file, or readable by other users. A FileStore refuses to read
// such a file rather than trust a secret someone else could have planted
// or read.
var ErrInsecureFile = errors.New("secret: insecure secret file")

// maxFileSize bounds what a FileStore reads; a secret is a line, not a
// document.
const maxFileSize = 64 << 10

// FileStore is a Store that keeps each secret in a file of its own, named
// after the account, in a directory only the owner can enter (mode 0700).
// Each file is readable by its owner only (mode 0600) and is replaced
// atomically, so an interrupted write never leaves half a secret behind.
//
// The files are not encrypted: anyone who can act as the user can read
// them. Prefer [Keyring], and use a FileStore as the [Fallback] where no
// keyring exists, such as on servers and in containers.
type FileStore struct {
	// Dir is the directory holding the secret files.
	Dir string
}

// NewFileStore returns a FileStore in the directory named app under the
// user's configuration directory (os.UserConfigDir), such as
// ~/.config/example-tool on Linux.
func NewFileStore(app string) (*FileStore, error) {
	if app == "" || !filepath.IsLocal(app) {
		return nil, fmt.Errorf("secret: invalid application directory name %q", app)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("secret: locate configuration directory: %w", err)
	}
	return &FileStore{Dir: filepath.Join(base, app)}, nil
}

// path returns the file holding account, refusing names that would leave
// Dir or hide among the store's temporary files.
func (s *FileStore) path(account string) (string, error) {
	if s.Dir == "" {
		return "", errors.New("secret: FileStore.Dir is empty")
	}
	if account == "" || strings.ContainsAny(account, `/\`+"\x00") ||
		strings.HasPrefix(account, ".") || !filepath.IsLocal(account) {
		return "", fmt.Errorf("secret: invalid account name %q", account)
	}
	return filepath.Join(s.Dir, account), nil
}

// Get implements Store. Trailing line endings are dropped, so a file edited
// by hand still works.
func (s *FileStore) Get(account string) (string, error) {
	path, err := s.path(account)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("secret: read %s: %w", path, err)
	}
	if err := checkSecretFile(path, info); err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("secret: read %s: %w", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileSize))
	if err != nil {
		return "", fmt.Errorf("secret: read %s: %w", path, err)
	}
	value := strings.TrimRight(string(data), "\r\n")
	if value == "" {
		return "", ErrNotFound
	}
	return value, nil
}

// checkSecretFile refuses symbolic links, non-regular files, and, outside
// Windows, files that grant any access to group or others.
func checkSecretFile(path string, info fs.FileInfo) error {
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("%w: %s is a symbolic link", ErrInsecureFile, path)
	case !info.Mode().IsRegular():
		return fmt.Errorf("%w: %s is not a regular file", ErrInsecureFile, path)
	case runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0:
		return fmt.Errorf("%w: %s is accessible by other users (mode %04o); run chmod 600 on it",
			ErrInsecureFile, path, info.Mode().Perm())
	}
	return nil
}

// Set implements Store. It creates Dir with mode 0700 when missing.
func (s *FileStore) Set(account, value string) error {
	if value == "" {
		return ErrEmpty
	}
	path, err := s.path(account)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("secret: create %s: %w", s.Dir, err)
	}
	return WriteFile(path, []byte(value))
}

// WriteFile writes data to path for its owner's eyes only: the file gets
// mode 0600 (outside Windows) before any data reaches it, and replaces an
// existing file atomically, so a reader sees the old contents or the new
// ones, never part of either, and the new contents are never readable by
// others, whatever mode the old file had. Use it for anything that holds a
// credential, such as a token cache.
//
// The directory must exist. A symbolic link at path is refused with an
// error wrapping [ErrInsecureFile], so a link planted there cannot redirect
// the write.
func WriteFile(path string, data []byte) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s is a symbolic link", ErrInsecureFile, path)
	}
	// CreateTemp creates the file with mode 0600, and a rename within one
	// directory replaces the old file atomically. The temporary name has a
	// fixed length, so a destination name close to the file system's limit
	// still fits.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".secret-*")
	if err != nil {
		return fmt.Errorf("secret: write %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // fails harmlessly once renamed
	if err := tmp.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		return fmt.Errorf("secret: write %s: %w", path, errors.Join(err, tmp.Close()))
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("secret: write %s: %w", path, errors.Join(err, tmp.Close()))
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("secret: write %s: %w", path, errors.Join(err, tmp.Close()))
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("secret: write %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("secret: write %s: %w", path, err)
	}
	return nil
}

// Delete implements Store.
func (s *FileStore) Delete(account string) error {
	path, err := s.path(account)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("secret: delete %s: %w", path, err)
	}
	return nil
}
