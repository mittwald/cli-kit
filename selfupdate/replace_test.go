package selfupdate

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestReplaceExecutable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(path, []byte("old"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceExecutable(path, []byte("new")); err != nil {
		t.Fatalf("ReplaceExecutable = %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "new" {
		t.Fatalf("file holds %q", data)
	}
	if runtime.GOOS != "windows" {
		if info, _ := os.Stat(path); info.Mode().Perm() != 0o750 {
			t.Fatalf("mode = %v, want 0750 kept", info.Mode().Perm())
		}
	}
	// Only Windows keeps the old executable, under a name of this
	// package's own, until RemoveOld deletes it.
	for _, name := range dirNames(t, filepath.Dir(path)) {
		if name != "tool" && (runtime.GOOS != "windows" || !strings.HasPrefix(name, oldPrefix(path))) {
			t.Errorf("left behind %s", name)
		}
	}
	RemoveOld(path)
	if names := dirNames(t, filepath.Dir(path)); !slices.Equal(names, []string{"tool"}) {
		t.Errorf("RemoveOld left %v", names)
	}
	if err := ReplaceExecutable(path, nil); err == nil {
		t.Fatal("empty binary accepted")
	}
}

// dirNames returns the names of the files in dir, sorted.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}

// stage writes a file to move in for path, the way ReplaceExecutable
// stages the new binary.
func stage(t *testing.T, path, content string) string {
	t.Helper()
	staged := filepath.Join(filepath.Dir(path), stagedPrefix(path)+content)
	if err := os.WriteFile(staged, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return staged
}

// The way Windows replaces an executable, which runs on every system here:
// each update moves the old executable to a name of its own, so one that
// is still running, and cannot be deleted, does not block the next update.
func TestSwapAsideKeepsEveryOldExecutableUnderItsOwnName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool.exe")
	if err := os.WriteFile(path, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, next := range []string{"v2", "v3"} {
		if err := swapAside(path, stage(t, path, next)); err != nil {
			t.Fatalf("swapAside to %s = %v", next, err)
		}
		if data, _ := os.ReadFile(path); string(data) != next {
			t.Fatalf("executable holds %q, want %q", data, next)
		}
	}
	var old []string
	for _, name := range dirNames(t, filepath.Dir(path)) {
		if name == "tool.exe" {
			continue
		}
		if !strings.HasPrefix(name, oldPrefix(path)) {
			t.Errorf("left behind %s", name)
		}
		data, _ := os.ReadFile(filepath.Join(filepath.Dir(path), name))
		old = append(old, string(data))
	}
	slices.Sort(old)
	if !slices.Equal(old, []string{"v1", "v2"}) {
		t.Fatalf("old executables hold %v, want v1 and v2 under names of their own", old)
	}

	// Nothing at the path yet: the staged file is moved in.
	fresh := filepath.Join(t.TempDir(), "tool.exe")
	if err := swapAside(fresh, stage(t, fresh, "v1")); err != nil {
		t.Fatalf("swapAside to a new path = %v", err)
	}
	if names := dirNames(t, filepath.Dir(fresh)); !slices.Equal(names, []string{"tool.exe"}) {
		t.Fatalf("directory holds %v", names)
	}
}

// When the new binary cannot be moved in, the old one is back in place.
func TestSwapAsideMovesTheOldExecutableBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool.exe")
	if err := os.WriteFile(path, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(filepath.Dir(path), stagedPrefix(path)+"gone")
	if err := swapAside(path, missing); err == nil {
		t.Fatal("swapAside moved in a file that does not exist")
	}
	if data, _ := os.ReadFile(path); string(data) != "v1" {
		t.Fatalf("executable holds %q, want the old one back", data)
	}
	if names := dirNames(t, filepath.Dir(path)); !slices.Equal(names, []string{"tool.exe"}) {
		t.Fatalf("directory holds %v", names)
	}
}

// RemoveOld deletes what this package left behind and nothing else: old
// executables only where they are moved aside, staged binaries only once
// they are stale, and no file of another name.
func TestRemoveOldDeletesOnlyItsOwnLeftovers(t *testing.T) {
	now := time.Now()
	setup := func(t *testing.T) (path string, write func(name string, age time.Duration)) {
		t.Helper()
		dir := t.TempDir()
		write = func(name string, age time.Duration) {
			t.Helper()
			file := filepath.Join(dir, name)
			if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(file, now.Add(-age), now.Add(-age)); err != nil {
				t.Fatal(err)
			}
		}
		write("tool", 0)
		write("tool.old", 48*time.Hour)            // a backup the user made, or the old name on Windows
		write("tool.bak", 48*time.Hour)            // the user's
		write(".tool.old-ABC", 48*time.Hour)       // an old executable moved aside
		write(".tool.update-111", 2*time.Hour)     // staged by an update that was interrupted
		write(".tool.update-222", time.Minute)     // staged by an update that is running
		write(".toolbox.update-333", 48*time.Hour) // another tool's
		write(".toolbox.old-DEF", 48*time.Hour)    // another tool's
		write("tool.update-444", 48*time.Hour)     // not this package's name
		if err := os.Mkdir(filepath.Join(dir, ".tool.update-dir"), 0o750); err != nil {
			t.Fatal(err)
		}
		return filepath.Join(dir, "tool"), write
	}

	path, _ := setup(t)
	removeLeftovers(path, false, now)
	want := []string{".tool.old-ABC", ".tool.update-222", ".tool.update-dir", ".toolbox.old-DEF", ".toolbox.update-333",
		"tool", "tool.bak", "tool.old", "tool.update-444"}
	if got := dirNames(t, filepath.Dir(path)); !slices.Equal(got, want) {
		t.Errorf("without moving aside:\n got %v\nwant %v", got, want)
	}

	path, _ = setup(t)
	removeLeftovers(path, true, now)
	want = []string{".tool.update-222", ".tool.update-dir", ".toolbox.old-DEF", ".toolbox.update-333",
		"tool", "tool.bak", "tool.update-444"}
	if got := dirNames(t, filepath.Dir(path)); !slices.Equal(got, want) {
		t.Errorf("where old executables are moved aside:\n got %v\nwant %v", got, want)
	}

	// RemoveOld is that for the system it runs on.
	path, _ = setup(t)
	RemoveOld(path)
	_, backupErr := os.Stat(path + ".old")
	_, staleErr := os.Stat(filepath.Join(filepath.Dir(path), ".tool.update-111"))
	if (backupErr == nil) == (runtime.GOOS == "windows") || !os.IsNotExist(staleErr) {
		t.Errorf("RemoveOld on %s: tool.old: %v, stale staged binary: %v", runtime.GOOS, backupErr, staleErr)
	}
	RemoveOld(filepath.Join(t.TempDir(), "missing", "tool")) // nothing to do, and no panic
}

func TestReplaceExecutableReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "tool")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := ReplaceExecutable(path, []byte("new")); err == nil {
		t.Fatal("replaced a file in a read-only directory")
	}
	if data, _ := os.ReadFile(path); string(data) != "old" {
		t.Fatalf("file holds %q", data)
	}
}

func TestPackageManaged(t *testing.T) {
	if runtime.GOOS == "windows" {
		for path, want := range map[string]bool{
			`C:\Users\a\scoop\apps\tool\current\tool.exe`:                   true,
			`C:\ProgramData\chocolatey\lib\tool\tool.exe`:                   true,
			`C:\Users\a\AppData\Local\Microsoft\WinGet\Packages\tool\x.exe`: true,
			`C:\Program Files\WinGet\Packages\tool\x.exe`:                   true,
			`C:\Users\a\bin\tool.exe`:                                       false,
		} {
			if got := PackageManaged(path); got != want {
				t.Errorf("PackageManaged(%q) = %v, want %v", path, got, want)
			}
		}
		return
	}
	for path, want := range map[string]bool{
		"/usr/bin/tool":                                true,
		"/usr/local/Cellar/tool/1.2.3/bin/tool":        true,
		"/opt/homebrew/Cellar/tool/1.2.3/bin/tool":     true,
		"/home/linuxbrew/.linuxbrew/Cellar/tool/bin/x": true,
		"/nix/store/abc-tool/bin/tool":                 true,
		"/opt/local/bin/tool":                          true, // MacPorts
		"/home/a/.linuxbrew/Cellar/tool/1.2.3/bin/x":   true,
		"/var/lib/snapd/snap/tool/12/bin/tool":         true,
		"/snap/tool/12/bin/tool":                       true,
		"/home/a/.local/share/mise/installs/tool/1/x":  true,
		"/home/a/.asdf/installs/tool/1.2.3/bin/tool":   true,
		"/opt/tool/bin/tool":                           false,
		"/opt/localtool/bin/tool":                      false,
		"/usr/local/bin/tool":                          false,
		"/home/a/.local/bin/tool":                      false,
		"/home/a/go/bin/tool":                          false,
	} {
		if got := PackageManaged(path); got != want {
			t.Errorf("PackageManaged(%q) = %v, want %v", path, got, want)
		}
	}
}

// A link in a bin directory leads into the package manager's directory:
// the executable is managed although the link's own path is not.
func TestPackageManagedResolvesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the directories below are Unix ones")
	}
	dir := t.TempDir()
	cellar := filepath.Join(dir, ".linuxbrew", "Cellar", "tool", "1.2.3", "bin")
	bin := filepath.Join(dir, ".linuxbrew", "bin")
	for _, d := range []string{cellar, bin} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(cellar, "tool"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(bin, "tool")
	if err := os.Symlink(filepath.Join(cellar, "tool"), link); err != nil {
		t.Skipf("cannot create a symbolic link: %v", err)
	}
	if managedPath(link) {
		t.Fatalf("test setup: the link's own path %s counts as managed", link)
	}
	if !PackageManaged(link) {
		t.Errorf("PackageManaged(%s) = false, want the link followed into the Cellar", link)
	}
	own := filepath.Join(bin, "own")
	if err := os.WriteFile(own, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if PackageManaged(own) {
		t.Errorf("PackageManaged(%s) = true for a file outside the Cellar", own)
	}
}
