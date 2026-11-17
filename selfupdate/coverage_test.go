package selfupdate

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"
)

// skipWithoutPermissions skips a test that needs a directory the user
// cannot write to.
func skipWithoutPermissions(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
}

func TestPrepareRefusesAnIncompleteUpdater(t *testing.T) {
	u, _, _ := newFixture(t)
	u.Source = nil
	if _, err := u.Update(context.Background(), false); err == nil || !strings.Contains(err.Error(), "Source is required") {
		t.Errorf("Update without Source = %v", err)
	}
	if _, err := u.Install(context.Background(), Release{Tag: "v1.1.0"}); err == nil || !strings.Contains(err.Error(), "Source is required") {
		t.Errorf("Install without Source = %v", err)
	}
	if _, _, err := u.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "Source is required") {
		t.Errorf("Check without Source = %v", err)
	}
}

func TestPrepareReportsAnExecutableThatCannotBeResolved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a path below a file is reported as missing on Windows")
	}
	u, source, exe := newFixture(t)
	u.Executable = filepath.Join(exe, "below-a-file")
	if _, err := u.Update(context.Background(), false); err == nil || !strings.Contains(err.Error(), "resolve") {
		t.Fatalf("Update = %v, want the failed resolution reported", err)
	}
	if source.calls != 0 {
		t.Fatal("asked the source before the executable was found")
	}
}

// Without Managed, the package managers' directories are judged; a
// temporary directory is none of them.
func TestPrepareJudgesByDefault(t *testing.T) {
	u, _, exe := newFixture(t)
	u.Managed = nil
	if result, err := u.Update(context.Background(), false); err != nil || !result.Updated {
		t.Fatalf("Update = %+v, %v", result, err)
	}
	if got := contents(t, exe); got != newBinary {
		t.Fatalf("executable holds %q", got)
	}
}

func TestPrepareRefusesADirectoryItCannotWrite(t *testing.T) {
	skipWithoutPermissions(t)
	u, source, exe := newFixture(t)
	dir := filepath.Dir(exe)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if _, err := u.Update(context.Background(), false); err == nil || !strings.Contains(err.Error(), "no write permission") {
		t.Fatalf("Update = %v, want the permission reported", err)
	}
	if source.calls != 0 {
		t.Fatal("asked the source before checking the directory")
	}
}

func TestPrepareReportsAMissingDirectory(t *testing.T) {
	u, _, exe := newFixture(t)
	u.Executable = filepath.Join(filepath.Dir(exe), "missing", "tool")
	if _, err := u.Update(context.Background(), false); err == nil || !strings.Contains(err.Error(), "cannot write to") {
		t.Fatalf("Update = %v, want the missing directory reported", err)
	}
}

// What cannot take the new binary's place is reported, and the update did
// not happen.
func TestInstallReportsAFailedReplacement(t *testing.T) {
	u, _, exe := newFixture(t)
	// A directory that is not empty takes no file's place.
	u.Executable = filepath.Join(filepath.Dir(exe), "tool-dir")
	u.Binary = "tool"
	if err := os.MkdirAll(filepath.Join(u.Executable, "inner"), 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := u.Update(context.Background(), false)
	if err == nil || result.Updated {
		t.Fatalf("Update = %+v, %v, want a failed replacement", result, err)
	}
	if !strings.Contains(err.Error(), "replace") {
		t.Fatalf("Update = %v, want the failed replacement reported", err)
	}
}

func TestUpdaterDefaults(t *testing.T) {
	if got := (&Updater{}).platform(); got != CurrentPlatform() {
		t.Errorf("platform = %+v, want the current one", got)
	}
	u := &Updater{Checksums: "SHA256SUMS"}
	if got := u.checksums(Release{Tag: "v1.0.0"}); got != "SHA256SUMS" {
		t.Errorf("checksums = %q", got)
	}
	// A ChecksumsFor that names nothing falls back to Checksums.
	u.ChecksumsFor = func(Release) string { return "" }
	if got := u.checksums(Release{Tag: "v1.0.0"}); got != "SHA256SUMS" {
		t.Errorf("checksums with an empty ChecksumsFor = %q", got)
	}
	if got := (&Updater{}).AssetName(Release{}); got != "" {
		t.Errorf("AssetName without Asset = %q", got)
	}
}

// failingSource fails reading every download after its first bytes.
type failingSource struct{ memorySource }

var errCut = errors.New("connection reset")

func (f *failingSource) Download(context.Context, Release, string) (io.ReadCloser, error) {
	return io.NopCloser(io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(errCut))), nil
}

