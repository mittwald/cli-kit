package selfupdate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrManaged reports that the executable belongs to a package manager,
// which is the one to update it.
var ErrManaged = errors.New("selfupdate: executable is managed by a package manager")

// managedPrefixes are directories only a package manager writes to.
var managedPrefixes = []string{
	"/bin/", "/sbin/", "/usr/bin/", "/usr/sbin/", "/usr/lib/", "/usr/libexec/", "/usr/share/",
	"/usr/local/Cellar/", "/usr/local/Caskroom/",
	"/opt/homebrew/Cellar/", "/opt/homebrew/Caskroom/",
	"/home/linuxbrew/.linuxbrew/Cellar/",
	"/nix/store/", "/snap/", "/var/lib/flatpak/",
}

// managedWindows are path parts of Windows package managers' install
// directories, compared in lower case.
var managedWindows = []string{`\scoop\apps\`, `\chocolatey\`, `\microsoft\winget\packages\`}

// PackageManaged reports whether the executable at path, with symbolic
// links resolved, lies where a package manager installs binaries: the
// system directories a .deb or .rpm fills, Homebrew's Cellar, the Nix
// store, Snap and Flatpak, or Scoop, Chocolatey and WinGet directories on
// Windows. It is the default for [Updater.Managed].
func PackageManaged(path string) bool {
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
	return false
}

// ReplaceExecutable replaces the file at path with binary, atomically: the
// new binary is written next to it and renamed over it, so the path always
// holds a complete binary. Replacing a running executable this way is safe
// on Unix. Windows cannot overwrite a running executable, so there the old
// one is first renamed to path+".old", which [RemoveOld] deletes on a later
// run. The new file keeps the old one's permission bits.
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
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".update-*")
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
		tmp.Close()
		return fmt.Errorf("selfupdate: write new binary: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("selfupdate: write new binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("selfupdate: write new binary: %w", err)
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("selfupdate: make new binary executable: %w", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Rename(tmpPath, path); err != nil {
			return fmt.Errorf("selfupdate: replace %s: %w", path, err)
		}
		return nil
	}
	old := path + ".old"
	_ = os.Remove(old)
	if err := os.Rename(path, old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("selfupdate: move %s aside: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Rename(old, path)
		return fmt.Errorf("selfupdate: replace %s: %w", path, err)
	}
	return nil
}

// RemoveOld deletes the path+".old" file [ReplaceExecutable] leaves behind
// on Windows, once the old executable no longer runs. It ignores a missing
// file and every other error, as the next run tries again.
func RemoveOld(path string) {
	_ = os.Remove(path + ".old")
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
