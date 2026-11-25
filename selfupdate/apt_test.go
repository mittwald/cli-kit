package selfupdate

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeDpkg plays dpkg-query, apt-get and apt-cache for an [APT]: one
// package owns one file, the repository offers some versions of it, and an
// install of a downloaded file changes the installed version.
type fakeDpkg struct {
	file      string
	pkg       string
	version   string   // installed
	offered   []string // in the repository, the last one is the candidate
	search    string   // what dpkg-query --search prints; empty means file's owner
	searchErr error
	fetchErr  error
	// unreachable makes apt-get update fetch nothing yet succeed, as it
	// does when it cannot reach the repository.
	unreachable bool
	runErr      error
	queried     [][]string
	ran         [][]string
	dirs        []string
}

func (f *fakeDpkg) output(_ context.Context, dir string, argv []string) ([]byte, error) {
	f.queried = append(f.queried, argv)
	switch {
	case argv[0] == dpkgQuery && slices.Contains(argv, "--search"):
		if f.searchErr != nil {
			return nil, f.searchErr
		}
		if f.search != "" {
			return []byte(f.search), nil
		}
		if argv[len(argv)-1] != f.file {
			return nil, errors.New("exit status 1")
		}
		return []byte(f.pkg + ": " + f.file + "\n"), nil
	case argv[0] == dpkgQuery && slices.Contains(argv, "--show"):
		return []byte(f.version), nil
	case argv[0] == aptGet && slices.Contains(argv, "update"):
		f.dirs = append(f.dirs, dir)
		if f.fetchErr != nil || f.unreachable {
			return nil, f.fetchErr
		}
		return nil, os.WriteFile(filepath.Join(dir, "lists", "example.org_dists_stable_main_binary-amd64_Packages"), nil, 0o644)
	case argv[0] == aptCache && slices.Contains(argv, "madison"):
		var out strings.Builder
		for _, v := range slices.Backward(f.offered) {
			out.WriteString(f.pkg + " | " + v + " | https://example.org stable/main amd64 Packages\n")
		}
		return []byte(out.String()), nil
	case argv[0] == aptCache && slices.Contains(argv, "policy"):
		// As APT: the installed version counts unless the repository has
		// a newer one.
		candidate := f.version
		if len(f.offered) > 0 && f.offered[len(f.offered)-1] > f.version {
			candidate = f.offered[len(f.offered)-1]
		}
		return []byte(f.pkg + ":\n  Installed: " + f.version + "\n  Candidate: " + candidate + "\n  Version table:\n"), nil
	case argv[0] == aptGet && slices.Contains(argv, "download"):
		_, version, _ := strings.Cut(argv[len(argv)-1], "=")
		if !slices.Contains(f.offered, version) {
			return nil, errors.New("exit status 100: E: Version '" + version + "' was not found")
		}
		name := strings.ReplaceAll(f.pkg, ":", "_") + "_" + strings.ReplaceAll(version, ":", "%3a") + "_amd64.deb"
		return nil, os.WriteFile(filepath.Join(dir, name), []byte("!<arch>\n"), 0o644)
	}
	return nil, errors.New("unexpected query")
}

func (f *fakeDpkg) run(_ context.Context, argv []string, _ io.Reader, _, _ io.Writer) error {
	f.ran = append(f.ran, argv)
	if f.runErr != nil {
		return f.runErr
	}
	if argv[len(argv)-1] == "update" {
		return nil
	}
	file := argv[len(argv)-1]
	if _, err := os.Stat(file); err != nil {
		return err
	}
	parts := strings.Split(filepath.Base(file), "_")
	f.version = strings.ReplaceAll(parts[len(parts)-2], "%3a", ":")
	return nil
}

