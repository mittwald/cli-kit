package buildinfo

import (
	"fmt"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// Values stamped at link time with -ldflags "-X …". Leave them empty in the
// source; [Get] falls back to the toolchain's build information for every
// value that was not stamped.
var (
	// Version is the release version, with or without a leading "v".
	Version string
	// Commit is the full VCS revision the binary was built from.
	Commit string
	// Date is the build or commit time in RFC 3339 format.
	Date string
)

// DevVersion is the version a build without any version information reports.
const DevVersion = "dev"

// shortCommitLength is how many characters of a revision [Info.String] shows.
const shortCommitLength = 12

// Info describes one build of a command.
type Info struct {
	// Version is the release version as stamped or recorded, such as "v1.2.3"
	// or "1.2.3", or [DevVersion] for a build that is no release. It keeps
	// the version as it was stamped, a leading "v" included; [Info.String]
	// is the form shown to users.
	Version string
	// Commit is the VCS revision, or "" when unknown. It is the full
	// revision, except for a build installed by commit ("go install
	// example.com/tool@main"), which only records its first 12 characters.
	Commit string
	// Date is the build or commit time, or the zero time when unknown.
	Date time.Time
	// Modified reports whether the working tree had uncommitted changes when
	// the binary was built. Only the toolchain records it.
	Modified bool
	// GoVersion is the Go release the binary was built with.
	GoVersion string
}

// readBuildInfo is debug.ReadBuildInfo, replaced in tests.
var readBuildInfo = debug.ReadBuildInfo

// Get returns the build information of the running binary: the values
// stamped through linker flags, completed from the toolchain's records.
//
// A version stamped through linker flags is kept as it is, unless it is
// [DevVersion]. Otherwise the module version the toolchain recorded is used
// when it names a release: "go install example.com/tool@v1.2.3", or a plain
// "go build" (Go 1.24 and later) of a checkout whose commit carries the
// release tag and has no uncommitted changes. A version the toolchain made
// up for any other commit (a pseudo-version, such as
// "v1.2.4-0.20260929160445-2f9134944bc2"), and one it marked "+dirty", name
// no release: the build reports DevVersion instead, and [Info.String] shows
// it with its revision, such as "dev+2f9134944bc2.dirty".
func Get() Info {
	return get(Version, Commit, Date, readBuildInfo)
}

func get(version, commit, date string, read func() (*debug.BuildInfo, bool)) Info {
	info := Info{
		Version:   strings.TrimSpace(version),
		Commit:    strings.TrimSpace(commit),
		GoVersion: runtime.Version(),
	}
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(date)); err == nil {
		info.Date = t
	}

	if build, ok := read(); ok && build != nil {
		if build.GoVersion != "" {
			info.GoVersion = build.GoVersion
		}
		var recorded string
		if info.Version == "" || info.Version == DevVersion {
			recorded = build.Main.Version
			if released(recorded) {
				info.Version = recorded
			}
		}
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = setting.Value
				}
			case "vcs.time":
				if info.Date.IsZero() {
					if t, err := time.Parse(time.RFC3339, setting.Value); err == nil {
						info.Date = t
					}
				}
			case "vcs.modified":
				info.Modified = setting.Value == "true"
			}
		}
		// A build of a module at a commit ("go install example.com/tool@main")
		// records no VCS settings; its pseudo-version names the commit.
		if revision, committed, ok := pseudoVersion(recorded); ok {
			if info.Commit == "" {
				info.Commit = revision
			}
			if info.Date.IsZero() {
				info.Date = committed
			}
		}
	}

	if info.Version == "" {
		info.Version = DevVersion
	}
	return info
}

// pseudoVersionPattern matches a Go pseudo-version, as golang.org/x/mod's
// module.IsPseudoVersion does: vX.0.0-yyyymmddhhmmss-abcdef123456,
// vX.Y.(Z+1)-0.yyyymmddhhmmss-abcdef123456 or
// vX.Y.Z-pre.0.yyyymmddhhmmss-abcdef123456, with optional build metadata.
var pseudoVersionPattern = regexp.MustCompile(
	`^v[0-9]+\.(?:0\.0-|\d+\.\d+-(?:[^+]*\.)?0\.)(\d{14})-([A-Za-z0-9]+)(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)

// pseudoVersion returns the revision and commit time a pseudo-version
// carries, and false for any other version.
func pseudoVersion(v string) (revision string, committed time.Time, ok bool) {
	match := pseudoVersionPattern.FindStringSubmatch(v)
	if match == nil || strings.Count(v, "-") < 2 {
		return "", time.Time{}, false
	}
	committed, err := time.Parse("20060102150405", match[1])
	if err != nil {
		return "", time.Time{}, false
	}
	return match[2], committed, true
}

// released reports whether v, a module version the toolchain recorded,
// names a release: not "(devel)", not a pseudo-version, and not marked as
// built from a modified tree.
func released(v string) bool {
	if v == "" || v == "(devel)" {
		return false
	}
	if _, _, ok := pseudoVersion(v); ok {
		return false
	}
	if _, metadata, ok := strings.Cut(v, "+"); ok {
		for part := range strings.SplitSeq(metadata, ".") {
			if part == "dirty" {
				return false
			}
		}
	}
	return true
}

// ShortCommit returns the first 12 characters of the revision, or "" when it
// is unknown.
func (i Info) ShortCommit() string {
	if len(i.Commit) > shortCommitLength {
		return i.Commit[:shortCommitLength]
	}
	return i.Commit
}

// String returns the version as shown to users. A version with a leading
// "v" before a digit loses it ("v1.4.0" becomes "1.4.0"; Version keeps it); a
// development build carries its short revision and, when the tree was dirty,
// a ".dirty" marker ("dev+0123456789ab.dirty").
func (i Info) String() string {
	v := strings.TrimSpace(i.Version)
	if v == "" {
		v = DevVersion
	}
	if v == DevVersion {
		if revision := i.ShortCommit(); revision != "" {
			v += "+" + revision
			if i.Modified {
				v += ".dirty"
			}
		}
		return v
	}
	if len(v) > 1 && (v[0] == 'v' || v[0] == 'V') && v[1] >= '0' && v[1] <= '9' {
		return v[1:]
	}
	return v
}

// Detail returns a multi-line description for a version command: the
// version line, then the revision, build date and Go release where known.
// name is the command's name, written in front of the version.
func (i Info) Detail(name string) string {
	var b strings.Builder
	if name != "" {
		b.WriteString(name)
		b.WriteByte(' ')
	}
	b.WriteString(i.String())
	b.WriteByte('\n')
	if i.Commit != "" {
		commit := i.Commit
		if i.Modified {
			commit += " (modified)"
		}
		fmt.Fprintf(&b, "commit: %s\n", commit)
	}
	if !i.Date.IsZero() {
		fmt.Fprintf(&b, "built:  %s\n", i.Date.UTC().Format(time.RFC3339))
	}
	if i.GoVersion != "" {
		fmt.Fprintf(&b, "go:     %s\n", i.GoVersion)
	}
	return b.String()
}
