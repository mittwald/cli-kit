// Package atomicfile replaces a file atomically. It is the one
// implementation behind fsx.WriteFile and secret.WriteFile, which word its
// errors each in their own way.
package atomicfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// ErrSymlink reports a symbolic link at the path to write.
var ErrSymlink = errors.New("is a symbolic link")

// Write writes data to path through a temporary file in the same directory,
// named after pattern as os.CreateTemp reads it, and renames it into place,
// so a reader sees the old contents or the new ones, never part of either.
// The temporary file gets mode perm before any data reaches it; outside
// Windows a failure to set it is an error, on Windows it is ignored, since
// only the read-only bit can be set there. The data is synced to disk
// before the rename.
//
// The directory must exist. A symbolic link at path is refused with
// [ErrSymlink]. Errors are returned as the os package returns them; the
// caller adds the path and its prefix.
func Write(path string, data []byte, perm fs.FileMode, pattern string) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return ErrSymlink
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), pattern)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // fails harmlessly once renamed
	if err := tmp.Chmod(perm.Perm()); err != nil && runtime.GOOS != "windows" {
		return errors.Join(err, tmp.Close())
	}
	if _, err := tmp.Write(data); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Sync(); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
