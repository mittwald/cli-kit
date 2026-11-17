package fsx

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/mittwald/cli-kit/internal/atomicfile"
)

// ErrSymlink reports a symbolic link at the path [WriteFile] was asked to
// write, which it refuses to replace.
var ErrSymlink = errors.New("fsx: refusing to replace a symbolic link")

// WriteFile writes data to path and replaces an existing file atomically:
// it writes a temporary file in the same directory, syncs it to disk and
// renames it into place. A reader sees the old contents or the new ones,
// never part of either, and an interrupted write leaves the old file as it
// was.
//
// The new file gets mode perm, as it is given: unlike os.WriteFile, the
// umask does not apply, and a file that already exists does not keep its
// mode. The mode is set before any data reaches the file, so data written
// with mode 0600 is never readable by others. On Windows only the
// read-only bit, set when perm has no write permission for the owner, can
// be set.
//
// The directory must exist. A symbolic link at path is refused with an
// error wrapping [ErrSymlink], as secret.WriteFile refuses one: the rename
// would replace the link instead of the file it points to, which is rarely
// what whoever made the link wanted.
func WriteFile(path string, data []byte, perm fs.FileMode) error {
	err := atomicfile.Write(path, data, perm, ".tmp-*")
	switch {
	case errors.Is(err, atomicfile.ErrSymlink):
		return fmt.Errorf("%w: %s", ErrSymlink, path)
	case err != nil:
		return fmt.Errorf("fsx: write %s: %w", path, err)
	}
	return nil
}