func TestUpdateReportsAFailedDownload(t *testing.T) {
	u, source, exe := newFixture(t)
	u.Source = &failingSource{memorySource: *source}
	if _, err := u.Update(context.Background(), false); !errors.Is(err, errCut) || !strings.Contains(err.Error(), "download") {
		t.Fatalf("Update = %v, want the download error", err)
	}
	if got := contents(t, exe); got != "old binary" {
		t.Fatalf("executable replaced: %q", got)
	}
}

func TestReplaceExecutableInAMissingDirectory(t *testing.T) {
	err := ReplaceExecutable(filepath.Join(t.TempDir(), "missing", "tool"), []byte("new"))
	if err == nil || !strings.Contains(err.Error(), "stage new binary") {
		t.Fatalf("ReplaceExecutable = %v", err)
	}
	if err := ReplaceExecutable(filepath.Join(t.TempDir(), "tool"), nil); !errors.Is(err, ErrNoBinary) {
		t.Fatalf("ReplaceExecutable of nothing = %v, want ErrNoBinary", err)
	}
}

// A staged file that cannot be moved in leaves the old executable where it
// was.
func TestSwapAsideMovesBackWhenTheStagedFileIsGone(t *testing.T) {
	path := filepath.Join(tempDir(t), "tool")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := swapAside(path, filepath.Join(filepath.Dir(path), "gone"))
	if err == nil || !strings.Contains(err.Error(), "replace") {
		t.Fatalf("swapAside = %v", err)
	}
	if got := contents(t, path); got != "old" {
		t.Fatalf("executable holds %q", got)
	}
}

func TestSwapAsideReportsAnExecutableThatCannotMove(t *testing.T) {
	skipWithoutPermissions(t)
	dir := tempDir(t)
	path := filepath.Join(dir, "tool")
	if err := os.WriteFile(path, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	staged := stage(t, path, "new")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := swapAside(path, staged); err == nil || !strings.Contains(err.Error(), "move") {
		t.Fatalf("swapAside = %v, want the failed move reported", err)
	}
}

func TestCutDescribeEdgeCases(t *testing.T) {
	for _, tc := range []struct {
		in    string
		tag   string
		ahead int
	}{
		{"release-gabc1234", "release-gabc1234", 0},
		{"v1.0.0-gabc1234", "v1.0.0-gabc1234", 0},
		{"gabc1234-gabc1234", "gabc1234-gabc1234", 0},
		{"v1.0.0-99999999999999999999-gabc1234", "v1.0.0-99999999999999999999-gabc1234", 0},
		{"v1.0.0-3-gabc1234-dirty", "v1.0.0", 3},
	} {
		tag, ahead, _ := cutDescribe(tc.in)
		if tag != tc.tag || ahead != tc.ahead {
			t.Errorf("cutDescribe(%q) = %q, %d; want %q, %d", tc.in, tag, ahead, tc.tag, tc.ahead)
		}
	}
}

func TestVerifyChecksumRefusesAnOverlongLine(t *testing.T) {
	checksums := []byte(strings.Repeat("a", 70_000) + "  tool.tar.gz\n")
	if err := VerifyChecksum(checksums, "tool.tar.gz", []byte("x")); err == nil || !strings.Contains(err.Error(), "read checksums") {
		t.Fatalf("VerifyChecksum = %v", err)
	}
}

// Without Executable, the running executable is the one to replace, with
// symbolic links resolved.
func TestExecutableDefaultsToTheRunningOne(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	want, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Skip(err)
	}
	if got, err := (&Updater{}).executable(); err != nil || got != want {
		t.Errorf("executable = %q, %v; want %q", got, err, want)
	}
}

func TestCredentialWithoutATokenFunc(t *testing.T) {
	if token, err := (&requester{}).credential(context.Background(), true); token != "" || err != nil {
		t.Errorf("credential = %q, %v", token, err)
	}
}

func TestTitleOSOfNothing(t *testing.T) {
	if got := titleOS(""); got != "" {
		t.Errorf("titleOS(\"\") = %q", got)
	}
}

func TestExtractWithoutABinaryName(t *testing.T) {
	archive := tarGz(t, map[string][]byte{"tool": executable(linuxAMD64, "binary"+strings.Repeat("x", MinBinarySize)), "README": []byte("r")})
	got, err := ExtractFor(linuxAMD64, "tool.tar.gz", archive, "")
	if err != nil || !bytes.Contains(got, []byte("binary")) {
		t.Fatalf("ExtractFor = %q, %v; want the largest file", got, err)
	}
}

