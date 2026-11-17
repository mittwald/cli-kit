package fsx

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteFile(t *testing.T) {
	tests := []struct {
		name string
		old  []byte // nil: no file before
		perm fs.FileMode
	}{
		{"new file", nil, 0o644},
		{"new private file", nil, 0o600},
		{"replaces a file", []byte("old contents"), 0o644},
		{"replaces a file of another mode", []byte("old"), 0o600},
		{"empty data", []byte("old"), 0o640},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "file")
			if tt.old != nil {
				if err := os.WriteFile(path, tt.old, 0o666); err != nil {
					t.Fatal(err)
				}
			}
			data := []byte("new contents\n")
			if tt.name == "empty data" {
				data = nil
			}
			if err := WriteFile(path, data, tt.perm); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != string(data) {
				t.Errorf("contents = %q, %v; want %q", got, err, data)
			}
			if runtime.GOOS != "windows" {
				if info, err := os.Stat(path); err != nil || info.Mode().Perm() != tt.perm {
					t.Errorf("mode = %v, %v; want %v", info.Mode().Perm(), err, tt.perm)
				}
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Errorf("directory holds %v, %v; want the file alone", entries, err)
			}
		})
	}
}

func TestWriteFileErrors(t *testing.T) {
	dir := t.TempDir()
	if err := WriteFile(filepath.Join(dir, "missing", "file"), []byte("x"), 0o600); err == nil {
		t.Error("writing into a missing directory succeeded")
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(sub, []byte("x"), 0o600); err == nil {
		t.Error("replacing a directory succeeded")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("a failed write left %v behind", entries)
	}
}

func TestWriteFileRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create a symbolic link: %v", err)
	}
	if err := WriteFile(link, []byte("new"), 0o600); !errors.Is(err, ErrSymlink) {
		t.Fatalf("err = %v, want ErrSymlink", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "kept" {
		t.Errorf("target = %q, want it unchanged", data)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("the link was replaced: %v, %v", info, err)
	}
}
