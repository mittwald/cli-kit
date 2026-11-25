package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// DefaultChecksums is the checksums file an [Updater] fetches when it is
// told no other name: the name a GoReleaser configuration gives it with
// checksum.name_template "checksums.txt". GoReleaser's own default name
// carries the version; see [VersionedChecksums].
const DefaultChecksums = "checksums.txt"

// DefaultMaxDownload bounds the archive download when
// Updater.MaxDownload is zero.
const DefaultMaxDownload = 512 << 20

// maxChecksumsSize bounds the checksums file download.
const maxChecksumsSize = 1 << 20

// Updater replaces the running executable with the latest release, or with
// a release it is given.
type Updater struct {
	// Source finds and downloads releases. It is required.
	Source Source
	// Current is the version of the running binary, usually injected at
	// build time. A value that is no version, such as "dev", counts as
	// older than any release; what "git describe" prints for a commit after
	// a tag, such as "v1.2.3-5-gabc1234", counts as newer than that tag
	// (see [Compare]).
	Current string
	// Asset names the archive for a platform. It is required; see
	// [DefaultAssets], [TitleCaseAssets] and [VersionedAssets].
	Asset AssetNamer
	// Platform is the platform to download for; the zero value means
	// [CurrentPlatform].
	Platform Platform
	// Checksums is the release's checksums file; empty means
	// [DefaultChecksums]. ChecksumsFor takes precedence.
	Checksums string
	// ChecksumsFor names the checksums file of a release, for a name that
	// changes from release to release, such as GoReleaser's default
	// "tool_1.2.3_checksums.txt" (see [VersionedChecksums]).
	ChecksumsFor func(rel Release) string
	// SkipChecksum installs the archive without verifying it, for a source
	// that publishes no checksums file. What is lost is the detection of a
	// download that was corrupted or altered on its way; the checksum never
	// proved more than that (see [VerifyChecksum]). It cannot be combined
	// with Verifier.
	SkipChecksum bool
	// Verifier, when set, requires the checksums file to carry a valid
	// signature by a trusted key, published beside it under the name
	// Verifier.SignatureName gives. The archive is then installed only when
	// it matches a checksums file the key holder signed; see [Minisign].
	Verifier Verifier
	// Binary is the file name of the binary inside the archive, without
	// ".exe"; empty means the running executable's own name. When no file
	// has that name, the largest file is taken. Either way the file has to
	// be an executable for Platform; see [ExtractFor].
	Binary string
	// Executable is the file to replace; empty means the running
	// executable. Symbolic links are resolved either way, so the file a
	// link leads to is replaced, not the link.
	Executable string
	// Managed reports whether a package manager owns the executable at a
	// path, which it is given with symbolic links resolved, in which case
	// Update and Install refuse to replace it. Nil
	// means [PackageManaged]; a tool that ships no OS package and may
	// replace itself anywhere sets a function that always returns false.
	Managed func(path string) bool
	// APT, when set, updates a managed executable that a .deb installed
	// from a registered APT repository through APT, instead of refusing
	// it; the repository then also replaces Source as where the latest
	// version is read. See [APT] for when it applies. Nil refuses every
	// managed executable.
	APT *APT
	// MaxDownload bounds the archive size in bytes; zero means
	// [DefaultMaxDownload]. The archive is held in memory.
	MaxDownload int64
}

// Result describes what [Updater.Update] or [Updater.Install] did.
type Result struct {
	// From is the version that ran before the update.
	From string
	// Release is the latest release, or the one Install was given. When
	// [APT] updates the executable, its Tag is the package's version in
	// the repository, such as "1.2.3".
	Release Release
	// Asset is the archive that was installed; empty when nothing was, or
	// when APT installed the release.
	Asset string
	// Package is the Debian package that owns the executable, as
	// [APTPlan.Package] names it, when [APT] updates it, whether or not it
	// installed anything; empty otherwise.
	Package string
	// Path is the executable that was, or would have been, replaced.
	Path string
	// Updated is false when the running version was already current.
	Updated bool
}

