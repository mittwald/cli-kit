package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode"
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

// archiveFile is one entry of an archive whose order matters.
type archiveFile struct {
	name string
	data []byte
	// kind is the tar type flag; zero means a regular file.
	kind byte
}

// tarGzOrdered packs files in the order given.
func tarGzOrdered(t *testing.T, files ...archiveFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		hdr := &tar.Header{Name: f.name, Mode: 0o755, Typeflag: tar.TypeReg, Size: int64(len(f.data))}
		if f.kind != 0 {
			hdr.Typeflag, hdr.Size, hdr.Linkname = f.kind, 0, string(f.data)
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if _, err := tw.Write(f.data); err != nil {
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

var (
	darwinARM64  = Platform{OS: "darwin", Arch: "arm64"}
	windowsAMD64 = Platform{OS: "windows", Arch: "amd64"}
)

// executable returns content that passes for an executable of platform p:
// the magic number of its format, then payload.
func executable(p Platform, payload string) []byte {
	switch executableFormat(p.OS) {
	case formatPE:
		return []byte("MZ" + payload)
	case formatMachO:
		return []byte("\xcf\xfa\xed\xfe" + payload)
	case formatELF:
		return []byte("\x7fELF" + payload)
	}
	return []byte(payload)
}

func TestExtractPicksTheBinaryNotTheReadme(t *testing.T) {
	big := executable(linuxAMD64, strings.Repeat("\xAB", 3<<20))
	archive := tarGz(t, map[string][]byte{"README.md": make([]byte, 500), "renamed": big})
	got, err := ExtractFor(linuxAMD64, "tool.tar.gz", archive, "tool")
	if err != nil {
		t.Fatalf("ExtractFor = %v", err)
	}
	if len(got) != len(big) {
		t.Fatalf("picked %d bytes, want the binary's %d", len(got), len(big))
	}
}

func TestExtractPrefersTheNamedBinary(t *testing.T) {
	small := executable(linuxAMD64, "small binary")
	archive := tarGz(t, map[string][]byte{
		"tool_1.2.3/tool":       small,
		"tool_1.2.3/completion": bytes.Repeat([]byte("x"), 2<<20),
	})
	got, err := ExtractFor(linuxAMD64, "tool.tgz", archive, "tool")
	if err != nil || !bytes.Equal(got, small) {
		t.Fatalf("ExtractFor = %q, %v", got, err)
	}
	windows := executable(windowsAMD64, "windows binary")
	zipArchive := zipped(t, map[string][]byte{"tool.exe": windows, "README.md": []byte("readme")})
	got, err = ExtractFor(windowsAMD64, "tool_Windows_x86_64.zip", zipArchive, "tool")
	if err != nil || !bytes.Equal(got, windows) {
		t.Fatalf("ExtractFor zip = %q, %v", got, err)
	}
}

// Extract is ExtractFor for the platform the tests run on.
func TestExtractIsForTheRunningPlatform(t *testing.T) {
	binary := executable(CurrentPlatform(), "binary")
	got, err := Extract("tool.tar.gz", tarGz(t, map[string][]byte{"tool": binary}), "tool")
	if err != nil || !bytes.Equal(got, binary) {
		t.Fatalf("Extract = %q, %v", got, err)
	}
	got, err = ExtractFor(Platform{}, "tool.tar.gz", tarGz(t, map[string][]byte{"tool": binary}), "tool")
	if err != nil || !bytes.Equal(got, binary) {
		t.Fatalf("ExtractFor with the zero Platform = %q, %v", got, err)
	}
}

// A completion script is commonly called what the binary is called. The
// binary is the file of that name nearest the archive's root, and among
// several equally near the largest, wherever in the archive it comes.
func TestExtractTakesTheShallowestFileOfTheBinarysName(t *testing.T) {
	binary := executable(linuxAMD64, strings.Repeat("b", 4096))
	completion := archiveFile{name: "tool_1.1.0/completions/bash/tool", data: []byte("complete -F _tool tool\n")}
	license := archiveFile{name: "tool_1.1.0/LICENSE", data: []byte("MIT")}
	atRoot := archiveFile{name: "tool_1.1.0/tool", data: binary}
	for label, files := range map[string][]archiveFile{
		"completion first": {completion, license, atRoot},
		"binary first":     {atRoot, license, completion},
	} {
		got, err := ExtractFor(linuxAMD64, "tool.tar.gz", tarGzOrdered(t, files...), "tool")
		if err != nil || !bytes.Equal(got, binary) {
			t.Errorf("%s: ExtractFor = %.40q, %v; want the binary", label, got, err)
		}
	}

	sibling := archiveFile{name: "completions/tool", data: []byte("complete -F _tool tool\n")}
	nested := archiveFile{name: "bin/tool", data: binary}
	for label, files := range map[string][]archiveFile{
		"script first": {sibling, nested},
		"binary first": {nested, sibling},
	} {
		got, err := ExtractFor(linuxAMD64, "tool.tar.gz", tarGzOrdered(t, files...), "tool")
		if err != nil || !bytes.Equal(got, binary) {
			t.Errorf("equally deep, %s: ExtractFor = %.40q, %v; want the larger file", label, got, err)
		}
	}
}

// "tool.exe" is the binary on Windows only: elsewhere it is another file.
func TestExtractTakesTheExeNameOnWindowsOnly(t *testing.T) {
	linux := executable(linuxAMD64, "linux")
	windows := executable(windowsAMD64, "windows binary")
	archive := tarGzOrdered(t, archiveFile{name: "tool.exe", data: windows}, archiveFile{name: "tool", data: linux})
	got, err := ExtractFor(linuxAMD64, "tool.tar.gz", archive, "tool")
	if err != nil || !bytes.Equal(got, linux) {
		t.Fatalf("for Linux: ExtractFor = %q, %v", got, err)
	}
	// On Windows both names are the binary's, and the PE file is larger.
	got, err = ExtractFor(windowsAMD64, "tool.tar.gz", archive, "tool")
	if err != nil || !bytes.Equal(got, windows) {
		t.Fatalf("for Windows: ExtractFor = %q, %v", got, err)
	}
	zipArchive := zipped(t, map[string][]byte{`dir\tool.exe`: windows})
	if got, err = ExtractFor(windowsAMD64, "tool.ZIP", zipArchive, "tool"); err != nil || !bytes.Equal(got, windows) {
		t.Fatalf("backslash path: ExtractFor = %q, %v", got, err)
	}
}

// Whatever file is taken has to be an executable for the platform.
func TestExtractRefusesAFileThatIsNoExecutable(t *testing.T) {
	script := []byte("#!/bin/sh\nexec tool-real \"$@\"\n")
	tests := []struct {
		label    string
		platform Platform
		files    map[string][]byte
		ok       bool
	}{
		{"script of the binary's name", linuxAMD64, map[string][]byte{"tool": script}, false},
		{"only a completion of the binary's name", linuxAMD64, map[string][]byte{
			"completions/tool": script, "tool-linux": executable(linuxAMD64, strings.Repeat("x", 2<<20))}, false},
		{"largest file is data", linuxAMD64, map[string][]byte{"data.bin": make([]byte, 2<<20)}, false},
		{"ELF for Linux", linuxAMD64, map[string][]byte{"tool": executable(linuxAMD64, "x")}, true},
		{"ELF for FreeBSD", Platform{OS: "freebsd", Arch: "amd64"}, map[string][]byte{"tool": executable(linuxAMD64, "x")}, true},
		{"ELF for macOS", darwinARM64, map[string][]byte{"tool": executable(linuxAMD64, "x")}, false},
		{"ELF for Windows", windowsAMD64, map[string][]byte{"tool.exe": executable(linuxAMD64, "x")}, false},
		{"Mach-O for macOS", darwinARM64, map[string][]byte{"tool": executable(darwinARM64, "x")}, true},
		{"universal Mach-O for macOS", darwinARM64, map[string][]byte{"tool": []byte("\xca\xfe\xba\xbe\x00\x00\x00\x02")}, true},
		{"32-bit big-endian Mach-O", darwinARM64, map[string][]byte{"tool": []byte("\xfe\xed\xfa\xce")}, true},
		{"Mach-O for Linux", linuxAMD64, map[string][]byte{"tool": executable(darwinARM64, "x")}, false},
		{"PE for Windows", windowsAMD64, map[string][]byte{"tool.exe": executable(windowsAMD64, "x")}, true},
		{"PE for Linux", linuxAMD64, map[string][]byte{"tool": executable(windowsAMD64, "x")}, false},
		{"largest file is an executable", linuxAMD64, map[string][]byte{
			"renamed": executable(linuxAMD64, strings.Repeat("x", 2<<20))}, true},
		{"unchecked format", Platform{OS: "plan9", Arch: "amd64"}, map[string][]byte{"tool": script}, true},
	}
	for _, tt := range tests {
		_, err := ExtractFor(tt.platform, "tool.tar.gz", tarGz(t, tt.files), "tool")
		if tt.ok && err != nil {
			t.Errorf("%s: ExtractFor = %v", tt.label, err)
		}
		if !tt.ok && !errors.Is(err, ErrNoBinary) {
			t.Errorf("%s: ExtractFor = %v, want ErrNoBinary", tt.label, err)
		}
	}
}

// Links are no files, and a path that points out of the archive is only a
// name: nothing is written anywhere.
func TestExtractIgnoresLinksAndPaths(t *testing.T) {
	binary := executable(linuxAMD64, "binary")
	archive := tarGzOrdered(t,
		archiveFile{name: "tool", data: []byte("/etc/passwd"), kind: tar.TypeSymlink},
		archiveFile{name: "tool", data: []byte("other"), kind: tar.TypeLink},
		archiveFile{name: "../../../../tmp/x/tool", data: binary},
	)
	got, err := ExtractFor(linuxAMD64, "tool.tgz", archive, "tool")
	if err != nil || !bytes.Equal(got, binary) {
		t.Fatalf("ExtractFor = %q, %v", got, err)
	}
}

// Only the best candidate is read and kept: an entry that cannot be the
// binary is skipped, however large it is.
func TestExtractReadsOnlyCandidates(t *testing.T) {
	limits := archiveLimits{entry: 2 << 20, total: 64 << 20, entries: 100}
	binary := executable(linuxAMD64, "binary")
	huge := make([]byte, 3<<20) // above the entry limit
	archive := tarGzOrdered(t, archiveFile{name: "tool", data: binary}, archiveFile{name: "assets.bin", data: huge})
	got, err := extract(linuxAMD64, "tool.tar.gz", archive, "tool", limits)
	if err != nil || !bytes.Equal(got, binary) {
		t.Fatalf("extract = %.20q, %v; the entry after the binary was read", got, err)
	}
	zipArchive := zipped(t, map[string][]byte{"tool": binary, "assets.bin": huge, "more.bin": huge})
	got, err = extract(linuxAMD64, "tool.zip", zipArchive, "tool", limits)
	if err != nil || !bytes.Equal(got, binary) {
		t.Fatalf("extract zip = %.20q, %v", got, err)
	}
	// The binary itself may not exceed the limit, whether it is found by
	// its name or by its size.
	for name, archive := range map[string][]byte{
		"tool.tar.gz":    tarGz(t, map[string][]byte{"tool": huge}),
		"tool.zip":       zipped(t, map[string][]byte{"tool": huge}),
		"renamed.tar.gz": tarGz(t, map[string][]byte{"renamed": huge, "README.md": []byte("readme")}),
		"renamed.zip":    zipped(t, map[string][]byte{"renamed": huge, "README.md": []byte("readme")}),
	} {
		if _, err := extract(linuxAMD64, name, archive, "tool", limits); err == nil || !strings.Contains(err.Error(), "larger than") {
			t.Errorf("%s: extract = %v, want the oversized entry refused", name, err)
		}
	}
}

// An archive that expands to more than the limit, or holds more entries,
// is refused, although no single entry is too large.
func TestExtractBoundsTheArchive(t *testing.T) {
	var files []archiveFile
	for i := range 8 {
		files = append(files, archiveFile{name: fmt.Sprintf("pad%d", i), data: make([]byte, 1<<20)})
	}
	bomb := tarGzOrdered(t, files...)
	if len(bomb) > 64<<10 {
		t.Fatalf("test archive is %d bytes, expected it to compress well", len(bomb))
	}
	_, err := extract(linuxAMD64, "tool.tar.gz", bomb, "tool", archiveLimits{entry: 2 << 20, total: 4 << 20, entries: 100})
	if err == nil || !strings.Contains(err.Error(), "expands to more than") {
		t.Fatalf("extract = %v, want the expanded size refused", err)
	}
	// In a zip only the entries read count: each one here is larger than
	// the one before, so each is a candidate.
	growing := map[string][]byte{}
	for i := range 4 {
		growing[fmt.Sprintf("pad%d", i)] = make([]byte, 1<<20+i)
	}
	_, err = extract(linuxAMD64, "tool.zip", zipped(t, growing), "tool", archiveLimits{entry: 2 << 20, total: 1 << 20, entries: 100})
	if err == nil || !strings.Contains(err.Error(), "expands to more than") {
		t.Fatalf("extract zip = %v, want the expanded size refused", err)
	}
	// Within the limit the same archive is merely one without a binary.
	if _, err = extract(linuxAMD64, "tool.tar.gz", bomb, "tool", archiveLimits{entry: 2 << 20, total: 64 << 20, entries: 100}); !errors.Is(err, ErrNoBinary) {
		t.Fatalf("extract within the limit = %v, want ErrNoBinary", err)
	}
	for name, archive := range map[string][]byte{"tool.tar.gz": bomb, "tool.zip": zipped(t, growing)} {
		_, err = extract(linuxAMD64, name, archive, "tool", archiveLimits{entry: 2 << 20, total: 64 << 20, entries: 3})
		if err == nil || !strings.Contains(err.Error(), "more than 3 entries") {
			t.Errorf("%s: extract = %v, want the entry count refused", name, err)
		}
	}
}

// An entry's name is the archive's to choose; it reaches an error without
// control characters.
func TestExtractErrorsCarryNoControlCharacters(t *testing.T) {
	archive := tarGz(t, map[string][]byte{"a\x1b[31m\x1b]0;title\x07\u202e.txt": []byte("x")})
	_, err := ExtractFor(linuxAMD64, "tool.tar.gz", archive, "tool")
	if !errors.Is(err, ErrNoBinary) {
		t.Fatalf("ExtractFor = %v, want ErrNoBinary", err)
	}
	if strings.ContainsFunc(err.Error(), func(r rune) bool { return unicode.IsControl(r) || r == '\u202e' }) {
		t.Fatalf("error carries control characters: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "a[31m]0;title.txt (1 bytes)") {
		t.Fatalf("error does not name the entry: %q", err.Error())
	}
}

func TestExtractRejectsArchiveWithNoBinary(t *testing.T) {
	archive := tarGz(t, map[string][]byte{"README.md": make([]byte, 500), "LICENSE": make([]byte, 200)})
	if _, err := ExtractFor(linuxAMD64, "tool.tar.gz", archive, "tool"); !errors.Is(err, ErrNoBinary) {
		t.Fatalf("ExtractFor = %v, want ErrNoBinary", err)
	}
	if _, err := ExtractFor(linuxAMD64, "tool.tar.gz", tarGz(t, nil), "tool"); !errors.Is(err, ErrNoBinary) {
		t.Fatalf("empty archive = %v, want ErrNoBinary", err)
	}
	if _, err := ExtractFor(linuxAMD64, "tool.rar", nil, "tool"); err == nil {
		t.Fatal("unsupported archive type accepted")
	}
	if _, err := ExtractFor(linuxAMD64, "tool.tar.gz", []byte("not gzip"), "tool"); err == nil {
		t.Fatal("corrupt archive accepted")
	}
	binary := tarGz(t, map[string][]byte{"tool": executable(linuxAMD64, strings.Repeat("x", 4096))})
	if _, err := ExtractFor(linuxAMD64, "tool.tar.gz", binary[:len(binary)/2], "tool"); err == nil {
		t.Fatal("truncated archive accepted")
	}
}
