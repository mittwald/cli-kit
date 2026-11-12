package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// memorySource serves one release from memory and counts calls.
type memorySource struct {
	release   Release
	files     map[string][]byte
	latestErr error
	calls     int
}

func (m *memorySource) Latest(context.Context) (Release, error) {
	m.calls++
	return m.release, m.latestErr
}

func (m *memorySource) Download(_ context.Context, _ Release, name string) (io.ReadCloser, error) {
	data, ok := m.files[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrAssetNotFound, name)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

var linuxAMD64 = Platform{OS: "linux", Arch: "amd64"}

// newBinary is the binary of the fixture's release: an ELF file, as the
// fixture updates for Linux.
const newBinary = "\x7fELF new binary"

// tempDir returns a new directory by its real path, as the Updater reports
// the executable with symbolic links resolved and macOS keeps its
// temporary directory behind one.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func newFixture(t *testing.T) (*Updater, *memorySource, string) {
	t.Helper()
	exe := filepath.Join(tempDir(t), "tool")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := tarGz(t, map[string][]byte{"tool": []byte(newBinary), "README.md": []byte("readme")})
	source := &memorySource{
		release: Release{Tag: "v1.1.0"},
		files: map[string][]byte{
			"tool_Linux_x86_64.tar.gz": archive,
			"checksums.txt":            checksumsFor(map[string][]byte{"tool_Linux_x86_64.tar.gz": archive}),
		},
	}
	u := &Updater{
		Source:     source,
		Current:    "v1.0.0",
		Asset:      TitleCaseAssets("tool"),
		Platform:   linuxAMD64,
		Executable: exe,
		Managed:    func(string) bool { return false },
	}
	return u, source, exe
}

func contents(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestUpdateInstallsANewerRelease(t *testing.T) {
	u, _, exe := newFixture(t)
	result, err := u.Update(context.Background(), false)
	if err != nil {
		t.Fatalf("Update = %v", err)
	}
	if !result.Updated || result.Release.Tag != "v1.1.0" || result.Asset != "tool_Linux_x86_64.tar.gz" ||
		result.From != "v1.0.0" || result.Path != exe {
		t.Fatalf("Result = %+v", result)
	}
	if got := contents(t, exe); got != newBinary {
		t.Fatalf("executable holds %q", got)
	}
}

func TestUpdateSkipsWhenCurrent(t *testing.T) {
	u, _, exe := newFixture(t)
	u.Current = "v1.1.0"
	result, err := u.Update(context.Background(), false)
	if err != nil || result.Updated {
		t.Fatalf("Update = %+v, %v; want no update", result, err)
	}
	if got := contents(t, exe); got != "old binary" {
		t.Fatalf("executable holds %q", got)
	}
	if result, err = u.Update(context.Background(), true); err != nil || !result.Updated {
		t.Fatalf("forced Update = %+v, %v", result, err)
	}
}

func TestUpdateRefusesATamperedArchive(t *testing.T) {
	u, source, exe := newFixture(t)
	source.files["checksums.txt"] = checksumsFor(map[string][]byte{"tool_Linux_x86_64.tar.gz": []byte("other")})
	if _, err := u.Update(context.Background(), false); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("Update = %v, want ErrChecksumMismatch", err)
	}
	if got := contents(t, exe); got != "old binary" {
		t.Fatalf("executable replaced despite the mismatch: %q", got)
	}
}

func TestUpdateRequiresChecksumsUnlessSkipped(t *testing.T) {
	u, source, exe := newFixture(t)
	delete(source.files, "checksums.txt")
	if _, err := u.Update(context.Background(), false); !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("Update = %v, want the missing checksums reported", err)
	}
	if got := contents(t, exe); got != "old binary" {
		t.Fatalf("executable replaced without verification: %q", got)
	}
	u.SkipChecksum = true
	if result, err := u.Update(context.Background(), false); err != nil || !result.Updated {
		t.Fatalf("Update with SkipChecksum = %+v, %v", result, err)
	}
}

func TestUpdateRefusesManagedBinaryBeforeDownloading(t *testing.T) {
	u, source, exe := newFixture(t)
	u.Managed = func(path string) bool { return path == exe }
	if _, err := u.Update(context.Background(), true); !errors.Is(err, ErrManaged) {
		t.Fatalf("Update = %v, want ErrManaged", err)
	}
	if source.calls != 0 {
		t.Fatal("Update asked the source although the binary is managed")
	}
}

