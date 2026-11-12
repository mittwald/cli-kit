package selfupdate

import (
	"runtime"
	"runtime/debug"
	"strings"
)

// Platform is the operating system and architecture a binary is built for,
// in Go's terms.
type Platform struct {
	// OS is a GOOS value, such as "linux".
	OS string
	// Arch is a GOARCH value, such as "amd64".
	Arch string
	// Arm is the GOARM version, such as "7", for Arch "arm" only.
	Arm string
}

// CurrentPlatform returns the platform the running binary was built for.
// The GOARM version comes from the binary's build information, and
// defaults to "7" when it is missing.
func CurrentPlatform() Platform {
	p := Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if p.Arch == "arm" {
		p.Arm = "7"
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				if setting.Key == "GOARM" && setting.Value != "" {
					// Go 1.22 added suffixes such as "7,softfloat".
					p.Arm, _, _ = strings.Cut(setting.Value, ",")
				}
			}
		}
	}
	return p
}

// ArchiveExt returns the archive extension of GoReleaser's tar.gz default
// with the usual zip override for Windows (format_overrides, as
// "goreleaser init" writes it): "zip" on Windows, "tar.gz" elsewhere.
func (p Platform) ArchiveExt() string {
	if p.OS == "windows" {
		return "zip"
	}
	return "tar.gz"
}

// AssetNamer returns the name of the archive a release carries for a
// platform. It mirrors the archives.name_template of the tool's GoReleaser
// configuration, plus the archive extension.
type AssetNamer func(rel Release, p Platform) string

// DefaultAssets names archives the way GoReleaser does when its
// configuration sets no archives.name_template, as in
// "tool_1.2.3_linux_amd64.tar.gz" or "tool_1.2.3_linux_armv7.tar.gz":
//
//	{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}
//	{{- with .Arm }}v{{ . }}{{ end }}
//
// GoReleaser's default goes on to append the MIPS variant and a GOAMD64
// level other than "v1"; a release built for those needs an [AssetNamer]
// of its own. Windows archives are named ".zip" (see
// [Platform.ArchiveExt]), which takes the usual format_overrides entry:
// without one GoReleaser packs a tar.gz for Windows too.
func DefaultAssets(project string) AssetNamer {
	return func(rel Release, p Platform) string {
		arch := p.Arch
		if p.Arm != "" {
			arch += "v" + p.Arm
		}
		return project + "_" + rel.Version() + "_" + p.OS + "_" + arch + "." + p.ArchiveExt()
	}
}

// TitleCaseAssets names archives by a common title-case template, the one
// "goreleaser init" writes into a new configuration so that the names
// match the output of uname, as in "tool_Linux_x86_64.tar.gz" or
// "tool_Windows_i386.zip". It is not GoReleaser's default, which
// [DefaultAssets] follows. The template is:
//
//	{{ .ProjectName }}_
//	{{- title .Os }}_
//	{{- if eq .Arch "amd64" }}x86_64
//	{{- else if eq .Arch "386" }}i386
//	{{- else }}{{ .Arch }}{{ end }}
//	{{- if .Arm }}v{{ .Arm }}{{ end }}
func TitleCaseAssets(project string) AssetNamer {
	return func(_ Release, p Platform) string {
		arch := p.Arch
		switch arch {
		case "amd64":
			arch = "x86_64"
		case "386":
			arch = "i386"
		}
		if p.Arm != "" {
			arch += "v" + p.Arm
		}
		return project + "_" + titleOS(p.OS) + "_" + arch + "." + p.ArchiveExt()
	}
}

// titleOS spells an OS the way GoReleaser's title function does, with
// macOS as "Darwin".
func titleOS(goos string) string {
	if goos == "" {
		return ""
	}
	return strings.ToUpper(goos[:1]) + goos[1:]
}

// VersionedAssets names archives with the version and Go's own platform
// names, as in "tool-1.2.3-linux-amd64.tar.gz" or
// "tool-1.2.3-linux-armv7.tar.gz":
//
//	{{ .ProjectName }}-{{ .Version }}-{{ .Os }}-{{ .Arch }}
//	{{- with .Arm }}v{{ . }}{{ end }}
func VersionedAssets(project string) AssetNamer {
	return func(rel Release, p Platform) string {
		arch := p.Arch
		if p.Arm != "" {
			arch += "v" + p.Arm
		}
		return project + "-" + rel.Version() + "-" + p.OS + "-" + arch + "." + p.ArchiveExt()
	}
}

// VersionedChecksums names a release's checksums file the way GoReleaser's
// default checksum name_template does, as in "tool_1.2.3_checksums.txt":
//
//	{{ .ProjectName }}_{{ .Version }}_checksums.txt
//
// Use it as Updater.ChecksumsFor.
func VersionedChecksums(project string) func(rel Release) string {
	return func(rel Release) string {
		return project + "_" + rel.Version() + "_checksums.txt"
	}
}
