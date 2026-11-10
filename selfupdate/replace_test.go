package selfupdate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if e.Name() != "tool" && e.Name() != "tool.old" {
			t.Errorf("left behind %s", e.Name())
		}
	}
	RemoveOld(path)
	if _, err := os.Stat(path + ".old"); !os.IsNotExist(err) {
		t.Errorf("RemoveOld left %s.old", path)
	}
	if err := ReplaceExecutable(path, nil); err == nil {
		t.Fatal("empty binary accepted")
	}
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
		"/usr/local/bin/tool":                          false,
		"/home/a/.local/bin/tool":                      false,
		"/home/a/go/bin/tool":                          false,
	} {
		if got := PackageManaged(path); got != want {
			t.Errorf("PackageManaged(%q) = %v, want %v", path, got, want)
		}
	}
}