// Check returns the latest release and whether it is newer than Current.
// When the release's tag is no version, such as "release-2", it cannot
// tell: it returns the release with an error wrapping [ErrNoVersion]
// rather than report the running version as current forever. An
// executable that [Updater.APT] applies to is checked against the APT
// repository instead, and newer means the repository offers a version
// other than the installed one.
func (u *Updater) Check(ctx context.Context) (Release, bool, error) {
	if _, pkg, err := u.aptPackage(ctx); err != nil {
		return Release{}, false, err
	} else if pkg != "" {
		latest, installed, err := u.APT.check(ctx, pkg)
		if err != nil {
			return Release{}, false, err
		}
		return Release{Tag: latest}, latest != installed, nil
	}
	if u.Source == nil {
		return Release{}, false, errors.New("selfupdate: Updater.Source is required")
	}
	rel, err := u.Source.Latest(ctx)
	if err != nil {
		return Release{}, false, err
	}
	if _, ok := parseVersion(rel.Tag); !ok {
		return rel, false, fmt.Errorf("%w: %q", ErrNoVersion, rel.Tag)
	}
	return rel, IsNewer(rel.Tag, u.Current), nil
}

// Update installs the latest release unless the running version is already
// current; force installs it anyway. Before it asks for the latest release
// it makes sure the executable may be replaced, as Install does. A latest
// release whose tag is no version is an error wrapping [ErrNoVersion],
// with force too; [Updater.Install] installs such a release.
func (u *Updater) Update(ctx context.Context, force bool) (Result, error) {
	result, err := u.prepare(ctx)
	if err != nil {
		return result, err
	}
	if result.Package != "" {
		version, updated, err := u.APT.update(ctx, result.Package, Release{}, force)
		result.Release, result.Updated = Release{Tag: version}, updated
		return result, err
	}
	rel, newer, err := u.Check(ctx)
	if err != nil {
		return result, err
	}
	result.Release = rel
	if !newer && !force {
		return result, nil
	}
	return u.install(ctx, result)
}

// Install installs rel whatever version is running, for a caller that
// checked first and reported what it found: the release installed is the
// one [Updater.Check] returned, even when a newer one was published since.
// Before downloading anything it makes sure the executable may be replaced:
// it refuses, with an error wrapping [ErrManaged], an executable a package
// manager owns, and fails early when the executable's directory is not
// writable. The archive is verified against the release's checksums file
// unless SkipChecksum is set; see [VerifyChecksum] for what that proves.
// With a Verifier, the checksums file has to carry a valid signature too.
// An executable that [Updater.APT] applies to is updated through APT
// instead, to the version of the package rel's version names.
func (u *Updater) Install(ctx context.Context, rel Release) (Result, error) {
	result, err := u.prepare(ctx)
	if err != nil {
		return result, err
	}
	result.Release = rel
	if result.Package != "" {
		if rel.Tag == "" {
			return result, fmt.Errorf("%w: %q", ErrNoVersion, rel.Tag)
		}
		_, result.Updated, err = u.APT.update(ctx, result.Package, rel, true)
		return result, err
	}
	return u.install(ctx, result)
}

// AssetName returns the name of the archive Update and Install download
// for rel: Asset applied to Platform, or to [CurrentPlatform] when Platform
// is the zero value. It returns "" when Asset is nil.
func (u *Updater) AssetName(rel Release) string {
	if u.Asset == nil {
		return ""
	}
	return u.Asset(rel, u.platform())
}

// prepare locates the executable and makes sure it may be replaced, or
// that APT updates it.
func (u *Updater) prepare(ctx context.Context) (Result, error) {
	// APT needs neither an archive nor a Source.
	if path, pkg, err := u.aptPackage(ctx); err != nil {
		return Result{}, err
	} else if pkg != "" {
		return Result{From: u.Current, Path: path, Package: pkg}, nil
	}
	if u.Asset == nil {
		return Result{}, errors.New("selfupdate: Updater.Asset is required")
	}
	if u.Source == nil {
		return Result{}, errors.New("selfupdate: Updater.Source is required")
	}
	if u.Verifier != nil && u.SkipChecksum {
		return Result{}, errors.New("selfupdate: Updater.Verifier needs the checksums file that SkipChecksum skips")
	}
	path, err := u.executable()
	if err != nil {
		return Result{}, err
	}
	result := Result{From: u.Current, Path: path}
	managed := u.Managed
	if managed == nil {
		managed = PackageManaged
	}
	if managed(path) {
		return result, fmt.Errorf("%w: %s; update it with that package manager instead", ErrManaged, path)
	}
	// Only now is the directory this package's to tidy: a managed one is
	// left exactly as it is.
	RemoveOld(path)
	if err := writable(filepath.Dir(path)); err != nil {
		return result, err
	}
	return result, nil
}