func TestUpdateFromDevBuild(t *testing.T) {
	u, _, _ := newFixture(t)
	u.Current = "dev"
	if result, err := u.Update(context.Background(), false); err != nil || !result.Updated {
		t.Fatalf("Update = %+v, %v", result, err)
	}
}

func TestUpdateReportsSourceErrors(t *testing.T) {
	u, source, _ := newFixture(t)
	source.latestErr = ErrNoRelease
	if _, err := u.Update(context.Background(), false); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("Update = %v, want ErrNoRelease", err)
	}
	if _, err := (&Updater{}).Update(context.Background(), false); err == nil {
		t.Fatal("Update without Asset succeeded")
	}
}

func TestUpdateBoundsTheDownload(t *testing.T) {
	u, _, exe := newFixture(t)
	u.MaxDownload = 10
	if _, err := u.Update(context.Background(), false); err == nil {
		t.Fatal("Update accepted an archive above MaxDownload")
	}
	if got := contents(t, exe); got != "old binary" {
		t.Fatalf("executable holds %q", got)
	}
}

// A MaxDownload as large as an int64 gets is a limit like any other.
func TestUpdateAcceptsTheLargestMaxDownload(t *testing.T) {
	u, _, exe := newFixture(t)
	u.MaxDownload = math.MaxInt64
	if result, err := u.Update(context.Background(), false); err != nil || !result.Updated {
		t.Fatalf("Update = %+v, %v", result, err)
	}
	if got := contents(t, exe); got != newBinary {
		t.Fatalf("executable holds %q", got)
	}
}

func TestCheck(t *testing.T) {
	u, _, _ := newFixture(t)
	rel, newer, err := u.Check(context.Background())
	if err != nil || !newer || rel.Tag != "v1.1.0" {
		t.Fatalf("Check = %+v, %v, %v", rel, newer, err)
	}
	if _, _, err := (&Updater{}).Check(context.Background()); err == nil {
		t.Fatal("Check without Source succeeded")
	}
}

// A release whose tag is no version cannot be compared: Check and Update
// say so instead of reporting the running version as current, and Install
// still installs it.
func TestCheckRejectsATagThatIsNoVersion(t *testing.T) {
	u, source, exe := newFixture(t)
	source.release.Tag = "release-2\x1b[31m"
	rel, newer, err := u.Check(context.Background())
	if !errors.Is(err, ErrNoVersion) || newer || rel.Tag != source.release.Tag {
		t.Fatalf("Check = %+v, %v, %v; want the release and ErrNoVersion", rel, newer, err)
	}
	if strings.Contains(err.Error(), "\x1b") {
		t.Fatalf("error carries the tag's control characters: %q", err.Error())
	}
	for _, force := range []bool{false, true} {
		if result, err := u.Update(context.Background(), force); !errors.Is(err, ErrNoVersion) || result.Updated {
			t.Fatalf("Update(force=%v) = %+v, %v; want ErrNoVersion", force, result, err)
		}
	}
	if got := contents(t, exe); got != "old binary" {
		t.Fatalf("executable holds %q", got)
	}
	if result, err := u.Install(context.Background(), rel); err != nil || !result.Updated {
		t.Fatalf("Install = %+v, %v", result, err)
	}
}

// A build of a commit after the latest release is not replaced by that
// release, and is by the next one.
func TestUpdateKeepsABuildAheadOfTheRelease(t *testing.T) {
	for _, current := range []string{"v1.1.0-5-gabc1234", "v1.1.0-5-gabc1234-dirty", "v1.1.0-dirty"} {
		u, source, exe := newFixture(t)
		u.Current = current
		if result, err := u.Update(context.Background(), false); err != nil || result.Updated {
			t.Fatalf("%s: Update = %+v, %v; want no update to v1.1.0", current, result, err)
		}
		if got := contents(t, exe); got != "old binary" {
			t.Fatalf("%s: executable holds %q", current, got)
		}
		source.release.Tag = "v1.1.1"
		if result, err := u.Update(context.Background(), false); err != nil || !result.Updated {
			t.Fatalf("%s: Update to v1.1.1 = %+v, %v", current, result, err)
		}
	}
}