func TestExtractRefusesALargestFileAboveTheLimit(t *testing.T) {
	archive := tarGz(t, map[string][]byte{"big": executable(linuxAMD64, strings.Repeat("x", 100)), "README": []byte("r")})
	_, err := extract(linuxAMD64, "tool.tar.gz", archive, "tool", archiveLimits{entry: 50, total: 1 << 20, entries: 10})
	if err == nil || !strings.Contains(err.Error(), "big is larger than 50 bytes") {
		t.Fatalf("extract = %v", err)
	}
}

func TestExtractBoundsTheUnpackedTotal(t *testing.T) {
	archive := tarGz(t, map[string][]byte{"tool": executable(linuxAMD64, strings.Repeat("x", 4000))})
	_, err := extract(linuxAMD64, "tool.tar.gz", archive, "tool", archiveLimits{entry: 1 << 20, total: 1000, entries: 10})
	if err == nil || !strings.Contains(err.Error(), "1000 bytes") {
		t.Fatalf("extract = %v, want the total bounded", err)
	}
}

// The error lists the first files of an archive without the binary, and
// counts the rest.
func TestExtractListsWhatTheArchiveHolds(t *testing.T) {
	files := map[string][]byte{}
	for i := range 12 {
		files[fmt.Sprintf("doc%02d.txt", i)] = []byte("small")
	}
	_, err := ExtractFor(linuxAMD64, "tool.tar.gz", tarGz(t, files), "tool")
	if !errors.Is(err, ErrNoBinary) || !strings.Contains(err.Error(), "and 2 more") {
		t.Fatalf("ExtractFor = %v", err)
	}
}

func TestCappedReader(t *testing.T) {
	// An archive that ends right at the limit is complete.
	c := &cappedReader{r: strings.NewReader("abc"), limit: 3, left: 3}
	if data, err := io.ReadAll(c); err != nil || string(data) != "abc" {
		t.Errorf("ReadAll at the limit = %q, %v", data, err)
	}
	c = &cappedReader{r: strings.NewReader("abcd"), limit: 3, left: 3}
	if _, err := io.ReadAll(c); err == nil {
		t.Error("ReadAll beyond the limit succeeded")
	}
}

func TestExtractRefusesABrokenZip(t *testing.T) {
	if _, err := Extract("tool.zip", []byte("not a zip archive"), "tool"); err == nil || !strings.Contains(err.Error(), "read zip archive") {
		t.Errorf("Extract = %v", err)
	}

	// An entry compressed in a way the reader does not know.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	content := executable(linuxAMD64, "binary")
	w, err := zw.CreateRaw(&zip.FileHeader{Name: "tool", Method: 99, CRC32: crc32.ChecksumIEEE(content),
		CompressedSize64: uint64(len(content)), UncompressedSize64: uint64(len(content))})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtractFor(linuxAMD64, "tool.zip", buf.Bytes(), "tool"); err == nil || !strings.Contains(err.Error(), "read zip entry") {
		t.Errorf("ExtractFor of an unknown compression = %v", err)
	}

	// An entry whose content does not match its checksum.
	buf.Reset()
	zw = zip.NewWriter(&buf)
	w, err = zw.CreateHeader(&zip.FileHeader{Name: "tool", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	broken := bytes.Replace(buf.Bytes(), []byte("binary"), []byte("binarz"), 1)
	if _, err := ExtractFor(linuxAMD64, "tool.zip", broken, "tool"); !errors.Is(err, zip.ErrChecksum) {
		t.Errorf("ExtractFor of a corrupted entry = %v, want zip.ErrChecksum", err)
	}
}

func TestWithinOrigin(t *testing.T) {
	r := &requester{origin: "https://gitlab.example.com/api/v4"}
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"https://gitlab.example.com:443/api/v4/projects", true},
		{"https://GITLAB.example.com/x", true},
		{"http://gitlab.example.com/x", false},
		{"https://storage.example.com/x", false},
		{"https://gitlab.example.com:8443/x", false},
		{"https://[::1", false},
	} {
		if got := r.withinOrigin(tc.url); got != tc.want {
			t.Errorf("withinOrigin(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
	if !(&requester{}).withinOrigin("https://anywhere.example.com") {
		t.Error("a requester without an origin refused a URL")
	}
	if (&requester{origin: "https://[::1"}).withinOrigin("https://example.com") {
		t.Error("a malformed origin matched")
	}
}

func TestHostPortOfAnotherScheme(t *testing.T) {
	u, err := url.Parse("ftp://example.com/x")
	if err != nil {
		t.Fatal(err)
	}
	if got := hostPort(u); got != "example.com:" {
		t.Errorf("hostPort = %q", got)
	}
}

func TestRequestErrors(t *testing.T) {
	errNoToken := errors.New("no token")
	r := &requester{token: func(context.Context, bool) (string, error) { return "", errNoToken }, authHeader: "PRIVATE-TOKEN"}
	if _, err := r.get(context.Background(), "https://example.com/x", "", ErrAssetNotFound); !errors.Is(err, errNoToken) {
		t.Errorf("get with a failing TokenFunc = %v", err)
	}
	// A URL no request can be made of, with the default client and with
	// the caller's.
	for _, client := range []*http.Client{nil, http.DefaultClient} {
		r := &requester{client: client}
		if _, err := r.get(context.Background(), "https://[::1", "", ErrAssetNotFound); err == nil || !strings.HasPrefix(err.Error(), "selfupdate:") {
			t.Errorf("client %v: get = %v", client, err)
		}
	}
}

// A TokenFunc that fails when asked again after a rejection ends the
// request with its error.
func TestRetryWithAFailingTokenFunc(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()
	errGone := errors.New("credential store gone")
	g := &GitLab{BaseURL: srv.URL, Project: "example/tool", Token: func(_ context.Context, retry bool) (string, error) {
		if retry {
			return "", errGone
		}
		return "tok", nil
	}}
	_, err := g.Latest(context.Background())
	if !errors.Is(err, errGone) {
		t.Fatalf("Latest = %v, want the TokenFunc's error", err)
	}
}

// A server that redirects without end is given up on.
func TestRedirectLoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/again", http.StatusFound)
	}))
	defer srv.Close()
	g := &GitLab{BaseURL: srv.URL, Project: "example/tool", Token: func(context.Context, bool) (string, error) { return "tok", nil }}
	if _, err := g.Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "10 redirects") {
		t.Fatalf("Latest = %v, want the redirects bounded", err)
	}
}