// aptPackage returns the executable and the package APT updates it
// through, or no package when APT does not apply to it.
func (u *Updater) aptPackage(ctx context.Context) (path, pkg string, err error) {
	if u.APT == nil {
		return "", "", nil
	}
	path, err = u.executable()
	if err != nil {
		return "", "", err
	}
	managed := u.Managed
	if managed == nil {
		managed = PackageManaged
	}
	if !managed(path) {
		return path, "", nil
	}
	return path, u.APT.owner(ctx, path), nil
}

// install downloads, verifies and installs result.Release over result.Path.
func (u *Updater) install(ctx context.Context, result Result) (Result, error) {
	rel := result.Release
	name := u.AssetName(rel)
	archive, err := u.download(ctx, rel, name, u.maxDownload())
	if err != nil {
		return result, err
	}
	if !u.SkipChecksum {
		checksumsName := u.checksums(rel)
		checksums, err := u.download(ctx, rel, checksumsName, maxChecksumsSize)
		if err != nil {
			return result, fmt.Errorf("selfupdate: fetch checksums to verify %s: %w", plain(name), err)
		}
		if u.Verifier != nil {
			signatureName := u.Verifier.SignatureName(checksumsName)
			signature, err := u.download(ctx, rel, signatureName, maxSignatureSize)
			if err != nil {
				return result, fmt.Errorf("selfupdate: fetch signature to verify %s: %w", plain(checksumsName), err)
			}
			if err := u.Verifier.Verify(checksums, signature); err != nil {
				return result, fmt.Errorf("%s: %w", plain(checksumsName), err)
			}
		}
		if err := VerifyChecksum(checksums, name, archive); err != nil {
			return result, err
		}
	}

	binaryName := u.Binary
	if binaryName == "" {
		binaryName = strings.TrimSuffix(filepath.Base(result.Path), ".exe")
	}
	binary, err := ExtractFor(u.platform(), name, archive, binaryName)
	if err != nil {
		return result, err
	}
	if err := ReplaceExecutable(result.Path, binary); err != nil {
		return result, err
	}
	result.Asset = name
	result.Updated = true
	return result, nil
}

func (u *Updater) platform() Platform {
	if u.Platform == (Platform{}) {
		return CurrentPlatform()
	}
	return u.Platform
}

// checksums names rel's checksums file.
func (u *Updater) checksums(rel Release) string {
	if u.ChecksumsFor != nil {
		if name := u.ChecksumsFor(rel); name != "" {
			return name
		}
	}
	if u.Checksums != "" {
		return u.Checksums
	}
	return DefaultChecksums
}

// executable returns the file to replace, with symbolic links resolved.
func (u *Updater) executable() (string, error) {
	if u.Executable == "" {
		return executablePath()
	}
	resolved, err := filepath.EvalSymlinks(u.Executable)
	switch {
	case err == nil:
		return resolved, nil
	case errors.Is(err, fs.ErrNotExist):
		// Nothing is there yet: the binary is installed at the path itself.
		return u.Executable, nil
	}
	return "", fmt.Errorf("selfupdate: resolve %s: %w", u.Executable, err)
}

func (u *Updater) maxDownload() int64 {
	if u.MaxDownload > 0 {
		return u.MaxDownload
	}
	return DefaultMaxDownload
}

func (u *Updater) download(ctx context.Context, rel Release, name string, limit int64) ([]byte, error) {
	body, err := u.Source.Download(ctx, rel, name)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	// One byte more than the limit is read to tell a file of exactly that
	// size from a larger one.
	limit = min(limit, math.MaxInt64-1)
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("selfupdate: download %s: %w", plain(name), err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("selfupdate: %s is larger than %d bytes", plain(name), limit)
	}
	return data, nil
}

// writable reports an error when no file can be created in dir, so Update
// fails before a long download rather than after it.
func writable(dir string) error {
	f, err := os.CreateTemp(dir, ".selfupdate-probe-*")
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("selfupdate: no write permission in %s: run the update with sufficient privileges, "+
				"or install the binary in a directory you own: %w", dir, err)
		}
		return fmt.Errorf("selfupdate: cannot write to %s: %w", dir, err)
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}