// A file of the binary's name that is no executable is never installed.
func TestUpdateInstallsOnlyAnExecutable(t *testing.T) {
	u, source, exe := newFixture(t)
	script := tarGz(t, map[string][]byte{"tool": []byte("#!/bin/sh\necho not the binary\n")})
	source.files["tool_Linux_x86_64.tar.gz"] = script
	source.files["checksums.txt"] = checksumsFor(map[string][]byte{"tool_Linux_x86_64.tar.gz": script})
	if result, err := u.Update(context.Background(), false); !errors.Is(err, ErrNoBinary) || result.Updated {
		t.Fatalf("Update = %+v, %v; want ErrNoBinary", result, err)
	}
	if got := contents(t, exe); got != "old binary" {
		t.Fatalf("executable replaced by a script: %q", got)
	}

	// The completion script of the binary's name comes first in the
	// archive; the binary is installed all the same.
	mixed := tarGzOrdered(t,
		archiveFile{name: "tool_1.1.0/completions/bash/tool", data: []byte("complete -F _tool tool\n")},
		archiveFile{name: "tool_1.1.0/tool", data: []byte(newBinary)},
	)
	source.files["tool_Linux_x86_64.tar.gz"] = mixed
	source.files["checksums.txt"] = checksumsFor(map[string][]byte{"tool_Linux_x86_64.tar.gz": mixed})
	if result, err := u.Update(context.Background(), false); err != nil || !result.Updated {
		t.Fatalf("Update = %+v, %v", result, err)
	}
	if got := contents(t, exe); got != newBinary {
		t.Fatalf("executable holds %q", got)
	}

	// The binary is checked against the platform updated for.
	u.Platform = Platform{OS: "darwin", Arch: "amd64"}
	source.files["tool_Darwin_x86_64.tar.gz"] = mixed
	source.files["checksums.txt"] = checksumsFor(map[string][]byte{"tool_Darwin_x86_64.tar.gz": mixed})
	if _, err := u.Update(context.Background(), true); !errors.Is(err, ErrNoBinary) {
		t.Fatalf("Update for macOS with an ELF binary = %v, want ErrNoBinary", err)
	}
}

// A file the user keeps beside the executable is not the Updater's to
// delete, whatever it is called, and nothing at all is deleted beside an
// executable a package manager owns.
func TestUpdateLeavesFilesBesideTheExecutableAlone(t *testing.T) {
	u, _, exe := newFixture(t)
	backup := exe + ".old"
	staged := filepath.Join(filepath.Dir(exe), stagedPrefix(exe)+"123")
	longAgo := time.Now().Add(-2 * staleAfter)
	for _, path := range []string{backup, staged} {
		if err := os.WriteFile(path, []byte("kept"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, longAgo, longAgo); err != nil {
			t.Fatal(err)
		}
	}

	u.Managed = func(string) bool { return true }
	if _, err := u.Update(context.Background(), false); !errors.Is(err, ErrManaged) {
		t.Fatalf("Update = %v, want ErrManaged", err)
	}
	for _, path := range []string{backup, staged} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("managed executable: %s was removed: %v", filepath.Base(path), err)
		}
	}

	u.Managed = func(string) bool { return false }
	u.Current = "v1.1.0" // already current
	if result, err := u.Update(context.Background(), false); err != nil || result.Updated {
		t.Fatalf("Update = %+v, %v; want no update", result, err)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Errorf("the stale staged binary of an interrupted update was kept: %v", err)
	}
	if runtime.GOOS != "windows" {
		if got := contents(t, backup); got != "kept" {
			t.Errorf("tool.old holds %q", got)
		}
	}
}

