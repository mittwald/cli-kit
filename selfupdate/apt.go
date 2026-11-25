package selfupdate

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrDeclined reports that [APT.Confirm] declined the update, so nothing
// was installed.
var ErrDeclined = errors.New("selfupdate: update declined")

// The programs an [APT] update runs, by absolute path, so that PATH cannot
// lead a command run as root to another program.
const (
	aptGet    = "/usr/bin/apt-get"
	aptCache  = "/usr/bin/apt-cache"
	dpkgQuery = "/usr/bin/dpkg-query"
	sudo      = "/usr/bin/sudo"
)

// APT updates an executable that a .deb installed while the APT repository
// that publishes the package is registered on the system, through APT
// rather than by replacing the file. Set it as [Updater.APT].
//
// It applies only on Linux, only to an executable that [Updater.Managed]
// reports as managed, only while the Sources file exists, and only when
// dpkg names exactly one package as the owner of the executable. Anything
// else is refused with [ErrManaged] as before: an executable a .deb
// installed without the repository, or another package manager's.
//
// Where it applies, the repository is the source of releases, not
// [Updater.Source], which is never asked: [Updater.Check] and
// [Updater.Update] read the latest version from the repository, so no
// credential for the Source is needed. The repository's index is fetched
// into a temporary directory without root privileges, leaving the
// system's package lists as they are, and the package is downloaded from
// it the same way; APT checks both against the repository's signing key.
// Only the install of the downloaded file needs root: it runs apt-get
// update and apt-get install with the file, so that dependencies come from
// all of the system's repositories, freshly fetched. Without root privileges it runs through sudo,
// which asks for a password on the terminal itself; the process never
// sees it. The release's archive, checksums file and [Updater.Verifier]
// are not used.
type APT struct {
	// Sources is the file that registers the repository, as a deb822
	// ".sources" or a one-line ".list" file, such as
	// "/etc/apt/sources.list.d/example.sources". It is required.
	Sources string
	// Confirm is asked before the package is installed, with the plan of
	// what will run. When it returns false, the update ends with
	// [ErrDeclined]. Nil installs without asking, for a command's --yes.
	Confirm func(ctx context.Context, plan APTPlan) (bool, error)
	// Stdin, Stdout and Stderr are given to the install command, so that
	// sudo can ask for a password and apt-get can report its progress. Nil
	// means the process's own.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// Set by tests to fake the system.
	goos   string
	euid   func() int
	output func(ctx context.Context, dir string, argv []string) ([]byte, error)
	run    func(ctx context.Context, argv []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// APTPlan is what an [APT] update is about to do, for [APT.Confirm] to
// show.
type APTPlan struct {
	// Package is the package that owns the executable, qualified with its
	// architecture, as in "tool:amd64", when dpkg qualifies it.
	Package string
	// Installed is the version of Package that is installed.
	Installed string
	// Version is the version that will be installed.
	Version string
	// File is the downloaded package that will be installed.
	File string
	// Sudo reports whether Commands run through sudo.
	Sudo bool
	// Commands are the commands that will run, in order, each as its
	// arguments with the program first.
	Commands [][]string
}

// String returns the commands one per line, quoted for a POSIX shell, so
// that a user can read them or run them by hand.
func (p APTPlan) String() string {
	lines := make([]string, len(p.Commands))
	for i, argv := range p.Commands {
		quoted := make([]string, len(argv))
		for j, arg := range argv {
			quoted[j] = shellQuote(arg)
		}
		lines[i] = strings.Join(quoted, " ")
	}
	return strings.Join(lines, "\n")
}

// shellQuote quotes s for a POSIX shell when it needs quoting.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./:=+,@") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// owner returns the package that installed path when the APT update
// applies to it, and "" when it does not.
func (a *APT) owner(ctx context.Context, path string) string {
	if a.os() != "linux" || a.Sources == "" {
		return ""
	}
	if info, err := os.Stat(a.Sources); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	for _, file := range dpkgPaths(path) {
		out, err := a.query(ctx, "", []string{dpkgQuery, "--search", "--", file})
		if err != nil {
			continue
		}
		return dpkgOwner(out, file)
	}
	return ""
}

// dpkgPaths returns the paths dpkg may know the executable at path by:
// path itself and, on a system with merged /usr, the path without "/usr"
// when that leads to the same file, since a package may install to /bin or
// /sbin.
func dpkgPaths(path string) []string {
	paths := []string{path}
	if alias, ok := strings.CutPrefix(path, "/usr"); ok && strings.HasPrefix(alias, "/") {
		if resolved, err := filepath.EvalSymlinks(alias); err == nil && resolved == path {
			paths = append(paths, alias)
		}
	}
	return paths
}

// dpkgOwner reads what "dpkg-query --search PATH" printed and returns the
// one package it names as the owner of path, with the architecture dpkg
// qualified it with, if any. It returns "" when no package or more than
// one owns path, or when a diversion moved what the package installed.
func dpkgOwner(out []byte, path string) string {
	owner := ""
	for line := range strings.Lines(string(out)) {
		line = strings.TrimRight(line, "\n")
		if strings.HasPrefix(line, "diversion by ") {
			return ""
		}
		packages, file, ok := strings.Cut(line, ": ")
		if !ok || file != path || owner != "" || strings.Contains(packages, ", ") {
			return ""
		}
		name, arch, qualified := strings.Cut(packages, ":")
		if !debName(name) || qualified && !debArch(arch) {
			return ""
		}
		owner = packages
	}
	return owner
}

// debName reports whether s is a valid Debian package name.
func debName(s string) bool {
	if len(s) < 2 || !isLowerAlnum(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if c := s[i]; !isLowerAlnum(c) && c != '+' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

// debArch reports whether s is a valid Debian architecture name.
func debArch(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !isLowerAlnum(c) && c != '-' {
			return false
		}
	}
	return true
}

// debVersion reports whether s can be a Debian version: it starts with a
// digit and holds only the characters a version may hold.
func debVersion(s string) bool {
	if s == "" || s[0] < '0' || s[0] > '9' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !isLowerAlnum(c) && (c < 'A' || c > 'Z') && !strings.ContainsRune(".+-~:", rune(c)) {
			return false
		}
	}
	return true
}

func isLowerAlnum(c byte) bool { return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' }

// installed returns the version of pkg dpkg has installed.
func (a *APT) installed(ctx context.Context, pkg string) (string, error) {
	out, err := a.query(ctx, "", []string{dpkgQuery, "--show", "--showformat=${Version}", "--", pkg})
	if err != nil {
		return "", fmt.Errorf("selfupdate: read installed version of %s: %w", pkg, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// aptRepo is the repository's index, fetched into a directory of its own.
type aptRepo struct {
	a    *APT
	dir  string
	opts []string
}

// fetch fetches the repository's index into a new temporary directory,
// which close removes. It needs no root privileges and leaves the system's
// package lists alone.
func (a *APT) fetch(ctx context.Context) (*aptRepo, error) {
	sources, err := filepath.Abs(a.Sources)
	if err != nil {
		return nil, fmt.Errorf("selfupdate: locate %s: %w", a.Sources, err)
	}
	dir, err := os.MkdirTemp("", "selfupdate-apt-")
	if err != nil {
		return nil, fmt.Errorf("selfupdate: create directory for the package index: %w", err)
	}
	repo := &aptRepo{a: a, dir: dir}
	// root, who installs the downloaded file, reads it, and so does the
	// user APT downloads as when it runs as root. Nobody else may write.
	if err := os.Chmod(dir, 0o755); err != nil { // #nosec G302 -- a directory others may read, not write
		repo.close()
		return nil, fmt.Errorf("selfupdate: create directory for the package index: %w", err)
	}
	for _, sub := range []string{"lists/partial", "cache/archives/partial"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil { // #nosec G301 -- as above
			repo.close()
			return nil, fmt.Errorf("selfupdate: create directory for the package index: %w", err)
		}
	}
	repo.opts = []string{
		"-o", "Dir::Etc::sourcelist=" + sources,
		"-o", "Dir::Etc::sourceparts=-",
		"-o", "Dir::State::Lists=" + filepath.Join(dir, "lists"),
		"-o", "Dir::Cache=" + filepath.Join(dir, "cache"),
		// A default release the system sets names a release of its own
		// repositories, which APT rejects when they are not listed.
		"-o", "APT::Default-Release=",
	}
	if a.uid() == 0 {
		// The directory is root's; the user APT would otherwise download
		// as cannot write to it.
		repo.opts = append(repo.opts, "-o", "APT::Sandbox::User=root")
	}
	// Error-Mode makes an index that could not be fetched an error rather
	// than a warning; an APT older than 2.1.16 ignores it.
	if _, err := a.query(ctx, dir, repo.argv(aptGet, "-o", "APT::Update::Error-Mode=any", "update")); err != nil {
		repo.close()
		return nil, fmt.Errorf("selfupdate: fetch the package index of %s: %w", a.Sources, err)
	}
	// Without any index, the installed version would pass for the latest.
	if indexes, _ := filepath.Glob(filepath.Join(dir, "lists", "*_Packages*")); len(indexes) == 0 {
		repo.close()
		return nil, fmt.Errorf("selfupdate: fetch the package index of %s: no index was fetched", a.Sources)
	}
	return repo, nil
}

// argv returns the command line of program with the options that limit it
// to the repository, followed by args.
func (r *aptRepo) argv(program string, args ...string) []string {
	argv := append([]string{program, "-q"}, r.opts...)
	return append(argv, args...)
}

func (r *aptRepo) close() { _ = os.RemoveAll(r.dir) }

// candidate returns the version of pkg that APT would install from the
// repository: its latest, unless a pin on the system says otherwise. A
// package that is installed in a version newer than any the repository
// has is its own candidate. A repository that does not offer pkg at all is
// an error, so that the installed version cannot pass for the latest.
func (r *aptRepo) candidate(ctx context.Context, pkg string) (string, error) {
	// madison lists the versions in the repository's index only, unlike
	// policy, which counts the installed one too.
	out, err := r.a.query(ctx, r.dir, r.argv(aptCache, "madison", "--", pkg))
	if err != nil {
		return "", fmt.Errorf("selfupdate: read the versions of %s: %w", pkg, err)
	}
	offered := false
	for line := range strings.Lines(string(out)) {
		if fields := strings.Split(line, "|"); len(fields) == 3 && debVersion(strings.TrimSpace(fields[1])) {
			offered = true
			break
		}
	}
	if !offered {
		return "", fmt.Errorf("selfupdate: %s does not offer %s", r.a.Sources, pkg)
	}
	out, err = r.a.query(ctx, r.dir, r.argv(aptCache, "policy", "--", pkg))
	if err != nil {
		return "", fmt.Errorf("selfupdate: read the versions of %s: %w", pkg, err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		if value, ok := strings.CutPrefix(strings.TrimSpace(scanner.Text()), "Candidate:"); ok {
			if version := strings.TrimSpace(value); debVersion(version) {
				return version, nil
			}
			break
		}
	}
	return "", fmt.Errorf("selfupdate: %s does not offer %s", r.a.Sources, pkg)
}

// download downloads version of pkg and returns the file.
func (r *aptRepo) download(ctx context.Context, pkg, version string) (string, error) {
	if _, err := r.a.query(ctx, r.dir, r.argv(aptGet, "download", "--", pkg+"="+version)); err != nil {
		return "", fmt.Errorf("selfupdate: download %s %s: %w", pkg, version, err)
	}
	files, err := filepath.Glob(filepath.Join(r.dir, "*.deb"))
	if err != nil || len(files) != 1 {
		return "", fmt.Errorf("selfupdate: download %s %s: found %d packages, not one", pkg, version, len(files))
	}
	return files[0], nil
}

// check returns the version the repository offers for pkg and the version
// that is installed.
func (a *APT) check(ctx context.Context, pkg string) (latest, installed string, err error) {
	installed, err = a.installed(ctx, pkg)
	if err != nil {
		return "", "", err
	}
	repo, err := a.fetch(ctx)
	if err != nil {
		return "", "", err
	}
	defer repo.close()
	latest, err = repo.candidate(ctx, pkg)
	return latest, installed, err
}

// update installs the version of pkg the repository offers, or the one
// rel names when it has a tag, unless that version is
// installed and force is false. It returns the version it found and
// whether it installed it.
func (a *APT) update(ctx context.Context, pkg string, rel Release, force bool) (string, bool, error) {
	installed, err := a.installed(ctx, pkg)
	if err != nil {
		return "", false, err
	}
	repo, err := a.fetch(ctx)
	if err != nil {
		return "", false, err
	}
	defer repo.close()
	version := rel.Version()
	if rel.Tag == "" {
		if version, err = repo.candidate(ctx, pkg); err != nil {
			return "", false, err
		}
	} else if !debVersion(version) {
		return "", false, fmt.Errorf("%w: %q", ErrNoVersion, plain(rel.Tag))
	}
	if version == installed && !force {
		return version, false, nil
	}
	file, err := repo.download(ctx, pkg, version)
	if err != nil {
		return version, false, err
	}
	install := []string{aptGet, "install", "--yes", "--allow-downgrades"}
	if version == installed {
		install = append(install, "--reinstall")
	}
	plan := APTPlan{
		Package:   pkg,
		Installed: installed,
		Version:   version,
		File:      file,
		Commands: [][]string{
			// The system's lists, refreshed, offer the dependencies the
			// new version may need.
			{aptGet, "update"},
			append(install, "--", file),
		},
	}
	if a.uid() != 0 {
		plan.Sudo = true
		for i, argv := range plan.Commands {
			plan.Commands[i] = append([]string{sudo, "--"}, argv...)
		}
	}
	if a.Confirm != nil {
		ok, err := a.Confirm(ctx, plan)
		if err != nil {
			return version, false, err
		}
		if !ok {
			return version, false, ErrDeclined
		}
	}
	for _, argv := range plan.Commands {
		if err := a.exec(ctx, argv); err != nil {
			return version, false, fmt.Errorf("selfupdate: %s: %w", strings.Join(argv, " "), err)
		}
	}
	now, err := a.installed(ctx, pkg)
	if err != nil {
		return version, false, err
	}
	if now != version {
		return version, false, fmt.Errorf("selfupdate: %s %s is installed after the update, not %s", pkg, plain(now), version)
	}
	return version, true, nil
}

func (a *APT) os() string {
	if a.goos != "" {
		return a.goos
	}
	return runtime.GOOS
}

func (a *APT) uid() int {
	if a.euid != nil {
		return a.euid()
	}
	return os.Geteuid()
}

// query runs argv in dir, or in the current directory when dir is empty,
// and returns what it printed.
func (a *APT) query(ctx context.Context, dir string, argv []string) ([]byte, error) {
	if a.output != nil {
		return a.output(ctx, dir, argv)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- fixed programs, arguments without a shell
	cmd.Dir = dir
	// The output is parsed, so it must not be translated.
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil && stderr.Len() > 0 {
		return out, fmt.Errorf("%w: %s", err, plain(strings.TrimSpace(stderr.String())))
	}
	return out, err
}

// exec runs argv with the streams of a.
func (a *APT) exec(ctx context.Context, argv []string) error {
	stdin, stdout, stderr := a.Stdin, a.Stdout, a.Stderr
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	if a.run != nil {
		return a.run(ctx, argv, stdin, stdout, stderr)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) // #nosec G204 -- fixed programs, arguments without a shell
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	return cmd.Run()
}
