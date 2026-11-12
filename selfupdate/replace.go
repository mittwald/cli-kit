package selfupdate

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ErrManaged reports that the executable belongs to a package manager,
// which is the one to update it.
var ErrManaged = errors.New("selfupdate: executable is managed by a package manager")

// managedPrefixes are directories only a package manager writes to.
var managedPrefixes = []string{
	"/bin/", "/sbin/", "/usr/bin/", "/usr/sbin/", "/usr/lib/", "/usr/libexec/", "/usr/share/",
	"/usr/local/Cellar/", "/usr/local/Caskroom/",
	"/opt/homebrew/Cellar/", "/opt/homebrew/Caskroom/",
	"/opt/local/", // MacPorts
	"/nix/store/", "/snap/", "/var/lib/snapd/snap/", "/var/lib/flatpak/",
}

// managedParts are path parts of package and version managers that
// install below a directory of the user's choosing, often the home
// directory: Homebrew on Linux, mise and asdf.
var managedParts = []string{"/.linuxbrew/Cellar/", "/mise/installs/", "/.asdf/installs/"}

// managedWindows are path parts of Windows package managers' install
// directories, compared in lower case. WinGet installs below
// %LOCALAPPDATA%\Microsoft\WinGet\Packages for a user and below
// %ProgramFiles%\WinGet\Packages for the machine.
var managedWindows = []string{`\scoop\apps\`, `\chocolatey\`, `\winget\packages\`}

// PackageManaged reports whether the executable at path lies where a
// package or version manager installs binaries: the system directories a
// .deb or .rpm fills, Homebrew's Cellar (on Linux too), MacPorts, the Nix
// store, Snap and Flatpak, mise and asdf, or Scoop, Chocolatey and WinGet
// directories on Windows. It looks at path as it is given and at what it
// leads to once its symbolic links are resolved, so the link Homebrew
// keeps in its bin directory counts like the file in the Cellar. It is the
// default for [Updater.Managed].
//
// It goes by the path alone and knows the usual locations only. A package
// that installs somewhere else, such as below /opt or, on the BSDs, in
// /usr/local/bin beside what the user put there, is not recognized.
func PackageManaged(path string) bool {
	if managedPath(path) {
		return true
	}
	resolved, err := filepath.EvalSymlinks(path)
	return err == nil && managedPath(resolved)
}

// managedPath reports whether path itself lies in a package manager's
// directory.
func managedPath(path string) bool {
	if runtime.GOOS == "windows" {
		lower := strings.ToLower(filepath.Clean(path))
		for _, part := range managedWindows {
			if strings.Contains(lower, part) {
				return true
			}
		}
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	for _, prefix := range managedPrefixes {
		if strings.HasPrefix(clean, prefix) {
			return true
		}
	}
	for _, part := range managedParts {
		if strings.Contains(clean, part) {
			return true
		}
	}
	return false
}

// staleAfter is the age from which a staged binary counts as left behind
// by an interrupted update rather than as part of one that is running.
const staleAfter = time.Hour

// stagedPrefix starts the names of the files ReplaceExecutable stages the
// new binary for path in, oldPrefix those it moves the old one to on
// Windows. Both names are this package's own, so that cleaning up never
// touches a file somebody else put beside the executable.
func stagedPrefix(path string) string { return "." + filepath.Base(path) + ".update-" }

func oldPrefix(path string) string { return "." + filepath.Base(path) + ".old-" }

// ReplaceExecutable replaces the file at path with binary: the new binary
// is written next to it and renamed over it. On Unix that is atomic, so
// the path always holds a complete binary, and it is safe for a running
// executable. The new file keeps the old one's permission bits.
//
// Windows cannot overwrite a running executable, so there the old one is
// first moved aside, to a name of its own beside it (".tool.exe.old-"
// and random characters), which [RemoveOld] deletes once that executable
// no longer runs. Between the two renames the path
// holds no file for a moment. When the new binary cannot be moved in, the
// old one is moved back; if that fails as well, the error says where the
// old executable now is.
func ReplaceExecutable(path string, binary []byte) error {
	if len(binary) == 0 {
		return fmt.Errorf("%w: empty binary", ErrNoBinary)
	}
	dir := filepath.Dir(path)
	mode := fs.FileMode(0o755)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm() | 0o100
	}
	// The replacement must be on the same file system as path, so the
	// rename is atomic.
	tmp, err := os.CreateTemp(dir, stagedPrefix(path)+"*")
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("selfupdate: no write permission in %s: run the update with sufficient privileges, "+
				"or install the binary in a directory you own: %w", dir, err)
		}
		return fmt.Errorf("selfupdate: stage new binary: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // fails harmlessly once renamed
	if _, err := tmp.Write(binary); err != nil {
		return fmt.Errorf("selfupdate: write new binary: %w", errors.Join(err, tmp.Close()))
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("selfupdate: write new binary: %w", errors.Join(err, tmp.Close()))
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("selfupdate: write new binary: %w", err)
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("selfupdate: make new binary executable: %w", err)
	}
	if runtime.GOOS == "windows" {
		return swapAside(path, tmpPath)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("selfupdate: replace %s: %w", path, err)
	}
	return nil
}

// swapAside puts the staged file in path's place the way Windows needs it
// for a running executable: the file at path is moved aside under a name
// no earlier update used, as an old executable that still runs can neither
// be deleted nor replaced, and then the staged file is moved in. When that
// fails the old file is moved back.
func swapAside(path, staged string) error {
	old := filepath.Join(filepath.Dir(path), oldPrefix(path)+rand.Text())
	moved := true
	if err := os.Rename(path, old); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("selfupdate: move %s aside: %w", path, err)
		}
		moved = false
	}
	err := os.Rename(staged, path)
	if err == nil {
		return nil
	}
	if moved {
		if back := os.Rename(old, path); back != nil {
			return fmt.Errorf("selfupdate: replace %s: %w; the previous executable could not be put back "+
				"and is now at %s: %w", path, err, old, back)
		}
	}
	return fmt.Errorf("selfupdate: replace %s: %w", path, err)
}

// RemoveOld deletes what [ReplaceExecutable] may leave beside the
// executable at path. On Windows these are the old executables it moved
// aside, which can be deleted once they no longer run, and the path+".old"
// file earlier versions of this package moved them to. On every system
// they are new binaries an update staged more than an hour ago and never
// moved in, because it was interrupted. Nothing else is touched: on Unix
// in particular, a path+".old" file is the user's own. It ignores every
// error, as the next run tries again. [Updater.Update] and
// [Updater.Install] call it once they know the executable may be
// replaced.
func RemoveOld(path string) {
	removeLeftovers(path, runtime.GOOS == "windows", time.Now())
}

// removeLeftovers is RemoveOld for a system that moves old executables
// aside or does not, at a given time.
func removeLeftovers(path string, movesAside bool, now time.Time) {
	dir := filepath.Dir(path)
	if movesAside {
		_ = os.Remove(path + ".old")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		switch {
		case movesAside && strings.HasPrefix(name, oldPrefix(path)):
			_ = os.Remove(filepath.Join(dir, name))
		case strings.HasPrefix(name, stagedPrefix(path)):
			info, err := entry.Info()
			if err == nil && info.Mode().IsRegular() && now.Sub(info.ModTime()) > staleAfter {
				_ = os.Remove(filepath.Join(dir, name))
			}
		}
	}
}

// executablePath returns the running executable with symbolic links
// resolved, so a link in a bin directory leads to the real file.
func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("selfupdate: locate running executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("selfupdate: resolve running executable: %w", err)
	}
	return resolved, nil
}