// The file a symbolic link leads to is the executable: it is what Managed
// judges and what is replaced, and the link stays a link.
func TestUpdateResolvesASymlinkedExecutable(t *testing.T) {
	u, _, exe := newFixture(t)
	link := filepath.Join(tempDir(t), "tool")
	if err := os.Symlink(exe, link); err != nil {
		t.Skipf("cannot create a symbolic link: %v", err)
	}
	u.Executable = link
	var judged string
	u.Managed = func(path string) bool { judged = path; return false }
	result, err := u.Update(context.Background(), false)
	if err != nil || !result.Updated || result.Path != exe {
		t.Fatalf("Update = %+v, %v; want %s replaced", result, err, exe)
	}
	if judged != exe {
		t.Errorf("Managed was asked about %s, want the link's target %s", judged, exe)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced: %v, %v", info, err)
	}
	if got := contents(t, exe); got != newBinary {
		t.Errorf("the link's target holds %q", got)
	}

	// A path nothing is at yet is installed to as it is.
	fresh := filepath.Join(tempDir(t), "tool")
	u.Executable = fresh
	if result, err = u.Update(context.Background(), true); err != nil || result.Path != fresh {
		t.Fatalf("Update to a new path = %+v, %v", result, err)
	}
	if got := contents(t, fresh); got != newBinary {
		t.Errorf("new path holds %q", got)
	}
}

// Install installs the release it is given, without asking the source for
// the latest one again, whatever version is running.
func TestInstallInstallsTheGivenRelease(t *testing.T) {
	u, source, exe := newFixture(t)
	u.Current = "v1.1.0" // Install does not compare
	rel := Release{Tag: "v1.1.0"}
	if got := u.AssetName(rel); got != "tool_Linux_x86_64.tar.gz" {
		t.Errorf("AssetName = %q", got)
	}
	result, err := u.Install(context.Background(), rel)
	if err != nil {
		t.Fatalf("Install = %v", err)
	}
	if !result.Updated || result.Release.Tag != "v1.1.0" || result.Asset != "tool_Linux_x86_64.tar.gz" ||
		result.From != "v1.1.0" || result.Path != exe {
		t.Fatalf("Result = %+v", result)
	}
	if source.calls != 0 {
		t.Errorf("Install asked the source for the latest release %d times", source.calls)
	}
	if got := contents(t, exe); got != newBinary {
		t.Fatalf("executable holds %q", got)
	}
}

func TestInstallRefusesManagedBinaryAndTamperedArchive(t *testing.T) {
	u, source, exe := newFixture(t)
	u.Managed = func(path string) bool { return path == exe }
	if _, err := u.Install(context.Background(), Release{Tag: "v1.1.0"}); !errors.Is(err, ErrManaged) {
		t.Fatalf("Install = %v, want ErrManaged", err)
	}
	u.Managed = func(string) bool { return false }
	source.files["checksums.txt"] = checksumsFor(map[string][]byte{"tool_Linux_x86_64.tar.gz": []byte("other")})
	if _, err := u.Install(context.Background(), Release{Tag: "v1.1.0"}); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("Install = %v, want ErrChecksumMismatch", err)
	}
	if got := contents(t, exe); got != "old binary" {
		t.Fatalf("executable replaced: %q", got)
	}
	if _, err := (&Updater{}).Install(context.Background(), Release{Tag: "v1.1.0"}); err == nil {
		t.Fatal("Install without Asset and Source succeeded")
	}
}

// A checksums file named after the release is fetched by that name.
func TestChecksumsFor(t *testing.T) {
	u, source, exe := newFixture(t)
	source.files["tool_1.1.0_checksums.txt"] = source.files["checksums.txt"]
	delete(source.files, "checksums.txt")
	u.Checksums = "ignored.txt"
	u.ChecksumsFor = VersionedChecksums("tool")
	if result, err := u.Update(context.Background(), false); err != nil || !result.Updated {
		t.Fatalf("Update = %+v, %v", result, err)
	}
	if got := contents(t, exe); got != newBinary {
		t.Fatalf("executable holds %q", got)
	}
}

func TestVersionedChecksums(t *testing.T) {
	name := VersionedChecksums("software-ctl")
	for tag, want := range map[string]string{
		"v1.1.48":       "software-ctl_1.1.48_checksums.txt",
		"1.2.0":         "software-ctl_1.2.0_checksums.txt",
		"v2.0.0-beta.1": "software-ctl_2.0.0-beta.1_checksums.txt",
	} {
		if got := name(Release{Tag: tag}); got != want {
			t.Errorf("VersionedChecksums(%s) = %q, want %q", tag, got, want)
		}
	}
	if got := (&Updater{}).AssetName(Release{Tag: "v1.0.0"}); got != "" {
		t.Errorf("AssetName without Asset = %q, want empty", got)
	}
}