func newAPTUpdater(t *testing.T, dpkg *fakeDpkg, euid int) (*Updater, *APT) {
	t.Helper()
	sources := filepath.Join(t.TempDir(), "example.sources")
	if err := os.WriteFile(sources, []byte("Types: deb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	apt := &APT{
		Sources: sources,
		goos:    "linux",
		euid:    func() int { return euid },
		output:  dpkg.output,
		run:     dpkg.run,
	}
	u := &Updater{
		// The repository is the source of releases; this one fails when
		// it is asked.
		Source:     &memorySource{latestErr: errors.New("source asked")},
		Current:    "v1.2.0",
		Asset:      DefaultAssets("example"),
		Executable: dpkg.file,
		Managed:    func(string) bool { return true },
		APT:        apt,
	}
	return u, apt
}

func newFakeDpkg() *fakeDpkg {
	return &fakeDpkg{file: "/usr/bin/example", pkg: "example", version: "1.2.0", offered: []string{"1.2.0", "1.3.0"}}
}

func TestAPTUpdateAsUser(t *testing.T) {
	dpkg := newFakeDpkg()
	u, apt := newAPTUpdater(t, dpkg, 1000)
	var shown APTPlan
	apt.Confirm = func(_ context.Context, plan APTPlan) (bool, error) {
		shown = plan
		return true, nil
	}
	res, err := u.Update(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Updated || res.Package != "example" || res.Asset != "" || res.Release.Tag != "1.3.0" {
		t.Fatalf("result = %+v", res)
	}
	if shown.Package != "example" || shown.Installed != "1.2.0" || shown.Version != "1.3.0" || !shown.Sudo {
		t.Fatalf("plan = %+v", shown)
	}
	if !slices.EqualFunc(dpkg.ran, shown.Commands, slices.Equal) {
		t.Fatalf("ran %q, showed %q", dpkg.ran, shown.Commands)
	}
	want := "/usr/bin/sudo -- /usr/bin/apt-get update\n" +
		"/usr/bin/sudo -- /usr/bin/apt-get install --yes --allow-downgrades -- " + shellQuote(shown.File)
	if got := shown.String(); got != want {
		t.Fatalf("plan:\n%s\nwant:\n%s", got, want)
	}
	// The index was fetched without sudo into a directory of its own,
	// which is gone with the downloaded file.
	for _, argv := range dpkg.queried {
		if argv[0] == sudo {
			t.Fatalf("queried %q", argv)
		}
		if argv[0] == aptGet && !slices.Contains(argv, "Dir::State::Lists="+filepath.Join(dpkg.dirs[0], "lists")) {
			t.Fatalf("queried %q without its own lists", argv)
		}
		if slices.Contains(argv, "APT::Sandbox::User=root") {
			t.Fatalf("queried %q as user", argv)
		}
	}
	if _, err := os.Stat(dpkg.dirs[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("index directory left behind: %v", err)
	}
}

func TestAPTUpdateAsRoot(t *testing.T) {
	dpkg := newFakeDpkg()
	u, _ := newAPTUpdater(t, dpkg, 0)
	if _, err := u.Update(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if len(dpkg.ran) != 2 || dpkg.ran[0][0] != aptGet || dpkg.ran[1][0] != aptGet {
		t.Fatalf("ran %q", dpkg.ran)
	}
	for _, argv := range dpkg.queried {
		if argv[0] == aptGet && !slices.Contains(argv, "APT::Sandbox::User=root") {
			t.Fatalf("queried %q as root", argv)
		}
	}
}

func TestAPTCheck(t *testing.T) {
	dpkg := newFakeDpkg()
	u, _ := newAPTUpdater(t, dpkg, 1000)
	rel, newer, err := u.Check(context.Background())
	if err != nil || !newer || rel.Tag != "1.3.0" {
		t.Fatalf("Check = %+v, %v, %v", rel, newer, err)
	}
	dpkg.version = "1.3.0"
	if rel, newer, err = u.Check(context.Background()); err != nil || newer || rel.Tag != "1.3.0" {
		t.Fatalf("Check = %+v, %v, %v", rel, newer, err)
	}
	if len(dpkg.ran) != 0 {
		t.Fatalf("ran %q", dpkg.ran)
	}
	// Installed in a version newer than the repository's.
	dpkg.version = "1.4.0"
	if rel, newer, err = u.Check(context.Background()); err != nil || newer || rel.Tag != "1.4.0" {
		t.Fatalf("Check = %+v, %v, %v", rel, newer, err)
	}
	// Not offered at all: the installed version is no candidate then.
	dpkg.offered = nil
	if _, _, err := u.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "does not offer example") {
		t.Fatalf("err = %v", err)
	}
	dpkg.offered = []string{"1.3.0"}
	dpkg.unreachable = true
	if _, _, err := u.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "no index was fetched") {
		t.Fatalf("err = %v", err)
	}
	dpkg.fetchErr = errors.New("exit status 100")
	if _, _, err := u.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "fetch the package index") {
		t.Fatalf("err = %v", err)
	}
}

func TestAPTCheckThenInstall(t *testing.T) {
	dpkg := newFakeDpkg()
	u, _ := newAPTUpdater(t, dpkg, 0)
	rel, _, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Published since the check, but Install keeps to what Check found.
	dpkg.offered = append(dpkg.offered, "1.4.0")
	res, err := u.Install(context.Background(), rel)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Updated || dpkg.version != "1.3.0" {
		t.Fatalf("result = %+v, installed %s", res, dpkg.version)
	}
	// A release tag from elsewhere names the package version without "v".
	if _, err := u.Install(context.Background(), Release{Tag: "v1.4.0"}); err != nil || dpkg.version != "1.4.0" {
		t.Fatalf("err = %v, installed %s", err, dpkg.version)
	}
	// One the repository lacks fails to download.
	if _, err := u.Install(context.Background(), Release{Tag: "v9.0.0"}); err == nil || !strings.Contains(err.Error(), "download example 9.0.0") {
		t.Fatalf("err = %v", err)
	}
}

func TestAPTUpdateCurrent(t *testing.T) {
	dpkg := newFakeDpkg()
	dpkg.version = "1.3.0"
	u, _ := newAPTUpdater(t, dpkg, 0)
	u.Current = "v1.3.0"
	res, err := u.Update(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Updated || res.Package != "example" || len(dpkg.ran) != 0 {
		t.Fatalf("result = %+v, ran %q", res, dpkg.ran)
	}
	// Forced, the same version is installed again.
	if _, err := u.Update(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if install := dpkg.ran[len(dpkg.ran)-1]; !slices.Contains(install, "--reinstall") {
		t.Fatalf("install = %q", install)
	}
}

func TestAPTArchitectureQualified(t *testing.T) {
	dpkg := newFakeDpkg()
	dpkg.pkg = "example:arm64"
	u, _ := newAPTUpdater(t, dpkg, 0)
	res, err := u.Update(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Package != "example:arm64" {
		t.Fatalf("result = %+v", res)
	}
	if !slices.ContainsFunc(dpkg.queried, func(argv []string) bool { return argv[len(argv)-1] == "example:arm64=1.3.0" }) {
		t.Fatalf("queried %q", dpkg.queried)
	}
}

func TestAPTEpochVersion(t *testing.T) {
	dpkg := newFakeDpkg()
	dpkg.version, dpkg.offered = "1:1.2.0-1", []string{"1:1.3.0-1"}
	u, _ := newAPTUpdater(t, dpkg, 0)
	res, err := u.Update(context.Background(), false)
	if err != nil || dpkg.version != "1:1.3.0-1" || res.Release.Tag != "1:1.3.0-1" {
		t.Fatalf("result = %+v, err = %v, installed %s", res, err, dpkg.version)
	}
}

func TestAPTWithoutSource(t *testing.T) {
	dpkg := newFakeDpkg()
	u, _ := newAPTUpdater(t, dpkg, 0)
	u.Source, u.Asset = nil, nil
	if _, _, err := u.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if res, err := u.Update(context.Background(), false); err != nil || !res.Updated {
		t.Fatalf("result = %+v, err = %v", res, err)
	}
	if _, err := u.Install(context.Background(), Release{Tag: "1.2.0"}); err != nil {
		t.Fatal(err)
	}
}

func TestAPTDeclined(t *testing.T) {
	dpkg := newFakeDpkg()
	u, apt := newAPTUpdater(t, dpkg, 1000)
	apt.Confirm = func(context.Context, APTPlan) (bool, error) { return false, nil }
	if _, err := u.Update(context.Background(), false); !errors.Is(err, ErrDeclined) {
		t.Fatalf("err = %v", err)
	}
	if len(dpkg.ran) != 0 {
		t.Fatalf("ran %q", dpkg.ran)
	}
	sentinel := errors.New("no terminal")
	apt.Confirm = func(context.Context, APTPlan) (bool, error) { return false, sentinel }
	if _, err := u.Update(context.Background(), false); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v", err)
	}
}

func TestAPTCommandFails(t *testing.T) {
	dpkg := newFakeDpkg()
	dpkg.runErr = errors.New("exit status 100")
	u, _ := newAPTUpdater(t, dpkg, 0)
	_, err := u.Update(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "apt-get update") || len(dpkg.ran) != 1 {
		t.Fatalf("err = %v, ran %q", err, dpkg.ran)
	}
}

func TestAPTVersionNotInstalled(t *testing.T) {
	dpkg := newFakeDpkg()
	u, apt := newAPTUpdater(t, dpkg, 0)
	apt.run = func(context.Context, []string, io.Reader, io.Writer, io.Writer) error { return nil }
	if _, err := u.Update(context.Background(), false); err == nil || !strings.Contains(err.Error(), "1.2.0 is installed") {
		t.Fatalf("err = %v", err)
	}
}

func TestAPTNotApplicable(t *testing.T) {
	cases := map[string]func(u *Updater, apt *APT, dpkg *fakeDpkg){
		"no repository": func(_ *Updater, apt *APT, _ *fakeDpkg) {
			apt.Sources = filepath.Join(filepath.Dir(apt.Sources), "missing")
		},
		"no Sources":      func(_ *Updater, apt *APT, _ *fakeDpkg) { apt.Sources = "" },
		"not Linux":       func(_ *Updater, apt *APT, _ *fakeDpkg) { apt.goos = "darwin" },
		"no owner":        func(_ *Updater, _ *APT, dpkg *fakeDpkg) { dpkg.searchErr = errors.New("exit status 1") },
		"other file":      func(u *Updater, _ *APT, _ *fakeDpkg) { u.Executable = "/usr/bin/other" },
		"diverted":        func(_ *Updater, _ *APT, dpkg *fakeDpkg) { dpkg.search = "diversion by x from: /usr/bin/example\n" },
		"two owners":      func(_ *Updater, _ *APT, dpkg *fakeDpkg) { dpkg.search = "a, b: /usr/bin/example\n" },
		"no APT":          func(u *Updater, _ *APT, _ *fakeDpkg) { u.APT = nil },
		"bad name":        func(_ *Updater, _ *APT, dpkg *fakeDpkg) { dpkg.search = "-x: /usr/bin/example\n" },
		"directory found": func(_ *Updater, apt *APT, _ *fakeDpkg) { apt.Sources = filepath.Dir(apt.Sources) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			dpkg := newFakeDpkg()
			u, apt := newAPTUpdater(t, dpkg, 0)
			change(u, apt, dpkg)
			if _, err := u.Update(context.Background(), false); !errors.Is(err, ErrManaged) {
				t.Fatalf("err = %v", err)
			}
			// Check falls back to Source, which fails here.
			if _, _, err := u.Check(context.Background()); err == nil || err.Error() != "source asked" {
				t.Fatalf("Check err = %v", err)
			}
			if len(dpkg.ran) != 0 {
				t.Fatalf("ran %q", dpkg.ran)
			}
		})
	}
}

func TestAPTInstallNoVersion(t *testing.T) {
	dpkg := newFakeDpkg()
	u, _ := newAPTUpdater(t, dpkg, 0)
	for _, tag := range []string{"release-2", "v1.0 --x", ""} {
		if _, err := u.Install(context.Background(), Release{Tag: tag}); !errors.Is(err, ErrNoVersion) {
			t.Fatalf("%q: err = %v", tag, err)
		}
	}
	if len(dpkg.ran) != 0 {
		t.Fatalf("ran %q", dpkg.ran)
	}
}

func TestDpkgOwner(t *testing.T) {
	cases := []struct{ out, want string }{
		{"example: /usr/bin/example\n", "example"},
		{"example:arm64: /usr/bin/example", "example:arm64"},
		{"example:ARM: /usr/bin/example", ""},
		{"example:: /usr/bin/example", ""},
		{"lib+x.y-1: /usr/bin/example\n", "lib+x.y-1"},
		{"example: /usr/bin/example2\n", ""},
		{"example: /usr/bin/example\nother: /usr/bin/example\n", ""},
		{"Example: /usr/bin/example\n", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := dpkgOwner([]byte(c.out), "/usr/bin/example"); got != c.want {
			t.Errorf("dpkgOwner(%q) = %q, want %q", c.out, got, c.want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	plan := APTPlan{Commands: [][]string{{"a b", "it's", "", "x=1.0~rc1"}}}
	if got, want := plan.String(), `'a b' 'it'\''s' '' 'x=1.0~rc1'`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
