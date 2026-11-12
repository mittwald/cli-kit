// Package selfupdate replaces a running command-line tool with its latest
// release.
//
// An [Updater] asks a [Source] for the latest release, downloads the archive
// built for the running platform, verifies it against the release's
// checksums file, extracts the binary and swaps it in for the running
// executable. [Updater.Update] does all of that in one call; a command that
// reports what it found before it installs calls [Updater.Check] and then
// [Updater.Install] with the release Check returned. Two sources are
// included: [GitLab], which reads releases from a GitLab project and
// downloads from its Generic Package Registry (the one GitLab download path
// that honors an API token rather than a browser session), and [GitHub],
// which reads GitHub Releases. Both return the latest stable release: a
// GitHub pre-release or draft, and a GitLab release whose tag has a
// pre-release part, are passed over.
//
// # What the checksum proves
//
// The checksum detects a download that was corrupted, or altered on a
// transport or storage hop between the release and the user. It does not
// protect against somebody who can write the release's files: the
// checksums file comes from the same release as the archive and carries no
// signature, so whoever can replace the one can replace the other. An
// update is as trustworthy as the account, tokens and CI jobs that may
// publish to the release, and the TLS connection to it. A tool that needs
// more verifies a signature of its own before it calls
// [ReplaceExecutable].
//
// What is extracted has to be an executable for the platform (ELF, Mach-O
// or PE), so a file that merely has the binary's name or size is never
// installed; see [ExtractFor].
//
// # Naming
//
// The archive names follow GoReleaser's name_template. [DefaultAssets]
// matches GoReleaser's default (tool_1.2.3_linux_amd64.tar.gz),
// [TitleCaseAssets] the title-case template "goreleaser init" writes
// (tool_Linux_x86_64.tar.gz) and [VersionedAssets] a common hyphenated one
// (tool-1.2.3-linux-amd64.tar.gz); any other template is a small
// [AssetNamer]. The checksums file is "checksums.txt" unless the Updater
// names another; [VersionedChecksums] matches GoReleaser's default,
// tool_1.2.3_checksums.txt.
//
// # Versions
//
// Versions are ordered as semantic versions; see [Compare]. A running
// version that is none, such as "dev", is older than any release, and what
// "git describe" prints for a commit after a tag is newer than that tag. A
// latest release whose tag is no version is an error ([ErrNoVersion])
// rather than "already up to date".
//
// # Package managers
//
// A binary installed by a package or version manager (a .deb or .rpm,
// Homebrew, MacPorts, Nix, Snap, mise, asdf, Scoop, Chocolatey, WinGet) is
// that manager's to update; [Updater.Update] and [Updater.Install] refuse
// to touch it or anything beside it and return [ErrManaged]. Symbolic
// links are resolved first, so the file a link leads to is the one that is
// judged and replaced. See [PackageManaged] for what is recognized.
//
// # Requests and credentials
//
// Requests are bounded by the context they are given. A source without an
// HTTP client of its own also gives up on a server that does not answer
// within half a minute and on a download that delivers nothing for a
// minute; a client the caller sets is the caller's to bound.
//
// Credentials come from a [TokenFunc], which is asked again with retry set
// after the server rejected a token. A rate limit ([ErrRateLimited]) is no
// rejection and does not ask again. The credential is sent to the source's
// API origin only, never to a host a download redirects to, and never over
// plain http except to a loopback address. A TokenFunc fits a
// secret.Resolver from this module:
//
//	var from secret.Source
//	token := func(ctx context.Context, retry bool) (string, error) {
//		if retry {
//			if err := resolver.Forget(from); err != nil {
//				return "", err
//			}
//		}
//		value, source, err := resolver.Resolve()
//		from = source
//		return value, err
//	}
//
// Text a server or an archive chose (a response body, a file name) reaches
// an error message with its control characters removed.
package selfupdate
