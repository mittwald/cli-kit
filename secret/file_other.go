//go:build !unix

package secret

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// openSecretFile opens the file at path for reading, refusing a symbolic
// link there with an error wrapping [ErrInsecureFile]. A missing file is
// reported with an error wrapping fs.ErrNotExist. Unlike on Unix, the link
// is looked for before the file is opened, since the open itself cannot
// refuse one.
func openSecretFile(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("secret: read %s: %w", path, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: %s is a symbolic link", ErrInsecureFile, path)
	}
	f, err := os.Open(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("secret: read %s: %w", path, err)
	}
	return f, err
}

// ownedByOther reports whether the file belongs to another user; that is
// only known on Unix.
func ownedByOther(fs.FileInfo) bool { return false }
