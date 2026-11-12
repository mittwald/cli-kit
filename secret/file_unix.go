//go:build unix

package secret

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// openSecretFile opens the file at path for reading without following a
// symbolic link there, which is refused with an error wrapping
// [ErrInsecureFile]. A missing file is reported with an error wrapping
// fs.ErrNotExist. The file is opened without blocking, so a named pipe put
// in its place is refused by the caller's checks instead of stalling the
// open.
func openSecretFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return f, err
	}
	// A symbolic link is refused with ELOOP on most systems and with EMLINK
	// on FreeBSD. Where it is something else again (EFTYPE on NetBSD), ask
	// what is there to word the refusal; the open has failed either way.
	if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EMLINK) {
		return nil, fmt.Errorf("%w: %s is a symbolic link", ErrInsecureFile, path)
	}
	if info, statErr := os.Lstat(path); statErr == nil && info.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: %s is a symbolic link", ErrInsecureFile, path)
	}
	return nil, fmt.Errorf("secret: read %s: %w", path, err)
}

// ownedByOther reports whether the file belongs to a user other than the
// one this process acts as.
func ownedByOther(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) != os.Geteuid()
}
