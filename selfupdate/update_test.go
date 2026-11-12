package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
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

func newFixture(t *testing.T) (*Updater, *memorySource, string) {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := tarGz(t, map[string][]byte{"tool": []byte("new binary"), "README.md": []byte("readme")})
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
	if got := contents(t, exe); got != "new binary" {
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
	if got := contents(t, exe); got != "new binary" {
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
	if got := contents(t, exe); got != "new binary" {
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
