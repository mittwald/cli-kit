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
// regular file, readable by other users or owned by another user, or a
// [FileStore] directory other users can write to. A FileStore refuses to
// read such a file rather than trust a secret someone else could have
// planted or read.
var ErrInsecureFile = errors.New("secret: insecure secret file")

// maxFileSize bounds what a FileStore keeps; a secret is a line, not a
// document.
const maxFileSize = 64 << 10

// utf8BOM is the byte order mark some editors put in front of a UTF-8 file.
const utf8BOM = "\xef\xbb\xbf"

// FileStore is a Store that keeps each secret in a file of its own, named
// after the account, in the directory Dir. Each file is readable by its
// owner only (mode 0600) and is replaced atomically, so an interrupted
// write never leaves half a secret behind.
//
// Set creates Dir for its owner only (mode 0700) when it is missing. An
// existing directory is used as it is, and its mode is never changed; it
// may be readable by others, who then see the account names, but it must
// not be writable by them: outside Windows, Get and Set refuse a directory
// that group or others can write to with an error wrapping
// [ErrInsecureFile], because whoever can write there can swap the files.
//
// Get refuses, with an error wrapping [ErrInsecureFile], a file that is a
// symbolic link, not a regular file, accessible by group or others, or
// owned by another user. Permissions are not checked on Windows, and the
// owner only on Unix. There the checks are made on the opened file, which
// is opened without following a link, so a file swapped in afterwards is
// not read instead. The owner counts for root as well: a tool run with sudo
// does not read the files of the user it was started by.
//
// A secret can be up to 64 KiB long. Set refuses, besides a longer one, a
// value that would not be read back unchanged: one that ends in a line
// break or starts with a UTF-8 byte order mark, both of which Get drops
// from a file edited by hand.
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
// ~/.config/example-tool on Linux. It refuses an app that names no
// directory below it, such as "." or "..".
func NewFileStore(app string) (*FileStore, error) {
	if app == "" || !filepath.IsLocal(app) || filepath.Clean(app) == "." {
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

// checkDir refuses a Dir that other users can write to: they could replace
// the files in it. A missing Dir is reported with an error wrapping
// fs.ErrNotExist.
func (s *FileStore) checkDir() error {
	info, err := os.Stat(s.Dir)
	if err != nil {
		return fmt.Errorf("secret: read %s: %w", s.Dir, err)
	}
	switch {
	case !info.IsDir():
		return fmt.Errorf("secret: %s is not a directory", s.Dir)
	case runtime.GOOS != "windows" && info.Mode().Perm()&0o022 != 0:
		return fmt.Errorf("%w: the directory %s is writable by other users (mode %04o); run chmod go-w on it",
			ErrInsecureFile, s.Dir, info.Mode().Perm())
	}
	return nil
}

// Get implements Store. A trailing line ending and a leading UTF-8 byte
// order mark are dropped, so a file edited by hand still works.
func (s *FileStore) Get(account string) (string, error) {
	path, err := s.path(account)
	if err != nil {
		return "", err
	}
	if err := s.checkDir(); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", err
	}
	f, err := openSecretFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	// The checks are made on the open file, not on the name: the name may
	// point somewhere else by the time it is read.
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("secret: read %s: %w", path, err)
	}
	if err := checkSecretFile(path, info); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return "", fmt.Errorf("secret: read %s: %w", path, err)
	}
	if len(data) > maxFileSize {
		return "", fmt.Errorf("%w: %s holds more than %d bytes", ErrTooLong, path, maxFileSize)
	}
	value := strings.TrimRight(strings.TrimPrefix(string(data), utf8BOM), "\r\n")
	if value == "" {
		return "", ErrNotFound
	}
	return value, nil
}

// checkSecretFile refuses anything but a regular file and, outside Windows,
// a file that grants any access to group or others or belongs to another
// user. info describes the opened file.
func checkSecretFile(path string, info fs.FileInfo) error {
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("%w: %s is a symbolic link", ErrInsecureFile, path)
	case !info.Mode().IsRegular():
		return fmt.Errorf("%w: %s is not a regular file", ErrInsecureFile, path)
	case runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0:
		return fmt.Errorf("%w: %s is accessible by other users (mode %04o); run chmod 600 on it",
			ErrInsecureFile, path, info.Mode().Perm())
	case ownedByOther(info):
		return fmt.Errorf("%w: %s belongs to another user", ErrInsecureFile, path)
	}
	return nil
}

// Set implements Store. It creates Dir with mode 0700 when missing. It
// refuses a value longer than 64 KiB with an error wrapping [ErrTooLong],
// and one that ends in a line break or starts with a UTF-8 byte order
// mark, which Get would not return unchanged.
func (s *FileStore) Set(account, value string) error {
	switch {
	case value == "":
		return ErrEmpty
	case len(value) > maxFileSize:
		return fmt.Errorf("%w: a FileStore keeps at most %d bytes", ErrTooLong, maxFileSize)
	case strings.HasSuffix(value, "\n") || strings.HasSuffix(value, "\r"):
		return errors.New("secret: a FileStore cannot keep a value that ends in a line break")
	case strings.HasPrefix(value, utf8BOM):
		return errors.New("secret: a FileStore cannot keep a value that starts with a byte order mark")
	}
	path, err := s.path(account)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("secret: create %s: %w", s.Dir, err)
	}
	if err := s.checkDir(); err != nil {
		return err
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