func TestSourcesRefuseMalformedAnswers(t *testing.T) {
	var answer string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	defer srv.Close()
	ctx := context.Background()
	gh := &GitHub{Owner: "example", Repo: "tool", APIURL: srv.URL}
	gl := &GitLab{BaseURL: srv.URL, Project: "example%2Ftool", Package: "tool"}

	answer = "{not json"
	if _, err := gh.Latest(ctx); err == nil || !strings.Contains(err.Error(), "decode latest release") {
		t.Errorf("GitHub.Latest of bad JSON = %v", err)
	}
	if _, err := gl.Latest(ctx); err == nil || !strings.Contains(err.Error(), "decode releases") {
		t.Errorf("GitLab.Latest of bad JSON = %v", err)
	}
	answer = `{"tag_name":""}`
	if _, err := gh.Latest(ctx); !errors.Is(err, ErrNoRelease) {
		t.Errorf("GitHub.Latest without a tag = %v, want ErrNoRelease", err)
	}

	status = http.StatusInternalServerError
	if _, err := gl.Download(ctx, Release{Tag: "v1.0.0"}, "tool.tar.gz"); err == nil || errors.Is(err, ErrAssetNotFound) {
		t.Errorf("GitLab.Download from a failing server = %v, want the failure, not a missing asset", err)
	}
}

func TestGitLabDownloadNeedsItsSettings(t *testing.T) {
	ctx := context.Background()
	if _, err := (&GitLab{}).Download(ctx, Release{Tag: "v1"}, "x"); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("Download without BaseURL = %v", err)
	}
	g := &GitLab{BaseURL: "https://gitlab.example.com", Project: "example/tool"}
	if _, err := g.Download(ctx, Release{Tag: "v1"}, "x"); err == nil || !strings.Contains(err.Error(), "Package is required") {
		t.Errorf("Download without Package = %v", err)
	}
}

// A project path that is already escaped is used as it is.
func TestGitLabProjectAlreadyEscaped(t *testing.T) {
	for _, tc := range []struct{ project, want string }{
		{"example%2Ftool", "example%2Ftool"},
		{"/example/tool/", "example%2Ftool"},
		{"42", "42"},
	} {
		if got := (&GitLab{Project: tc.project}).project(); got != tc.want {
			t.Errorf("project(%q) = %q, want %q", tc.project, got, tc.want)
		}
	}
}

// A retry that fails to reach the server reports that failure.
func TestRetryThatFailsToConnect(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		conn, _, err := http.NewResponseController(w).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer srv.Close()
	g := &GitLab{BaseURL: srv.URL, Project: "example/tool", Token: func(context.Context, bool) (string, error) { return "tok", nil }}
	_, err := g.Latest(context.Background())
	// The HTTP client may itself try a GET again on a closed connection.
	if err == nil || errors.Is(err, ErrUnauthorized) || requests.Load() < 2 {
		t.Fatalf("Latest = %v after %d requests, want the failed retry reported", err, requests.Load())
	}
}
