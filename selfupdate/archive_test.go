package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"testing"
)

func tarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: 0o755, Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipped(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractPicksTheBinaryNotTheReadme(t *testing.T) {
	big := bytes.Repeat([]byte{0xAB}, 3<<20)
	archive := tarGz(t, map[string][]byte{"README.md": make([]byte, 500), "renamed": big})
	got, err := Extract("tool.tar.gz", archive, "tool")
	if err != nil {
		t.Fatalf("Extract = %v", err)
	}
	if len(got) != len(big) {
		t.Fatalf("picked %d bytes, want the binary's %d", len(got), len(big))
	}
}

func TestExtractPrefersTheNamedBinary(t *testing.T) {
	archive := tarGz(t, map[string][]byte{
		"tool_1.2.3/tool":       []byte("small binary"),
		"tool_1.2.3/completion": bytes.Repeat([]byte("x"), 2<<20),
	})
	got, err := Extract("tool.tgz", archive, "tool")
	if err != nil || string(got) != "small binary" {
		t.Fatalf("Extract = %q, %v", got, err)
	}
	zipArchive := zipped(t, map[string][]byte{"tool.exe": []byte("windows binary"), "README.md": []byte("readme")})
	got, err = Extract("tool_Windows_x86_64.zip", zipArchive, "tool")
	if err != nil || string(got) != "windows binary" {
		t.Fatalf("Extract zip = %q, %v", got, err)
	}
}

func TestExtractRejectsArchiveWithNoBinary(t *testing.T) {
	archive := tarGz(t, map[string][]byte{"README.md": make([]byte, 500), "LICENSE": make([]byte, 200)})
	if _, err := Extract("tool.tar.gz", archive, "tool"); !errors.Is(err, ErrNoBinary) {
		t.Fatalf("Extract = %v, want ErrNoBinary", err)
	}
	if _, err := Extract("tool.tar.gz", tarGz(t, nil), "tool"); !errors.Is(err, ErrNoBinary) {
		t.Fatalf("empty archive = %v, want ErrNoBinary", err)
	}
	if _, err := Extract("tool.rar", nil, "tool"); err == nil {
		t.Fatal("unsupported archive type accepted")
	}
	if _, err := Extract("tool.tar.gz", []byte("not gzip"), "tool"); err == nil {
		t.Fatal("corrupt archive accepted")
	}
}
