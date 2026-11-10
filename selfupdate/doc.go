// Package selfupdate replaces a running command-line tool with its latest
// release.
//
// An [Updater] asks a [Source] for the latest release, downloads the archive
// built for the running platform, verifies it against the release's
// checksums file, extracts the binary and swaps it in for the running
// executable. Two sources are included: [GitLab], which reads releases from
// a GitLab project and downloads from its Generic Package Registry (the one
// GitLab download path that honors an API token rather than a browser
// session), and [GitHub], which reads GitHub Releases.
//
// The archive names follow GoReleaser's name_template. [TitleCaseAssets]
// matches GoReleaser's default template (tool_Linux_x86_64.tar.gz) and
// [VersionedAssets] the common versioned one (tool-1.2.3-linux-amd64.tar.gz);
// any other template is a small [AssetNamer].
//
// A binary installed by a package manager (Homebrew, a .deb or .rpm, Nix,
// Scoop) is that manager's to update; [Updater.Update] refuses to touch it
// and returns [ErrManaged].
//
// Credentials come from a [TokenFunc], which is asked again with retry set
// after the server rejected a token. It fits a secret.Resolver from this
// module:
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
package selfupdate
